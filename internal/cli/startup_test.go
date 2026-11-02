package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/beepgrid/beepgrid/sdk/go"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNoCommandExplainsSetupWithoutStartingServer(t *testing.T) {
	var output bytes.Buffer
	handled, err := runClientCommand(context.Background(), nil, strings.NewReader(""), &output)
	if !handled || err != nil {
		t.Fatalf("no-command setup: handled=%v err=%v", handled, err)
	}
	for _, want := range []string{"PostgreSQL", "BEEPGRID_DATABASE_URL", "beepgrid serve --dev", "docs/PACKAGING.md"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing first-run guidance %q", want)
		}
	}
}

func TestDatabaseStartupErrorsAreActionableAndPrivate(t *testing.T) {
	config, err := pgconn.ParseConfig("postgres://private_user:private_password@127.0.0.1/beepgrid")
	if err != nil {
		t.Fatal(err)
	}
	config.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("private connection detail")
	}
	_, connectionErr := pgconn.ConnectConfig(context.Background(), config)
	config.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	}
	_, timeoutErr := pgconn.ConnectConfig(context.Background(), config)
	_, configurationErr := pgxpool.ParseConfig("postgres://private_user:private_password@127.0.0.1:invalid/beepgrid")
	for _, original := range []error{connectionErr, timeoutErr, configurationErr} {
		if original == nil {
			t.Fatal("expected a real driver error")
		}
		err := databaseStartupError(original)
		if !errors.Is(err, original) {
			t.Fatal("startup guidance discarded the underlying error")
		}
		var output bytes.Buffer
		writeCommandError(&output, err, false)
		text := output.String()
		if !strings.HasPrefix(text, "Beepgrid could not") || !strings.Contains(text, "BEEPGRID_DATABASE_URL") || !strings.Contains(text, "docs/PACKAGING.md") {
			t.Fatal("missing actionable startup guidance", text)
		}
		for _, private := range []string{"private_user", "private_password", "private connection detail"} {
			if strings.Contains(text, private) {
				t.Fatal("driver details appeared in first-run output")
			}
		}
	}
}

func TestStartupGuidancePreservesOtherErrorsAndClientJSON(t *testing.T) {
	for _, err := range []error{context.Canceled} {
		if databaseStartupError(err) != err {
			t.Fatal("unrelated error was relabeled as a database connection failure")
		}
	}
	var output bytes.Buffer
	writeCommandError(&output, &beepgrid.APIError{Status: 403, Code: "DENIED", Message: "Denied"}, false)
	var response map[string]any
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal("client error is no longer JSON", err)
	}
	if response["error"] != "DENIED" || response["status"] != float64(403) || response["exit_code"] != float64(3) {
		t.Fatal("client error contract changed", response)
	}
}

func TestDatabaseAdviceDistinguishesSetupFailures(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"28P01", "credentials"}, {"3D000", "does not exist"},
		{"42501", "permissions"}, {"42P01", "not initialized"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			original := &pgconn.PgError{Code: tc.code, Message: "private database detail"}
			err := databaseStartupError(fmt.Errorf("database: %w", original))
			var output bytes.Buffer
			writeCommandError(&output, err, false)
			if !strings.Contains(output.String(), tc.want) || strings.Contains(output.String(), "private database detail") || !errors.Is(err, original) {
				t.Fatal("wrong or unsafe database guidance", output.String())
			}
		})
	}
	for _, message := range []string{"schema must be migrated to version 38 before serving", "database version 99 is newer than this binary", "migration 2 checksum changed"} {
		advice := commandErrorAdvice(databaseStartupError(errors.New(message)))
		if !strings.Contains(advice.guide, "OPERATIONS.md") || strings.Contains(advice.message, "could not connect") {
			t.Fatal("schema failure was mislabeled as connectivity", advice)
		}
	}
}

func TestForcedJSONKeepsStartupHintsAndClientContract(t *testing.T) {
	var output bytes.Buffer
	err := setupFailure(errors.New("private underlying detail"), "Configuration is missing.", "Set the required configuration.", "PACKAGING.md#server-configuration")
	writeCommandError(&output, err, true)
	var result map[string]any
	if e := json.Unmarshal(output.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result["message"] != "Configuration is missing." || result["hint"] != "Set the required configuration." || result["exit_code"] != float64(1) || strings.Contains(output.String(), "private underlying detail") {
		t.Fatal(result)
	}
	output.Reset()
	original := &beepgrid.APIError{Status: 409, Code: "CONFLICT", Message: "Resource changed"}
	err = fmt.Errorf("update: %w", original)
	writeCommandError(&output, err, true)
	if e := json.Unmarshal(output.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result["error"] != original.Code || result["status"] != float64(original.Status) || result["message"] != err.Error() || result["exit_code"] != float64(clientExitCode(err)) {
		t.Fatal("machine error fields changed", result)
	}
}

func TestHumanErrorsStayShortWithoutTerminalControls(t *testing.T) {
	err := &beepgrid.APIError{Status: 401, Message: "\x1b[31mCredential\nwas rejected" + strings.Repeat("界", 700)}
	var output bytes.Buffer
	writeErrorAdvice(&output, commandErrorAdvice(err))
	text := output.String()
	if len(strings.Split(strings.TrimSpace(text), "\n")) != 3 || strings.Contains(text, "\x1b") || !strings.Contains(text, "expired or been revoked") || len([]rune(strings.Split(text, "\n")[0])) > 503 {
		t.Fatal("unbounded or unreadable terminal error", text)
	}
}

func TestListenGuidanceKeepsOccupiedListenerRunning(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	second, original := net.Listen("tcp", listener.Addr().String())
	if second != nil {
		second.Close()
		t.Fatal("occupied address was acquired")
	}
	err = listenStartupError(original, "BEEPGRID_ADDR")
	advice := commandErrorAdvice(err)
	if !errors.Is(err, syscall.EADDRINUSE) || !strings.Contains(advice.message, "already in use") || !strings.Contains(advice.action, "BEEPGRID_ADDR") {
		t.Fatal(advice)
	}
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal("guidance changed the existing service", err)
	}
	connection.Close()
}

func TestCLIRejectsInvalidServerURLsWithoutEchoingSecrets(t *testing.T) {
	t.Setenv("BEEPGRID_TOKEN", "private_token")
	for _, value := range []string{"postgres://private_user:private_password@localhost/beepgrid", "https://private_user:private_password@example.com", "https://example.com?token=private_token", "http://%"} {
		t.Setenv("BEEPGRID_URL", value)
		_, err := clientFromEnv()
		if err == nil || clientExitCode(err) != 2 || strings.Contains(err.Error(), "private_") {
			t.Fatal("bad client URL accepted or echoed", err)
		}
	}
}

func TestLocalCredentialFilesystemFailureHasGuidance(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".local", []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEEPGRID_BOOTSTRAP_TOKEN", "")
	t.Setenv("BEEPGRID_SECRET_KEY", "")
	t.Setenv("BEEPGRID_ADDR", "127.0.0.1:4010")
	t.Setenv("BEEPGRID_PUBLIC_URL", "http://localhost:4010")
	err := serve(context.Background(), []string{"--dev"})
	if err == nil || !strings.Contains(commandErrorAdvice(err).guide, "#local-files") {
		t.Fatal("local write failure did not explain setup", err)
	}
	contents, _ := os.ReadFile(".local")
	if string(contents) != "not a directory" {
		t.Fatal("existing local path was replaced")
	}
}
