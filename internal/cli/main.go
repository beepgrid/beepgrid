package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/beepgrid/beepgrid/internal/aimodel"
	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/internal/httpapi"
	"github.com/beepgrid/beepgrid/internal/mcp"
	"github.com/beepgrid/beepgrid/internal/web"
	"github.com/beepgrid/beepgrid/sdk/go"
)

var Version = "0.2.1"

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func Main() {
	args := os.Args[1:]
	forceJSON := len(args) > 0 && args[0] == "--json"
	if forceJSON {
		args = args[1:]
	}
	if e := run(args); e != nil && !errors.Is(e, flag.ErrHelp) {
		writeCommandError(os.Stderr, e, forceJSON)
		os.Exit(clientExitCode(e))
	}
}
func run(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if handled, err := runClientCommand(ctx, args, os.Stdin, os.Stdout); handled {
		return err
	}
	command := args[0]
	switch command {
	case "reconcile":
		f := flag.NewFlagSet("reconcile", flag.ContinueOnError)
		restored := f.Bool("restored", false, "Freeze effects and rotate all credentials after database restore")
		resume := f.Bool("resume", false, "Resume after external reconciliation; does not replay unknown work")
		if e := parseCommandFlags(f, args[1:]); e != nil {
			return e
		}
		if *restored == *resume {
			return fmt.Errorf("choose exactly one of --restored or --resume")
		}
		s, e := engine.OpenExisting(ctx, env("BEEPGRID_DATABASE_URL", "postgres://beepgrid:beepgrid@localhost:5499/beepgrid?sslmode=disable"), env("BEEPGRID_SCHEMA", "beepgrid"), nil)
		if e != nil {
			return databaseStartupError(e)
		}
		defer s.Close()
		scope := env("BEEPGRID_WORKSPACE", "local") + "/" + env("BEEPGRID_ENVIRONMENT", "development")
		if *resume {
			return s.ResumeAfterReconciliation(ctx, scope)
		}
		token := engine.Secret()
		path := env("BEEPGRID_RECOVERY_TOKEN_FILE", ".local/recovery-owner-token")
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		if _, e = file.WriteString(token + "\n"); e != nil {
			file.Close()
			return e
		}
		if e = file.Close(); e != nil {
			return e
		}
		if e = s.ReconcileRestored(ctx, scope, token); e != nil {
			_ = os.Remove(path)
			return e
		}
		fmt.Println("Recovery hold set. New owner credential file:", path)
		return nil
	case "migrate":
		if err := parseCommandFlags(flag.NewFlagSet("migrate", flag.ContinueOnError), args[1:]); err != nil {
			return err
		}
		return databaseStartupError(engine.Migrate(ctx, env("BEEPGRID_DATABASE_URL", "postgres://beepgrid:beepgrid@localhost:5499/beepgrid?sslmode=disable"), env("BEEPGRID_SCHEMA", "beepgrid")))
	case "seed":
		return seed(ctx, args[1:])
	case "trace":
		return trace(ctx, args[1:])
	case "serve":
		return serve(ctx, args[1:])
	case "mcp":
		flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
		liveInput := flags.Bool("live-input", false, "Use a separate live receiver; disable MCP inbox intake")
		if err := parseCommandFlags(flags, args[1:]); err != nil {
			return err
		}
		c, err := clientFromEnv()
		if err != nil {
			return err
		}
		if id := os.Getenv("BEEPGRID_AGENT_ID"); id != "" {
			worker, _, e := c.Enroll(ctx, beepgrid.EnrollInput{Agent: id, Name: env("BEEPGRID_AGENT_NAME", id), Capabilities: []string{"message", "request"}})
			if e != nil {
				return e
			}
			c = worker
		}
		return mcp.ServeWithInput(ctx, c, os.Stdin, os.Stdout, *liveInput)
	case "demo":
		return demo(ctx, args[1:])
	case "diagnose":
		if err := parseCommandFlags(flag.NewFlagSet("diagnose", flag.ContinueOnError), args[1:]); err != nil {
			return err
		}
		c, e := operatorClient()
		if e != nil {
			return e
		}
		var me any
		if e := c.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
			return e
		}
		fmt.Println("Authenticated API connection: OK")
		for _, p := range []string{"agents", "requests", "events"} {
			var out any
			if e := c.Do(ctx, "GET", "/api/v1/"+p, nil, &out, ""); e != nil {
				return e
			}
			fmt.Println(p + ": OK")
		}
		return nil
	case "version", "--version":
		if err := parseCommandFlags(flag.NewFlagSet("version", flag.ContinueOnError), args[1:]); err != nil {
			return err
		}
		fmt.Println("beepgrid " + Version)
		return nil
	}
	return usageError{fmt.Errorf("unknown command: %q", command)}
}
func localSecret(path string, n int) (string, error) {
	raw, e := os.ReadFile(path)
	if e == nil {
		return strings.TrimSpace(string(raw)), nil
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", e
	}
	b := make([]byte, n)
	if _, e = rand.Read(b); e != nil {
		return "", e
	}
	secret := hex.EncodeToString(b)
	e = os.WriteFile(path, []byte(secret+"\n"), 0600)
	return secret, e
}
func serve(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	dev := f.Bool("dev", false, "Generate local credentials and allow local provider connections; localhost only")
	noUI := f.Bool("no-ui", false, "Disable workspace browser pages; keep the authenticated API and workers")
	if e := parseCommandFlags(f, args); e != nil {
		return e
	}
	addr := env("BEEPGRID_ADDR", "127.0.0.1:4010")
	public := env("BEEPGRID_PUBLIC_URL", "http://localhost:4010")
	if e := httpapi.ValidatePublicURL(public); e != nil {
		return setupFailure(e, "The public server URL is invalid.", "Set BEEPGRID_PUBLIC_URL to an HTTP(S) origin, such as http://localhost:4010, without a path.", "PACKAGING.md#server-configuration")
	}
	token := os.Getenv("BEEPGRID_BOOTSTRAP_TOKEN")
	key := os.Getenv("BEEPGRID_SECRET_KEY")
	if *dev {
		if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") {
			return setupFailure(usageError{fmt.Errorf("--dev requires a loopback listening address")}, "Local development mode needs a local listen address.", "Set BEEPGRID_ADDR to 127.0.0.1:4010. For public hosting, configure production credentials and run without --dev.", "PACKAGING.md#server-configuration")
		}
		var e error
		if token == "" {
			token, e = localSecret(".local/owner-token", 32)
			if e != nil {
				return localFileStartupError(e)
			}
		}
		if key == "" {
			key, e = localSecret(".local/encryption-key", 32)
			if e != nil {
				return localFileStartupError(e)
			}
		}
	} else if token == "" || key == "" {
		return setupFailure(fmt.Errorf("server credentials are missing"), "Beepgrid needs its server credentials.", "For local use, run beepgrid serve --dev. For hosting, set BEEPGRID_BOOTSTRAP_TOKEN and BEEPGRID_SECRET_KEY.", "OPERATIONS.md#existing-postgresql")
	}
	decoded, e := hex.DecodeString(key)
	if e != nil || len(decoded) != 32 {
		return setupFailure(fmt.Errorf("invalid encryption key"), "The server encryption key is invalid.", "BEEPGRID_SECRET_KEY must contain 64 hexadecimal characters. Keep the original key when reopening an existing database.", "PACKAGING.md#server-configuration")
	}
	openStore := engine.OpenExisting
	if *dev {
		openStore = engine.Open
	}
	store, e := openStore(ctx, env("BEEPGRID_DATABASE_URL", "postgres://beepgrid:beepgrid@localhost:5499/beepgrid?sslmode=disable"), env("BEEPGRID_SCHEMA", "beepgrid"), decoded)
	if e != nil {
		return databaseStartupError(e)
	}
	defer store.Close()
	stopDiagnostics, e := startDiagnostics(ctx, store, os.Getenv("BEEPGRID_PROFILE_ADDR"))
	if e != nil {
		return setupFailure(e, "The optional diagnostics listener could not start.", "Use an available literal loopback address in BEEPGRID_PROFILE_ADDR, or unset it to disable profiling.", "OPERATIONS.md#local-profiling-and-load-qualification")
	}
	defer stopDiagnostics()
	store.AllowLocal = *dev
	store.AttachmentsRoot = env("BEEPGRID_ATTACHMENTS_DIR", ".local/attachments")
	aiConfig, e := aimodel.FromEnv(os.Getenv)
	if e != nil {
		return setupFailure(e, "The optional AI configuration is invalid.", "Check BEEPGRID_AI_* settings, or unset them if model assistance is not needed.", "ASSISTANCE.md")
	}
	if e = store.ConfigureAI(ctx, aiConfig); e != nil {
		return setupFailure(e, "The optional AI provider could not be configured.", "Check the BEEPGRID_AI_* provider, model, endpoint and credential settings.", "ASSISTANCE.md")
	}
	if e = store.ConfigureOverseer(ctx, os.Getenv("BEEPGRID_OVERSEER_URL"), os.Getenv("BEEPGRID_OVERSEER_TOKEN")); e != nil {
		return setupFailure(e, "The optional Coordinator endpoint could not be configured.", "Check BEEPGRID_OVERSEER_URL and BEEPGRID_OVERSEER_TOKEN, or unset them if unused.", "ASSISTANCE.md")
	}
	owner, e := store.Bootstrap(ctx, env("BEEPGRID_WORKSPACE", "local")+"/"+env("BEEPGRID_ENVIRONMENT", "development"), env("BEEPGRID_COMPANY", "Local workspace"), env("BEEPGRID_ENVIRONMENT", "development"), token)
	if e != nil {
		return setupFailure(e, "The workspace could not be opened.", "Check the database permissions and server credentials for this workspace.", "OPERATIONS.md#existing-postgresql")
	}
	_ = owner
	var ui http.Handler
	if !*noUI {
		ui = web.Handler()
	}
	handler := httpapi.New(store, public, ui)
	if e = handler.ConfigureOIDC(ctx, owner.Scope, os.Getenv("BEEPGRID_OIDC_ISSUER"), os.Getenv("BEEPGRID_OIDC_CLIENT_ID"), os.Getenv("BEEPGRID_OIDC_CLIENT_SECRET")); e != nil {
		return setupFailure(e, "The sign-in provider could not be configured.", "Check the BEEPGRID_OIDC_* settings and that the issuer is reachable.", "OPERATIONS.md#credentials-and-accounts")
	}
	server := &http.Server{Addr: addr, Handler: handler, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	listener, e := net.Listen("tcp", addr)
	if e != nil {
		return listenStartupError(e, "BEEPGRID_ADDR")
	}
	defer listener.Close()
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go store.RunCoordinatorSummaries(workerCtx)
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			ready, release := store.WatchProviders()
			n, e := store.DispatchBatch(workerCtx, public, 32)
			if e != nil && workerCtx.Err() == nil {
				slog.Error("dispatch failed", "error", e)
			}
			if e == nil && n == 32 {
				release()
				continue
			}
			select {
			case <-workerCtx.Done():
				release()
				return
			case <-ticker.C:
			case <-ready:
			}
			release()
		}
	}()
	failures := make(chan error, 1)
	go func() {
		slog.Info("Beep Grid ready", "url", public, "people_and_agents", "separate")
		if *dev {
			slog.Info("Local owner credential file", "path", ".local/owner-token")
		}
		failures <- server.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		stopWorker()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case e := <-failures:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	}
}
func demo(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("demo", flag.ContinueOnError)
	name := f.String("name", "scout", "Agent name")
	ask := f.Bool("ask", false, "Ask the owner a sample question")
	if e := parseCommandFlags(f, args); e != nil {
		return e
	}
	token := os.Getenv("BEEPGRID_TOKEN")
	if token == "" {
		raw, e := os.ReadFile(".local/owner-token")
		if e != nil {
			return fmt.Errorf("BEEPGRID_TOKEN or local owner token is required")
		}
		token = strings.TrimSpace(string(raw))
	}
	admin := beepgrid.New(env("BEEPGRID_URL", "http://localhost:4010"), token)
	var me struct {
		Person beepgrid.Person `json:"person"`
	}
	if e := admin.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
		return e
	}
	// The demo explicitly grants narrowly named capabilities through the same role API.
	var role map[string]any
	var roles []map[string]any
	if e := admin.Do(ctx, "GET", "/api/v1/roles", nil, &roles, ""); e != nil {
		return e
	}
	for _, existing := range roles {
		if existing["name"] == "Demo "+*name {
			role = existing
			break
		}
	}
	var roleID any
	if role != nil {
		roleID = role["id"]
	}
	if e := admin.Do(ctx, "POST", "/api/v1/roles", map[string]any{"id": roleID, "name": "Demo " + *name, "rules": []map[string]string{{"action": "view", "target_kind": "*", "target_id": "*"}, {"action": "message", "target_kind": "person", "target_id": me.Person.ID}, {"action": "ask", "target_kind": "person", "target_id": me.Person.ID}}}, &role, ""); e != nil {
		return e
	}
	c, session, e := admin.Enroll(ctx, beepgrid.EnrollInput{Agent: "demo/" + *name, Name: *name, Capabilities: []string{"message", "status"}})
	if e != nil {
		return e
	}
	if e = admin.Do(ctx, "POST", "/api/v1/role-bindings", map[string]any{"role_id": role["id"], "subject_kind": "principal", "subject_id": session.Agent.ID}, nil, ""); e != nil {
		return e
	}
	fmt.Printf("%s connected as %s (%s)\n", *name, session.Agent.ID, session.InstanceID)
	_ = c.Heartbeat(ctx, map[string]any{"state": "ready", "runtime": "deterministic-example"})
	if *ask {
		q, e := c.Ask(ctx, beepgrid.AskInput{Target: beepgrid.Target{Kind: "person", ID: me.Person.ID}, Title: "Which report format should I use?", Choices: []string{"Brief summary", "Detailed report"}}, "demo-question-"+session.InstanceID)
		if e != nil {
			return e
		}
		fmt.Println("Question queued:", q.ID)
	}
	return c.Run(ctx, func(ctx context.Context, d beepgrid.Delivery) error {
		if d.Payload.Type == "message" && d.Payload.Message != nil {
			m := d.Payload.Message
			_, e := c.Send(ctx, beepgrid.SendInput{ConversationID: m.ConversationID, Body: *name + ": received your message. I am connected and ready. This example uses deterministic responses."}, "reply-"+d.ID)
			return e
		}
		if d.Payload.Type == "request.resolved" && d.Payload.Request != nil {
			fmt.Printf("%s received decision for %s: %v\n", *name, d.Payload.Request.ID, d.Payload.Request.Response)
		}
		return nil
	})
}
