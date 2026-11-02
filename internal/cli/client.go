package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

const clientHelp = `Beep Grid

Server: serve [--dev] [--no-ui], migrate, reconcile, seed, diagnose
Clients (flags before positional arguments):
  send [--input file|-] [--key KEY] agent|person|group ID MESSAGE
  ask|approve|task --input file|- [--key KEY]
  respond --input file|- REQUEST_ID
  wait [--timeout 5m] REQUEST_ID
  list [--limit 50] [--cursor CURSOR] [--search TEXT] COLLECTION
  api [--input file|-] [--key KEY] [--out FILE] METHOD /api/v1/PATH
  enroll --input file|- --out TOKEN_FILE
	files CONVERSATION_ID
	file-upload --conversation CONVERSATION_ID [--media MIME] FILE
	file-stage [--media MIME] FILE
	file-download --out FILE ATTACHMENT_ID
  mcp, trace REQUEST_ID, version

Clients use NOTIF_URL and NOTIF_TOKEN_FILE (or NOTIF_TOKEN).
JSON goes to stdout; errors go to stderr. Input is limited to 4 MiB.
The JSON commands (send through enroll) accept --timeout (30s; wait 5m).
See docs/CLI.md for schemas, permissions and safe retry examples.
`

type usageError struct{ error }

func clientExitCode(err error) int {
	var usage usageError
	var api *notif.APIError
	switch {
	case errors.As(err, &usage):
		return 2
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return 4
	case errors.As(err, &api) && (api.Status == 401 || api.Status == 403):
		return 3
	default:
		return 1
	}
}

func clientToken(localOwner bool) (string, error) {
	if token := strings.TrimSpace(os.Getenv("NOTIF_TOKEN")); token != "" {
		return token, nil
	}
	file := os.Getenv("NOTIF_TOKEN_FILE")
	if file == "" && localOwner {
		file = ".local/owner-token"
	}
	if file == "" {
		return "", usageError{fmt.Errorf("set NOTIF_TOKEN_FILE or NOTIF_TOKEN explicitly for this client")}
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read credential file: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", usageError{fmt.Errorf("credential file is empty")}
	}
	return token, nil
}

func clientFromEnv() (*notif.Client, error) {
	token, err := clientToken(false)
	if err != nil {
		return nil, err
	}
	c := notif.New(env("NOTIF_URL", "http://localhost:4010"), token)
	// Bearer credentials must never follow a redirect to another server.
	c.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c, nil
}

