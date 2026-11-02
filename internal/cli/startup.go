package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/beepgrid/beepgrid/sdk/go"
	"github.com/jackc/pgx/v5/pgconn"
)

type startupError struct {
	message string
	cause   error
}

func (e *startupError) Error() string { return e.message }
func (e *startupError) Unwrap() error { return e.cause }

func databaseStartupError(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	var connection *pgconn.ConnectError
	var configuration *pgconn.ParseConfigError
	message := ""
	switch {
	case errors.As(err, &connection):
		message = "Beepgrid could not connect to PostgreSQL.\nCheck that PostgreSQL is running and that BEEPGRID_DATABASE_URL has the correct server, database and credentials."
	case errors.As(err, &configuration):
		message = "Beepgrid could not read BEEPGRID_DATABASE_URL.\nSet it to a valid PostgreSQL connection URL."
	default:
		return err
	}
	return &startupError{message + "\nSetup: https://github.com/beepgrid/beepgrid/blob/main/docs/PACKAGING.md", err}
}

func writeCommandError(w io.Writer, err error) {
	var startup *startupError
	if errors.As(err, &startup) {
		_, _ = fmt.Fprintln(w, startup.Error())
		return
	}
	var api *beepgrid.APIError
	code, status := "command_failed", 0
	if errors.As(err, &api) {
		code, status = api.Code, api.Status
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"error": code, "message": err.Error(), "status": status, "exit_code": clientExitCode(err)})
}
