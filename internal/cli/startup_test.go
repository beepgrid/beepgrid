package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
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
		writeCommandError(&output, err)
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
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("schema mismatch")} {
		if databaseStartupError(err) != err {
			t.Fatal("unrelated error was relabeled as a database connection failure")
		}
	}
	var output bytes.Buffer
	writeCommandError(&output, &beepgrid.APIError{Status: 403, Code: "DENIED", Message: "Denied"})
	var response map[string]any
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal("client error is no longer JSON", err)
	}
	if response["error"] != "DENIED" || response["status"] != float64(403) || response["exit_code"] != float64(3) {
		t.Fatal("client error contract changed", response)
	}
}
