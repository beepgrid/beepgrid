package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func TestClientCommandsShareSDKContracts(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer worker-token" {
			t.Error("wrong credential")
		}
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		requests = append(requests, map[string]any{"path": r.URL.Path, "body": body, "key": r.Header.Get("Idempotency-Key")})
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/query":
			_, _ = w.Write([]byte(`{"items":[],"next_cursor":"next-page","order":"id"}`))
		case "/api/v1/sessions":
			_, _ = w.Write([]byte(`{"token":"secret-worker","agent":{"id":"a1"},"instance_id":"i1"}`))
		default:
			_, _ = w.Write([]byte(`{"id":"r1","state":"resolved","version":2}`))
		}
	}))
	defer server.Close()
	t.Setenv("NOTIF_TOKEN", "worker-token")
	t.Setenv("NOTIF_URL", server.URL)
	run := func(args []string, input string) string {
		t.Helper()
		var out bytes.Buffer
		handled, err := runClientCommand(context.Background(), args, strings.NewReader(input), &out)
		if !handled || err != nil || !json.Valid(out.Bytes()) {
			t.Fatalf("%v: %v %s", args, err, out.String())
		}
		return out.String()
	}
	run([]string{"ask", "--input", "-", "--key", "stable-key"}, `{"target":{"kind":"person","id":"p1"},"title":"Report?"}`)
	if requests[0]["path"] != "/api/v1/requests" || requests[0]["key"] != "stable-key" {
		t.Fatal(requests)
	}
	run([]string{"approve", "--input", "-"}, `{"target":{"kind":"person","id":"p1"},"title":"Deploy?"}`)
	if requests[1]["body"].(map[string]any)["kind"] != "approval" {
		t.Fatal(requests[1])
	}
	run([]string{"task", "--input", "-"}, `{"target":{"kind":"agent","id":"a1"},"title":"Inspect"}`)
	run([]string{"respond", "--input", "-", "r1"}, `{"outcome":"answered","answer":"Ready","version":1}`)
	run([]string{"wait", "r1"}, "")
	page := run([]string{"list", "--limit", "3", "--cursor", "saved-cursor", "people"}, "")
	if !strings.Contains(page, "next-page") || requests[5]["body"].(map[string]any)["cursor"] != "saved-cursor" {
		t.Fatal(page, requests[5])
	}
	run([]string{"send", "person", "p1", "Hello"}, "")
	run([]string{"api", "GET", "/api/v1/me"}, "")
	tokenFile := filepath.Join(t.TempDir(), "worker-token")
	metadata := run([]string{"enroll", "--input", "-", "--out", tokenFile}, `{"agent":"research/scout","name":"Scout"}`)
	if strings.Contains(metadata, "secret-worker") {
		t.Fatal("credential printed")
	}
	raw, _ := os.ReadFile(tokenFile)
	info, _ := os.Stat(tokenFile)
	if string(raw) != "secret-worker\n" || info.Mode().Perm() != 0600 {
		t.Fatal("credential not protected")
	}
	before := len(requests)
	_, err := runClientCommand(context.Background(), []string{"enroll", "--input", "-", "--out", tokenFile}, strings.NewReader(`{}`), &bytes.Buffer{})
	if err == nil || len(requests) != before {
		t.Fatal("existing token file was overwritten or session issued")
	}
}

func TestClientInputAndAuthorityBoundaries(t *testing.T) {
	t.Setenv("NOTIF_TOKEN", "")
	t.Setenv("NOTIF_TOKEN_FILE", "")
	if _, err := clientToken(false); err == nil {
		t.Fatal("agent client inherited local owner")
	}
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("worker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIF_TOKEN_FILE", file)
	if token, err := clientToken(false); err != nil || token != "worker" {
		t.Fatal(token, err)
	}
	for _, input := range []string{`{"title":"x","typo":true}`, `{} {}`, `null trailing`, strings.Repeat(" ", (4<<20)+1)} {
		if err := readClientInput("-", strings.NewReader(input), &notif.AskInput{}); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	for _, args := range [][]string{
		{"api", "GET", "https://example.com/api/v1/me"},
		{"api", "GET", "/api/v1/../../elsewhere"},
		{"api", "GET", "/api/v1/%2e%2e/elsewhere"},
		{"api", "POST", "/api/v1/credentials"},
		{"enroll", "--input", "-"},
		{"wait", "--timeout", "0", "r1"},
	} {
		_, err := runClientCommand(context.Background(), args, strings.NewReader(`{}`), &bytes.Buffer{})
		if err == nil || clientExitCode(err) != 2 {
			t.Fatal(args, err)
		}
	}
	if clientExitCode(&notif.APIError{Status: 403}) != 3 || clientExitCode(context.DeadlineExceeded) != 4 || clientExitCode(errors.New("network")) != 1 {
		t.Fatal("unstable exit codes")
	}
}

func TestClientTimeoutDoesNotSendCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/requests/r1" {
			t.Error("wait mutated the request", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"r1","state":"open","version":1}`))
	}))
	defer server.Close()
	t.Setenv("NOTIF_TOKEN", "worker")
	t.Setenv("NOTIF_URL", server.URL)
	start := time.Now()
	_, err := runClientCommand(context.Background(), []string{"wait", "--timeout", "50ms", "r1"}, strings.NewReader(""), &bytes.Buffer{})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatal(err)
	}
}

func TestClientDoesNotForwardCredentialsOnRedirect(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/v1/me", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	t.Setenv("NOTIF_TOKEN", "worker")
	t.Setenv("NOTIF_URL", origin.URL)
	_, err := runClientCommand(context.Background(), []string{"api", "GET", "/api/v1/me"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || forwarded {
		t.Fatal("redirect was followed", err, forwarded)
	}
}
