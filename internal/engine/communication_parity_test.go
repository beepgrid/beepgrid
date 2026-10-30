package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestExplicitBlockingCyclesAndResolution(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	agents := []Actor{testWorker(t, s, owner, "Builder 1"), testWorker(t, s, owner, "Billing 2"), testWorker(t, s, owner, "Reviewer 3")}
	requests := []Request{}
	for i, a := range agents {
		q, e := s.Ask(ctx, a, AskInput{Target: Target{"agent", agents[(i+1)%3].ID}, Kind: "question", Body: "I need this result before continuing"}, ID("key"))
		if e != nil {
			t.Fatal(e)
		}
		requests = append(requests, q)
	}
	status, e := s.FleetStatus(ctx, owner, nil)
	if e != nil || len(status["deadlocks"].([]AgentDeadlock)) != 0 {
		t.Fatal("ordinary waiting became a deadlock", status, e)
	}
	for i, q := range requests {
		if _, e = s.SetRequestBlocking(ctx, agents[i], q.ID, true); e != nil {
			t.Fatal(e)
		}
	}
	status, e = s.FleetStatus(ctx, owner, nil)
	if e != nil {
		t.Fatal(e)
	}
	cycles := status["deadlocks"].([]AgentDeadlock)
	if len(cycles) != 1 || len(cycles[0].Dependencies) != 3 {
		t.Fatal(cycles)
	}
	reader := testPerson(t, s, owner, "Restricted observer")
	grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "agent", TargetID: agents[0].ID}, {Action: "view", TargetKind: "agent", TargetID: agents[1].ID}})
	restricted, e := s.FleetStatus(ctx, reader, nil)
	if e != nil || len(restricted["deadlocks"].([]AgentDeadlock)) != 0 {
		t.Fatal("hidden cycle leaked", restricted, e)
	}
	if _, e = s.SetRequestBlocking(ctx, owner, requests[0].ID, false); ErrorStatus(e) != 403 {
		t.Fatal("admin silently changed an agent dependency", e)
	}
	if _, e = s.Respond(ctx, agents[1], requests[0].ID, Response{Version: 1, Outcome: "answered", Answer: "Commit 9f3 adds that header"}); e != nil {
		t.Fatal(e)
	}
	status, e = s.FleetStatus(ctx, owner, nil)
	if e != nil || len(status["deadlocks"].([]AgentDeadlock)) != 0 {
		t.Fatal("resolved dependency retained a deadlock", e)
	}
	if _, e = s.SetRequestBlocking(ctx, agents[1], requests[1].ID, false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Ask(ctx, owner, AskInput{Target: Target{"agent", agents[0].ID}, Kind: "question", Body: "Ordinary question", Blocking: true}, ID("key")); ErrorStatus(e) != 400 {
		t.Fatal("person created agent dependency", e)
	}
}
func TestSharedMentionsAddressWithoutRestrictingObservers(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder Agent 1")
	two := testWorker(t, s, owner, "DevOps Agent 2")
	chat, e := s.StartChat(ctx, owner, StartChatInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "Migration planning"}, "chat")
	if e != nil {
		t.Fatal(e)
	}
	body := "🙂 @DevOps Agent 2, keep concurrency at eight."
	mentions := []Mention{{PrincipalID: two.ID, Start: 2, End: 2 + len([]rune("@DevOps Agent 2"))}}
	input := SendInput{ConversationID: chat.ConversationID, Body: body, Mentions: mentions}
	sent, e := s.Send(ctx, owner, input, "mention")
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Send(ctx, owner, input, "mention")
	if e != nil || again.Messages[0].ID != sent.Messages[0].ID {
		t.Fatal(again, e)
	}
	for _, a := range []Actor{one, two} {
		var raw []byte
		if e = s.Pool.QueryRow(ctx, `SELECT payload FROM deliveries WHERE scope=$1 AND operation_id=$2 AND recipient_id=$3 AND connection_id=''`, owner.Scope, sent.Messages[0].ID, a.ID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		var payload struct {
			Message Message `json:"message"`
		}
		if e = json.Unmarshal(raw, &payload); e != nil {
			t.Fatal(e)
		}
		if payload.Message.Body != body || len(payload.Message.Mentions) != 1 || payload.Message.Mentions[0].PrincipalID != two.ID {
			t.Fatal("observer delivery changed", payload)
		}
		hint := MentionContext(payload.Message, a.ID)
		if a.ID == two.ID && hint != "You are explicitly addressed. Other participants can also contribute relevant context." {
			t.Fatal(hint)
		}
		c, e := s.Conversation(ctx, a, chat.ConversationID)
		if e != nil || len(c.Messages[1].Mentions) != 1 {
			t.Fatal(c, e)
		}
	}
	// An observer remains free to add useful context and does not gain authority.
	if _, e = s.Send(ctx, one, SendInput{ConversationID: chat.ConversationID, Body: "The new API client supports that limit."}, "observer"); e != nil {
		t.Fatal(e)
	}
	stranger := testWorker(t, s, owner, "Outside agent")
	input.Mentions[0].PrincipalID = stranger.ID
	if _, e = s.Send(ctx, owner, input, "outside"); ErrorStatus(e) != 400 {
		t.Fatal("mention invited a nonmember", e)
	}
	input.Mentions[0].PrincipalID = two.ID
	input.Mentions[0].End += 2
	if _, e = s.Send(ctx, owner, input, "offset"); ErrorStatus(e) != 400 {
		t.Fatal("bad Unicode range accepted", e)
	}
}
func TestCoordinatorDistinctAutomaticDispatchAndDurableSynthesis(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 7")
	two := testWorker(t, s, owner, "DevOps 2")
	var calls, summaries atomic.Int32
	s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
		calls.Add(1)
		return OverseerDecision{Operation: "task", Summary: "Builder updates the client; DevOps confirms staging capacity.", Instructions: []OverseerInstruction{{one.ID, "task", "Migrate the worker client to the V2 endpoint."}, {two.ID, "question", "How many staging workers can run during the demo?"}}}, nil
	}
	s.OverseerSummarizer = func(_ context.Context, _ string, replies []OverseerReply) (string, error) {
		summaries.Add(1)
		if len(replies) != 2 || replies[0].State == "open" || replies[1].State == "open" {
			t.Fatal(replies)
		}
		return "The V2 client is ready; staging supports eight workers during the demo.", nil
	}
	enableAssistance(t, s, owner, true)
	in := OverseerInput{Prompt: "Update the client and check staging capacity", Automatic: true}
	// Recover a persisted planning receipt after a crash before dispatch.
	preview, e := s.PlanOverseer(ctx, owner, in, "auto")
	if e != nil || len(preview.Dispatches) != 0 {
		t.Fatal(preview, e)
	}
	plan, e := s.Coordinate(ctx, owner, in, "auto")
	if e != nil || plan.RequiresConfirmation || len(plan.Dispatches) != 2 {
		t.Fatal(plan, e)
	}
	again, e := s.Coordinate(ctx, owner, in, "auto")
	if e != nil || again.Dispatches[0].RequestID != plan.Dispatches[0].RequestID || calls.Load() != 1 {
		t.Fatal(again, e, calls.Load())
	}
	if n, e := s.ProcessCoordinatorSummary(ctx); e != nil || n != 0 {
		t.Fatal("summarized pending work", n, e)
	}
	for i, d := range plan.Dispatches {
		a := []Actor{one, two}[i]
		q, e := s.Request(ctx, a, d.RequestID)
		if e != nil {
			t.Fatal(e)
		}
		if q.Body != plan.Instructions[i].Body {
			t.Fatal("specialist got common instruction", q)
		}
		outcome, answer := "completed", any("V2 client ready")
		if i == 1 {
			outcome, answer = "answered", 8
		}
		if _, e = s.Respond(ctx, a, q.ID, Response{Version: q.Version, Outcome: outcome, Answer: answer}); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.ProcessCoordinatorSummary(ctx); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	result, e := s.OverseerPlan(ctx, owner, plan.ID)
	if e != nil || result.ReplySummary == "" || summaries.Load() != 1 {
		t.Fatal(result, e, summaries.Load())
	}
}

