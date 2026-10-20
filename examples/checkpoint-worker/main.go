// A small restartable adapter. It owns no LLM and demonstrates durable question/result recovery.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/notif-dev/notif/sdk/go/notif"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type checkpoint struct {
	RequestID string `json:"request_id"`
	Stage     string `json:"stage"`
	Answer    string `json:"answer"`
}

func save(path string, state checkpoint) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	raw, e := json.Marshal(state)
	if e != nil {
		return e
	}
	file, e := os.CreateTemp(filepath.Dir(path), "checkpoint-*")
	if e != nil {
		return e
	}
	defer os.Remove(file.Name())
	if e = file.Chmod(0600); e != nil {
		file.Close()
		return e
	}
	if _, e = file.Write(raw); e != nil {
		file.Close()
		return e
	}
	if e = file.Sync(); e != nil {
		file.Close()
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	if e = os.Rename(file.Name(), path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func run(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("checkpoint-worker", flag.ContinueOnError)
	statePath := f.String("state", ".local/checkpoint-worker/state.json", "Local checkpoint, one active process per directory")
	pause := f.Bool("stop-after-question", false, "Persist question handle and exit to demonstrate restart")
	if e := f.Parse(args); e != nil {
		return e
	}
	token := os.Getenv("NOTIF_TOKEN")
	human := os.Getenv("NOTIF_PERSON_ID")
	if token == "" || human == "" {
		return fmt.Errorf("NOTIF_TOKEN (launcher) and NOTIF_PERSON_ID are required")
	}
	base := os.Getenv("NOTIF_URL")
	if base == "" {
		base = "http://localhost:4010"
	}
	launcher := notif.New(base, token)
	c, _, e := launcher.Enroll(ctx, notif.EnrollInput{Agent: "examples/checkpoint", Name: "Checkpoint worker", Capabilities: []string{"question", "checkpoint-resume"}})
	if e != nil {
		return e
	}
	state := checkpoint{}
	raw, e := os.ReadFile(*statePath)
	if e == nil {
		if e = json.Unmarshal(raw, &state); e != nil {
			return fmt.Errorf("invalid checkpoint: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if state.Stage == "finished" {
		fmt.Println("Already finished; no duplicate result.")
		return nil
	}
	// A stable key recovers even if the process dies after API commit and before saving this file.
	if state.RequestID == "" {
		q, e := c.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: human}, Title: "Choose the checkpoint example report format", Choices: []string{"Brief", "Detailed"}}, "checkpoint/format/v1")
		if e != nil {
			return e
		}
		state.RequestID = q.ID
		state.Stage = "waiting"
		if e = save(*statePath, state); e != nil {
			return e
		}
	}
	fmt.Println("Durable request:", state.RequestID)
	if *pause {
		return nil
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			_ = c.Heartbeat(heartbeatCtx, map[string]any{"stage": "waiting for durable decision"})
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	q, e := c.Wait(ctx, state.RequestID)
	if e != nil {
		return e
	}
	if q.State != "resolved" || q.Response == nil || q.Response.Outcome != "answered" || q.Response.SourceInvalidated {
		return fmt.Errorf("request ended without a current usable answer: %s", q.State)
	}
	answer, ok := q.Response.Answer.(string)
	if !ok {
		return fmt.Errorf("expected a text choice")
	}
	state.Answer = answer
	state.Stage = "result-ready"
	if e = save(*statePath, state); e != nil {
		return e
	}
	// The only side effect is an idempotent message. For real effects, enforce permits at the tool boundary
	// and use a destination idempotency key or reconciliation. A checkpoint alone is not exactly-once execution.
	_, e = c.Send(ctx, notif.SendInput{ConversationID: q.ConversationID, Body: "Checkpoint workflow resumed and finished with format: " + strings.TrimSpace(answer)}, "checkpoint/result/v1")
	if e != nil {
		return e
	}
	state.Stage = "finished"
	if e = save(*statePath, state); e != nil {
		return e
	}
	fmt.Println("Finished; durable result delivered once.")
	return nil
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := run(ctx, os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
