package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/notif-dev/notif/internal/engine"
	"github.com/notif-dev/notif/internal/httpapi"
	"github.com/notif-dev/notif/internal/mcp"
	"github.com/notif-dev/notif/internal/web"
	"github.com/notif-dev/notif/sdk/go/notif"
)

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func main() {
	if e := run(); e != nil {
		slog.Error("command failed", "error", e)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "serve":
		return serve(ctx, os.Args[2:])
	case "mcp":
		return mcp.Serve(ctx, notif.New(env("NOTIF_URL", "http://localhost:4010"), os.Getenv("NOTIF_TOKEN")), os.Stdin, os.Stdout)
	case "demo":
		return demo(ctx, os.Args[2:])
	case "send":
		if len(os.Args) < 5 {
			return fmt.Errorf("usage: notif send agent|person|group ID MESSAGE")
		}
		c := notif.New(env("NOTIF_URL", "http://localhost:4010"), os.Getenv("NOTIF_TOKEN"))
		r, e := c.Send(ctx, notif.SendInput{Target: notif.Target{Kind: os.Args[2], ID: os.Args[3]}, Body: strings.Join(os.Args[4:], " ")}, "")
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	case "diagnose":
		c := notif.New(env("NOTIF_URL", "http://localhost:4010"), os.Getenv("NOTIF_TOKEN"))
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
	case "version":
		fmt.Println("notif 0.1.0-dev")
		return nil
	}
	return fmt.Errorf("unknown command: %s", command)
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
	if e := f.Parse(args); e != nil {
		return e
	}
	addr := env("NOTIF_ADDR", "127.0.0.1:4010")
	public := env("NOTIF_PUBLIC_URL", "http://localhost:4010")
	if e := httpapi.ValidatePublicURL(public); e != nil {
		return e
	}
	token := os.Getenv("NOTIF_BOOTSTRAP_TOKEN")
	key := os.Getenv("NOTIF_SECRET_KEY")
	if *dev {
		if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") {
			return fmt.Errorf("--dev requires a loopback listening address")
		}
		var e error
		if token == "" {
			token, e = localSecret(".local/owner-token", 32)
			if e != nil {
				return e
			}
		}
		if key == "" {
			key, e = localSecret(".local/encryption-key", 32)
			if e != nil {
				return e
			}
		}
	} else if token == "" || key == "" {
		return fmt.Errorf("NOTIF_BOOTSTRAP_TOKEN and NOTIF_SECRET_KEY are required; use --dev for local setup")
	}
	decoded, e := hex.DecodeString(key)
	if e != nil || len(decoded) != 32 {
		return fmt.Errorf("NOTIF_SECRET_KEY must contain 64 hexadecimal characters")
	}
	store, e := engine.Open(ctx, env("NOTIF_DATABASE_URL", "postgres://notif:notif@localhost:5499/notif?sslmode=disable"), env("NOTIF_SCHEMA", "notif"), decoded)
	if e != nil {
		return e
	}
	defer store.Close()
	store.AllowLocal = *dev
	store.AttachmentsRoot = env("NOTIF_ATTACHMENTS_DIR", ".local/attachments")
	if e = store.ConfigureMatcher(ctx, os.Getenv("NOTIF_MATCHER_URL"), os.Getenv("NOTIF_MATCHER_TOKEN")); e != nil {
		return e
	}
	owner, e := store.Bootstrap(ctx, env("NOTIF_WORKSPACE", "local")+"/"+env("NOTIF_ENVIRONMENT", "development"), env("NOTIF_COMPANY", "Local workspace"), env("NOTIF_ENVIRONMENT", "development"), token)
	if e != nil {
		return e
	}
	_ = owner
	handler := httpapi.New(store, public, web.Handler())
	if e = handler.ConfigureOIDC(ctx, owner.Scope, os.Getenv("NOTIF_OIDC_ISSUER"), os.Getenv("NOTIF_OIDC_CLIENT_ID"), os.Getenv("NOTIF_OIDC_CLIENT_SECRET")); e != nil {
		return e
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			if e := store.DispatchOnce(workerCtx, public); e != nil && workerCtx.Err() == nil {
				slog.Error("dispatch failed", "error", e)
			}
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	failures := make(chan error, 1)
	go func() {
		slog.Info("Notif ready", "url", public, "people_and_agents", "separate")
		if *dev {
			slog.Info("Local owner credential file", "path", ".local/owner-token")
		}
		failures <- server.ListenAndServe()
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
	if e := f.Parse(args); e != nil {
		return e
	}
	token := os.Getenv("NOTIF_TOKEN")
	if token == "" {
		raw, e := os.ReadFile(".local/owner-token")
		if e != nil {
			return fmt.Errorf("NOTIF_TOKEN or local owner token is required")
		}
		token = strings.TrimSpace(string(raw))
	}
	admin := notif.New(env("NOTIF_URL", "http://localhost:4010"), token)
	var me struct {
		Person notif.Person `json:"person"`
	}
	if e := admin.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
		return e
	}
	// The demo explicitly grants narrowly named capabilities through the same role API.
	var role map[string]any
	if e := admin.Do(ctx, "POST", "/api/v1/roles", map[string]any{"name": "Demo " + *name + " " + notif.Key()[:6], "rules": []map[string]string{{"action": "view", "target_kind": "*", "target_id": "*"}, {"action": "message", "target_kind": "person", "target_id": me.Person.ID}, {"action": "ask", "target_kind": "person", "target_id": me.Person.ID}}}, &role, ""); e != nil {
		return e
	}
	c, session, e := admin.Enroll(ctx, notif.EnrollInput{Agent: "demo/" + *name, Name: *name, Capabilities: []string{"message", "status"}})
	if e != nil {
		return e
	}
	if e = admin.Do(ctx, "POST", "/api/v1/role-bindings", map[string]any{"role_id": role["id"], "subject_kind": "principal", "subject_id": session.Agent.ID}, nil, ""); e != nil {
		return e
	}
	fmt.Printf("%s connected as %s (%s)\n", *name, session.Agent.ID, session.InstanceID)
	if *ask {
		q, e := c.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: me.Person.ID}, Title: "Which report format should I use?", Choices: []string{"Brief summary", "Detailed report"}}, "demo-question-"+session.InstanceID)
		if e != nil {
			return e
		}
		fmt.Println("Question queued:", q.ID)
	}
	return c.Run(ctx, func(ctx context.Context, d notif.Delivery) error {
		if d.Payload.Type == "message" && d.Payload.Message != nil {
			m := d.Payload.Message
			_, e := c.Send(ctx, notif.SendInput{ConversationID: m.ConversationID, Body: *name + ": received your message. I am connected and ready. This example uses deterministic responses."}, "reply-"+d.ID)
			return e
		}
		if d.Payload.Type == "request.resolved" && d.Payload.Request != nil {
			fmt.Printf("%s received decision for %s: %v\n", *name, d.Payload.Request.ID, d.Payload.Request.Response)
		}
		return nil
	})
}
