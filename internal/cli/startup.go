package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"syscall"
	"unicode"

	"github.com/beepgrid/beepgrid/sdk/go"
	"github.com/jackc/pgx/v5/pgconn"
)

const docsURL = "https://github.com/beepgrid/beepgrid/blob/main/docs/"

type startupError struct {
	message string
	action  string
	guide   string
	cause   error
}

func (e *startupError) Error() string { return e.message }
func (e *startupError) Unwrap() error { return e.cause }

func setupFailure(err error, message, action, guide string) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	return &startupError{message: message, action: action, guide: docsURL + guide, cause: err}
}

func databaseStartupError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var connection *pgconn.ConnectError
	var configuration *pgconn.ParseConfigError
	var postgres *pgconn.PgError
	guide := "PACKAGING.md#database-connection"
	if errors.As(err, &postgres) {
		switch postgres.Code {
		case "28P01", "28000":
			return setupFailure(err, "PostgreSQL did not accept the database credentials.", "Check the username and password in BEEPGRID_DATABASE_URL.", guide)
		case "3D000":
			return setupFailure(err, "The configured PostgreSQL database does not exist.", "Create the database named in BEEPGRID_DATABASE_URL, then try again.", guide)
		case "42501":
			return setupFailure(err, "The database user does not have the required permissions.", "Check the database grants. Run migrations with a deployment credential permitted to change the schema.", "OPERATIONS.md#existing-postgresql")
		case "42P01":
			return setupFailure(err, "The Beepgrid database schema is not initialized.", "Run beepgrid migrate with your deployment database credential before starting the server.", "OPERATIONS.md#existing-postgresql")
		}
	}
	switch {
	case errors.As(err, &connection):
		return setupFailure(err, "Beepgrid could not connect to PostgreSQL.", "Start PostgreSQL and check the server, database and credentials in BEEPGRID_DATABASE_URL.", guide)
	case errors.As(err, &configuration):
		return setupFailure(err, "Beepgrid could not read BEEPGRID_DATABASE_URL.", "Set it to a valid PostgreSQL connection URL.", guide)
	case strings.HasPrefix(err.Error(), "schema must be migrated to version "):
		return setupFailure(err, "The database schema needs an upgrade.", "Back up the database, then run beepgrid migrate with your deployment database credential.", "OPERATIONS.md#existing-postgresql")
	case strings.HasPrefix(err.Error(), "database version "):
		return setupFailure(err, "This Beepgrid executable is older than the database schema.", "Install a compatible newer Beepgrid version. Do not downgrade the database.", "OPERATIONS.md#existing-postgresql")
	}
	return setupFailure(err, "Beepgrid could not open its database.", "Check the database schema, migration history and permissions before retrying.", "OPERATIONS.md#existing-postgresql")
}

func listenStartupError(err error, setting string) error {
	message := "Beepgrid could not open its listen address."
	action := "Check " + setting + " and use an available host:port address."
	if errors.Is(err, syscall.EADDRINUSE) {
		message = "The listen address is already in use."
		action = "Stop the other process or choose an available port in " + setting + "."
	} else if errors.Is(err, fs.ErrPermission) {
		message = "Beepgrid is not allowed to use this listen address."
		action = "Choose an unprivileged port, such as 4010, in " + setting + "."
	}
	return setupFailure(err, message, action, "PACKAGING.md#listen-address")
}

func localFileStartupError(err error) error {
	return setupFailure(err, "Beepgrid could not read or save its local credentials.", "Run from a writable directory and check access to .local. Keep existing credential files when reopening this workspace.", "PACKAGING.md#local-files")
}

func parseCommandFlags(flags *flag.FlagSet, args []string) error {
	flags.SetOutput(io.Discard)
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprintf(os.Stdout, "Usage: beepgrid %s [options]\n", flags.Name())
		flags.SetOutput(os.Stdout)
		flags.PrintDefaults()
		return flag.ErrHelp
	}
	if err != nil {
		return usageError{err}
	}
	if flags.NArg() != 0 {
		return usageError{fmt.Errorf("%s accepts options, not positional arguments", flags.Name())}
	}
	return nil
}

type errorAdvice struct{ message, action, guide string }

func commandErrorAdvice(err error) errorAdvice {
	var startup *startupError
	if errors.As(err, &startup) {
		return errorAdvice{startup.message, startup.action, startup.guide}
	}
	advice := errorAdvice{message: err.Error(), guide: docsURL + "CLI.md#troubleshooting"}
	var api *beepgrid.APIError
	var path *os.PathError
	var transport *url.Error
	var usage usageError
	switch {
	case errors.As(err, &api):
		if api.Message != "" {
			advice.message = api.Message
		}
		switch api.Status {
		case 401:
			advice.action = "Check that your credential belongs to this server and has not expired or been revoked."
		case 403:
			advice.action = "Use a credential with permission for this operation, or ask your workspace administrator."
		case 404:
			advice.action = "Check the resource ID and that it is visible to your credential."
		case 409:
			advice.action = "Read the current resource state before retrying the operation."
		case 429:
			advice.action = "Wait for the server's retry interval before trying again."
		default:
			advice.action = "Check the request and the server status before retrying."
		}
	case errors.Is(err, context.Canceled):
		advice.message = "The command was interrupted."
		advice.action = "Check the current request state before retrying. Interrupting this client does not cancel a saved request."
	case errors.Is(err, context.DeadlineExceeded):
		advice.message = "The command timed out."
		advice.action = "Check server connectivity and the current request state before retrying. A wait timeout does not cancel the request."
	case errors.As(err, &path):
		advice.action = "Check the file path and your read/write permissions. Existing output files are never overwritten."
		if errors.Is(err, fs.ErrExist) {
			advice.message = "The output file already exists."
			advice.action = "Choose a new output path. Existing files are never overwritten."
		}
	case errors.As(err, &transport):
		advice.message = "Beepgrid could not reach the server."
		advice.action = "Check BEEPGRID_URL, that the server is running, and your network or TLS configuration."
	case errors.As(err, &usage):
		advice.action = "Run beepgrid help, or beepgrid COMMAND --help, for usage and configuration."
	default:
		advice.action = "Check the command and configuration. See the troubleshooting guide for the next step."
	}
	return advice
}

func terminalOutput(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func writeCommandError(w io.Writer, err error, forceJSON bool) {
	advice := commandErrorAdvice(err)
	var startup *startupError
	if !forceJSON && (errors.As(err, &startup) || terminalOutput(w)) {
		writeErrorAdvice(w, advice)
		return
	}
	var api *beepgrid.APIError
	code, status := "command_failed", 0
	if errors.As(err, &api) {
		code, status = api.Code, api.Status
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"error": code, "message": err.Error(), "status": status, "exit_code": clientExitCode(err), "hint": advice.action, "docs_url": advice.guide})
}

func writeErrorAdvice(w io.Writer, advice errorAdvice) {
	message := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, advice.message)
	message = strings.Join(strings.Fields(message), " ")
	runes := []rune(message)
	if len(runes) > 500 {
		message = string(runes[:500]) + "..."
	}
	_, _ = fmt.Fprintf(w, "%s\n%s\nGuide: %s\n", message, advice.action, advice.guide)
}