func readClientInput(file string, stdin io.Reader, out any) error {
	if file == "" {
		return usageError{fmt.Errorf("--input file|- is required")}
	}
	r := stdin
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	raw, err := io.ReadAll(io.LimitReader(r, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 4<<20 {
		return usageError{fmt.Errorf("JSON input exceeds 4 MiB")}
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return usageError{fmt.Errorf("invalid JSON input: %w", err)}
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return usageError{fmt.Errorf("input must contain exactly one JSON value")}
	}
	return nil
}

// Output files are reserved before network mutations and never overwritten.
func reserveClientOutput(file string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return nil, err
	}
	return os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
}

func runClientCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	command := args[0]
	if command == "help" || command == "--help" || command == "-h" {
		_, err := io.WriteString(stdout, clientHelp)
		return true, err
	}
	switch command {
	case "send", "ask", "approve", "task", "respond", "wait", "list", "api", "enroll", "files", "file-upload", "file-stage", "file-download":
	default:
		return false, nil
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	input := f.String("input", "", "JSON file or - for stdin")
	key := f.String("key", "", "Stable idempotency key for creation")
	out := f.String("out", "", "New protected output file; never overwritten")
	limit := f.Int("limit", 50, "Query page size")
	cursor := f.String("cursor", "", "Query continuation cursor")
	search := f.String("search", "", "Query search text")
	conversation := f.String("conversation", "", "Existing conversation for file upload")
	media := f.String("media", "application/octet-stream", "File media type")
	defaultTimeout := 30 * time.Second
	if command == "wait" {
		defaultTimeout = 5 * time.Minute
	}
	timeout := f.Duration("timeout", defaultTimeout, "Total command deadline")
	if err := f.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		_, err = io.WriteString(stdout, clientHelp)
		return true, err
	} else if err != nil {
		return true, usageError{err}
	}
	if *timeout <= 0 {
		return true, usageError{fmt.Errorf("--timeout must be positive")}
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	c, err := clientFromEnv()
	if err != nil {
		return true, err
	}
	var output *os.File
	success := false
	if *out != "" {
		output, err = reserveClientOutput(*out)
		if err != nil {
			return true, err
		}
		defer func() {
			output.Close()
			if !success {
				_ = os.Remove(*out)
			}
		}()
	}
	pos := f.Args()
	var result any
	need := func(count int) error {
		if len(pos) != count {
			return usageError{fmt.Errorf("invalid %s arguments; see notif help (flags precede positional arguments)", command)}
		}
		return nil
	}
	switch command {
	case "files":
		if err = need(1); err == nil {
			result, err = c.Attachments(ctx, pos[0])
		}
	case "file-upload", "file-stage":
		if err = need(1); err != nil {
			break
		}
		if command == "file-upload" && *conversation == "" {
			err = usageError{fmt.Errorf("file-upload requires --conversation")}
			break
		}
		var file *os.File
		file, err = os.Open(pos[0])
		if err != nil {
			break
		}
		var data []byte
		data, err = io.ReadAll(io.LimitReader(file, notif.MaxAttachmentBytes+1))
		file.Close()
		if err == nil {
			if command == "file-stage" {
				result, err = c.StageAttachment(ctx, filepath.Base(pos[0]), *media, data)
			} else {
				result, err = c.UploadAttachment(ctx, *conversation, filepath.Base(pos[0]), *media, data)
			}
		}
	case "file-download":
		if err = need(1); err != nil {
			break
		}
		if output == nil {
			err = usageError{fmt.Errorf("file-download requires --out FILE; bytes are never printed to the terminal")}
			break
		}
		var data []byte
		data, err = c.DownloadAttachment(ctx, pos[0])
		if err == nil {
			_, err = output.Write(data)
		}
		result = map[string]any{"output_file": *out, "bytes": len(data)}
	case "send":
		var in notif.SendInput
		if *input != "" {
			if err = need(0); err == nil {
				err = readClientInput(*input, stdin, &in)
			}
		} else if len(pos) >= 3 {
			in.Target = notif.Target{Kind: pos[0], ID: pos[1]}
			in.Body = strings.Join(pos[2:], " ")
		} else {
			err = usageError{fmt.Errorf("send needs --input or target kind, ID and message")}
		}
		if err == nil {
			result, err = c.Send(ctx, in, *key)
		}
	case "ask", "approve", "task":
		var in notif.AskInput
		if err = need(0); err == nil {
			err = readClientInput(*input, stdin, &in)
		}
		if err == nil {
			if command != "ask" {
				in.Kind = map[string]string{"approve": "approval", "task": "task"}[command]
			}
			result, err = c.Ask(ctx, in, *key)
		}
	case "respond":
		var in notif.Response
		if err = need(1); err == nil {
			err = readClientInput(*input, stdin, &in)
		}
		if err == nil {
			result, err = c.Respond(ctx, pos[0], in)
		}
	case "wait":
		if err = need(1); err == nil {
			result, err = c.Wait(ctx, pos[0])
		}
	case "list":
		if err = need(1); err == nil {
			result, err = c.Query(ctx, notif.QueryInput{Collection: pos[0], Search: *search, Limit: *limit, Cursor: *cursor})
		}
	case "enroll":
		var in notif.EnrollInput
		if output == nil {
			err = usageError{fmt.Errorf("enroll requires --out TOKEN_FILE; credentials are never printed")}
		} else if err = need(0); err == nil {
			err = readClientInput(*input, stdin, &in)
		}
		if err == nil {
			var session notif.Session
			_, session, err = c.Enroll(ctx, in)
			if err == nil {
				_, err = io.WriteString(output, session.Token+"\n")
				result = map[string]any{"agent": session.Agent, "instance_id": session.InstanceID, "expires_at": session.ExpiresAt, "token_file": *out}
			}
		}
	case "api":
		if err = need(2); err != nil {
			break
		}
		u, parseErr := url.ParseRequestURI(pos[1])
		if parseErr != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/api/v1/") || path.Clean(u.Path) != u.Path {
			err = usageError{fmt.Errorf("api requires a relative /api/v1/ path without traversal")}
			break
		}
		method := strings.ToUpper(pos[0])
		if method != "GET" && method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
			err = usageError{fmt.Errorf("unsupported HTTP method")}
			break
		}
		if method != "GET" && (u.Path == "/api/v1/sessions" || u.Path == "/api/v1/credentials") && output == nil {
			err = usageError{fmt.Errorf("credential issuance requires --out FILE (or use enroll)")}
			break
		}
		var in json.RawMessage
		if *input != "" {
			err = readClientInput(*input, stdin, &in)
		}
		if err == nil {
			var raw json.RawMessage
			err = c.Do(ctx, method, pos[1], in, &raw, *key)
			if len(raw) == 0 {
				raw = json.RawMessage("null")
			}
			result = raw
		}
	}
	if err != nil {
		return true, err
	}
	if output != nil {
		if command != "enroll" && command != "file-download" {
			err = json.NewEncoder(output).Encode(result)
		}
		if err == nil {
			err = output.Sync()
		}
		if err != nil {
			return true, err
		}
		if command != "enroll" && command != "file-download" {
			result = map[string]string{"output_file": *out}
		}
	}
	success = true
	return true, json.NewEncoder(stdout).Encode(result)
}