func TestCoordinatorSummaryRetryAndRevokedAuthority(t *testing.T) {
	for _, mode := range []string{"retry", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			s, owner := testStore(t)
			ctx := context.Background()
			worker := testWorker(t, s, owner, "DevOps 2")
			s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
				return OverseerDecision{Operation: "question", Summary: "Check capacity", Instructions: []OverseerInstruction{{worker.ID, "question", "How many workers fit?"}}}, nil
			}
			var calls atomic.Int32
			s.OverseerSummarizer = func(context.Context, string, []OverseerReply) (string, error) {
				if calls.Add(1) == 1 {
					return "", errors.New("temporary provider failure")
				}
				return "Eight workers fit.", nil
			}
			enableAssistance(t, s, owner, true)
			plan, e := s.Coordinate(ctx, owner, OverseerInput{Prompt: "Check capacity"}, "retry-plan")
			if e != nil {
				t.Fatal(e)
			}
			q, e := s.Request(ctx, worker, plan.Dispatches[0].RequestID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.Respond(ctx, worker, q.ID, Response{Version: q.Version, Outcome: "answered", Answer: 8}); e != nil {
				t.Fatal(e)
			}
			if mode == "revoked" {
				if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE scope=$1 AND id=$2`, owner.Scope, owner.CredentialID); e != nil {
					t.Fatal(e)
				}
			}
			n, e := s.ProcessCoordinatorSummary(ctx)
			if n != 1 || e == nil {
				t.Fatal(n, e)
			}
			var done bool
			var failure string
			if e = s.Pool.QueryRow(ctx, `SELECT summary_done,summary_error FROM overseer_plans WHERE scope=$1 AND id=$2`, owner.Scope, plan.ID).Scan(&done, &failure); e != nil || done || failure == "" {
				t.Fatal(done, failure, e)
			}
			if mode == "revoked" {
				if calls.Load() != 0 {
					t.Fatal("revoked authority reached model")
				}
				return
			}
			// The direct retry response must clear the old error immediately,
			// without relying on a later GET to repair stale UI state.
			direct, retryErr := s.SummarizeOverseer(ctx, owner, plan.ID)
			if retryErr != nil || direct.SummaryError != "" || direct.ReplySummary != "Eight workers fit." {
				t.Fatal(direct, retryErr)
			}
			if _, e = s.Pool.Exec(ctx, `UPDATE overseer_plans SET summary_next_at=now() WHERE scope=$1 AND id=$2`, owner.Scope, plan.ID); e != nil {
				t.Fatal(e)
			}
			if n, e = s.ProcessCoordinatorSummary(ctx); e != nil || n != 1 {
				t.Fatal(n, e)
			}
			recovered, e := s.OverseerPlan(ctx, owner, plan.ID)
			if e != nil || recovered.ReplySummary != "Eight workers fit." || recovered.SummaryError != "" || calls.Load() != 2 {
				t.Fatal(recovered, e, calls.Load())
			}
		})
	}
}
