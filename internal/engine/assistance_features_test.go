package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func boolValue(v bool) *bool { return &v }
func enableAssistance(t *testing.T, s *Store, a Actor, automatic, overseer bool) {
	t.Helper()
	if _, err := s.UpdateAssistance(context.Background(), a, AssistanceUpdate{AutomaticAnswers: boolValue(automatic), Overseer: boolValue(overseer)}); err != nil {
		t.Fatal(err)
	}
}
func publishedFixture(t *testing.T, s *Store, a Actor) (Actor, SharedAnswer, AskInput) {
	t.Helper()
	ctx := context.Background()
	worker := testWorker(t, s, a, "research/scout")
	in := AskInput{Target: Target{"person", a.ID}, Title: "Should prices include GST?", Body: "India report for fiscal year 2026.", Choices: []string{"Yes", "No"}, SharedKey: "reports/india/2026/v1"}
	source, err := s.Ask(ctx, worker, in, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, a, source.ID, Response{Outcome: "answered", Answer: "Yes", Version: 1}); err != nil {
		t.Fatal(err)
	}
	answer, err := s.PublishAnswer(ctx, a, PublishInput{RequestID: source.ID, Audience: []string{worker.ID}})
	if err != nil {
		t.Fatal(err)
	}
	in.Title = "For this report, are prices tax-inclusive?"
	return worker, answer, in
}
func TestAssistanceDefaultsAndOwnerControls(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	worker, answer, in := publishedFixture(t, s, a)
	var calls atomic.Int32
	s.AutomaticMatcher = func(context.Context, Request, []SharedAnswer) (AutomaticMatch, error) {
		calls.Add(1)
		return AutomaticMatch{answer.ID, true, 1, "Same scoped question"}, nil
	}
	settings, err := s.AssistanceSettings(ctx, a)
	if err != nil || settings.AutomaticAnswers || settings.Overseer {
		t.Fatal(settings, err)
	}
	q, err := s.Ask(ctx, worker, in, "off")
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := s.ProcessAutomaticAnswer(ctx); err != nil || worked || calls.Load() != 0 {
		t.Fatal("disabled feature called a model", worked, err)
	}
	if _, err = s.UpdateAssistance(ctx, worker, AssistanceUpdate{AutomaticAnswers: boolValue(true)}); ErrorStatus(err) != 403 {
		t.Fatal(err)
	}
	enableAssistance(t, s, a, true, false)
	if _, err = s.PlanOverseer(ctx, a, OverseerInput{Prompt: "Status?"}); ErrorStatus(err) != 409 {
		t.Fatal(err)
	}
	q, err = s.Request(ctx, worker, q.ID)
	if err != nil || q.State != "open" {
		t.Fatal(q, err)
	}
}
func TestAutomaticAnswersDurableScopedProvenance(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	worker, answer, in := publishedFixture(t, s, a)
	s.AutomaticMatcher = func(_ context.Context, q Request, candidates []SharedAnswer) (AutomaticMatch, error) {
		if len(candidates) != 1 || candidates[0].ID != answer.ID {
			t.Error("wrong candidates", candidates)
		}
		return AutomaticMatch{answer.ID, true, 0.995, "Same tax convention, resource and year."}, nil
	}
	enableAssistance(t, s, a, true, false)
	q, err := s.Ask(ctx, worker, in, "automatic")
	if err != nil || q.State != "open" {
		t.Fatal(q, err)
	}
	if worked, err := s.ProcessAutomaticAnswer(ctx); err != nil || !worked {
		t.Fatal(worked, err)
	}
	q, err = s.Request(ctx, worker, q.ID)
	if err != nil || q.State != "resolved" || q.Response.SourceAnswerID != answer.ID || q.Response.ReuseMethod != "automatic" || q.Response.Answer != "Yes" {
		t.Fatal(q, err)
	}
	if worked, err := s.ProcessAutomaticAnswer(ctx); err != nil || worked {
		t.Fatal("job replayed", worked, err)
	}
	if err = s.InvalidateAnswer(ctx, a, answer.ID); err != nil {
		t.Fatal(err)
	}
	q, err = s.Request(ctx, worker, q.ID)
	if err != nil || !q.Response.SourceInvalidated {
		t.Fatal(q, err)
	}
}
func TestAutomaticAnswersRejectUnsafeMatchesAndInFlightChanges(t *testing.T) {
	for _, scenario := range []string{"invented", "uncertain", "abstain", "provider failure", "different key", "different choices", "invalidated", "expired", "publisher disabled", "requester disabled", "credential revoked", "requester permission removed", "disabled during call", "human answered"} {
		t.Run(scenario, func(t *testing.T) {
			s, a := testStore(t)
			ctx := context.Background()
			worker, answer, in := publishedFixture(t, s, a)
			s.AutomaticMatcher = func(context.Context, Request, []SharedAnswer) (AutomaticMatch, error) { return AutomaticMatch{}, nil }
			enableAssistance(t, s, a, true, false)
			if scenario == "different key" {
				in.SharedKey = "another/resource/version"
			}
			if scenario == "different choices" {
				in.Choices = []string{"Include", "Exclude"}
			}
			q, err := s.Ask(ctx, worker, in, "question")
			if err != nil {
				t.Fatal(err)
			}
			s.AutomaticMatcher = func(_ context.Context, _ Request, _ []SharedAnswer) (AutomaticMatch, error) {
				result := AutomaticMatch{answer.ID, true, 1, "Exact same informational meaning in the same scope."}
				switch scenario {
				case "invented":
					result.AnswerID = "private-answer-not-in-candidates"
				case "uncertain":
					result.Confidence = 0.9
				case "abstain":
					result.Equivalent = false
				case "provider failure":
					return result, errors.New("provider unavailable")
				case "invalidated":
					if err := s.InvalidateAnswer(ctx, a, answer.ID); err != nil {
						t.Error(err)
					}
				case "expired":
					_, err := s.Pool.Exec(ctx, `UPDATE shared_answers SET valid_until=now()-interval '1 second' WHERE id=$1`, answer.ID)
					if err != nil {
						t.Error(err)
					}
				case "publisher disabled":
					_, err := s.Pool.Exec(ctx, `UPDATE principals SET disabled=true WHERE id=$1`, a.ID)
					if err != nil {
						t.Error(err)
					}
				case "requester disabled":
					_, err := s.Pool.Exec(ctx, `UPDATE principals SET disabled=true WHERE id=$1`, worker.ID)
					if err != nil {
						t.Error(err)
					}
				case "credential revoked":
					_, err := s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE id=$1`, worker.CredentialID)
					if err != nil {
						t.Error(err)
					}
				case "requester permission removed":
					_, err := s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE subject_id=$1`, worker.ID)
					if err != nil {
						t.Error(err)
					}
				case "disabled during call":
					if _, err := s.UpdateAssistance(ctx, a, AssistanceUpdate{AutomaticAnswers: boolValue(false)}); err != nil {
						t.Error(err)
					}
				case "human answered":
					if _, err := s.Respond(ctx, a, q.ID, Response{Outcome: "answered", Answer: "No", Version: 1}); err != nil {
						t.Error(err)
					}
				}
				return result, nil
			}
			if _, err = s.ProcessAutomaticAnswer(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := scanRequest(s.Pool.QueryRow(ctx, `SELECT `+requestFields+` FROM requests r WHERE r.scope=$1 AND r.id=$2`, a.Scope, q.ID))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "human answered" {
				if current.Response.Answer != "No" || current.Response.SourceAnswerID != "" {
					t.Fatal(current)
				}
			} else if current.State != "open" {
				t.Fatal("unsafe automatic resolution", current)
			}
		})
	}
}
func TestAutomaticAnswersRespectCurrentResponderAndGroupRights(t *testing.T) {
	for _, change := range []string{"responder rights", "group membership", "publisher rights"} {
		t.Run(change, func(t *testing.T) {
			s, owner := testStore(t)
			ctx := context.Background()
			human := testPerson(t, s, owner, "reviewer")
			w := testWorker(t, s, owner, "worker")
			replyRole := grant(t, s, owner, human, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID}, {Action: "message", TargetKind: "agent", TargetID: w.ID}})
			publishRole := grant(t, s, owner, human, []Rule{{Action: "manage", TargetKind: "answer", TargetID: "*"}})
			group, err := s.SaveGroup(ctx, owner, Group{Name: "Reviewers", Members: []string{human.ID}})
			if err != nil {
				t.Fatal(err)
			}
			in := AskInput{Target: Target{"group", group.ID}, Title: "Include GST?", SharedKey: "policy/one"}
			source, err := s.Ask(ctx, w, in, "source")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Respond(ctx, human, source.ID, Response{Outcome: "answered", Answer: "Yes", Version: 1}); err != nil {
				t.Fatal(err)
			}
			answer, err := s.PublishAnswer(ctx, human, PublishInput{RequestID: source.ID, Audience: []string{w.ID}})
			if err != nil {
				t.Fatal(err)
			}
			s.AutomaticMatcher = func(context.Context, Request, []SharedAnswer) (AutomaticMatch, error) { return AutomaticMatch{}, nil }
			enableAssistance(t, s, owner, true, false)
			in.Title = "Are prices GST-inclusive?"
			q, err := s.Ask(ctx, w, in, "future")
			if err != nil {
				t.Fatal(err)
			}
			s.AutomaticMatcher = func(context.Context, Request, []SharedAnswer) (AutomaticMatch, error) {
				var err error
				switch change {
				case "responder rights":
					err = s.BindRole(ctx, owner, Binding{replyRole.ID, "principal", human.ID}, true)
				case "publisher rights":
					err = s.BindRole(ctx, owner, Binding{publishRole.ID, "principal", human.ID}, true)
				case "group membership":
					_, err = s.SaveGroup(ctx, owner, Group{ID: group.ID, Name: group.Name, Members: []string{}})
				}
				if err != nil {
					t.Error(err)
				}
				return AutomaticMatch{answer.ID, true, 1, "Same question"}, nil
			}
			if _, err = s.ProcessAutomaticAnswer(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := s.Request(ctx, w, q.ID)
			if err != nil || current.State != "open" {
				t.Fatal("revoked authority reused", current, err)
			}
		})
	}
}
func TestAutomaticAnswerLeaseFencesCompetingWorkers(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w, answer, in := publishedFixture(t, s, a)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.AutomaticMatcher = func(context.Context, Request, []SharedAnswer) (AutomaticMatch, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return AutomaticMatch{answer.ID, true, 1, "Same question"}, nil
	}
	enableAssistance(t, s, a, true, false)
	q, err := s.Ask(ctx, w, in, "race")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.ProcessAutomaticAnswer(ctx); done <- err }()
	<-started
	if _, err = s.Pool.Exec(ctx, `UPDATE answer_match_jobs SET lease_until=now()-interval '1 second' WHERE request_id=$1`, q.ID); err != nil {
		t.Fatal(err)
	}
	if worked, err := s.ProcessAutomaticAnswer(ctx); err != nil || !worked {
		t.Fatal(worked, err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE scope=$1 AND operation_id=$2`, a.Scope, q.ID+"/result").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestOverseerPreviewAtomicConfirmationAndPrivateReplies(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "one")
	two := testWorker(t, s, owner, "two")
	s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
		return OverseerDecision{Operation: "question", AgentIDs: []string{one.ID, two.ID}, Body: "What have you finished?", Summary: "Ask both agents for their progress."}, nil
	}
	if _, err := s.PlanOverseer(ctx, owner, OverseerInput{Prompt: "Ask the fleet for progress"}); ErrorStatus(err) != 409 {
		t.Fatal(err)
	}
	enableAssistance(t, s, owner, false, true)
	plan, err := s.PlanOverseer(ctx, owner, OverseerInput{Prompt: "Ask the fleet for progress"})
	if err != nil || !plan.RequiresConfirmation {
		t.Fatal(plan, err)
	}
	var count int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM requests`).Scan(&count)
	if count != 0 {
		t.Fatal("preview sent work")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.ConfirmOverseer(ctx, owner, plan.ID); results <- err }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	plan, err = s.OverseerPlan(ctx, owner, plan.ID)
	if err != nil || len(plan.Dispatches) != 2 || plan.RequiresConfirmation {
		t.Fatal(plan, err)
	}
	for i, w := range []Actor{one, two} {
		q := plan.Replies[i]
		if _, err = s.Respond(ctx, w, q.ID, Response{Outcome: "answered", Answer: w.Name + " finished a report", Version: 1}); err != nil {
			t.Fatal(err)
		}
	}
	plan, err = s.OverseerPlan(ctx, owner, plan.ID)
	if err != nil || len(plan.Replies) != 2 {
		t.Fatal(plan, err)
	}
	for _, q := range plan.Replies {
		if q.Response == nil || q.RequesterID != owner.ID {
			t.Fatal(q)
		}
	}
	stranger := testPerson(t, s, owner, "stranger")
	if _, err = s.OverseerPlan(ctx, stranger, plan.ID); ErrorStatus(err) != 404 {
		t.Fatal("plan leaked", err)
	}
}
func TestOverseerRechecksPermissionsAndDoesNotTrustModel(t *testing.T) {
	for _, change := range []string{"invented target", "approval operation", "view revoked during model", "send revoked before confirm", "disabled before confirm", "settings disabled", "expired preview", "group removed", "partial rollback", "revoked read"} {
		t.Run(change, func(t *testing.T) {
			s, owner := testStore(t)
			ctx := context.Background()
			one := testWorker(t, s, owner, "one")
			two := testWorker(t, s, owner, "two")
			person := testPerson(t, s, owner, "operator")
			role := grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "message", TargetKind: "agent", TargetID: "*"}})
			group, err := s.SaveGroup(ctx, owner, Group{Name: "Fleet", Members: []string{one.ID, two.ID}})
			if err != nil {
				t.Fatal(err)
			}
			s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) { return OverseerDecision{}, nil }
			enableAssistance(t, s, owner, false, true)
			s.OverseerPlanner = func(_ context.Context, _ string, sources []FleetStatus) (OverseerDecision, error) {
				d := OverseerDecision{Operation: "message", AgentIDs: []string{one.ID, two.ID}, Body: "Use the revised report format.", Summary: "Notify the fleet."}
				if change == "invented target" {
					d.AgentIDs = []string{"hidden-agent"}
				}
				if change == "approval operation" {
					d.Operation = "approve"
				}
				if change == "view revoked during model" {
					if err := s.BindRole(ctx, owner, Binding{role.ID, "principal", person.ID}, true); err != nil {
						t.Error(err)
					}
				}
				return d, nil
			}
			plan, err := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Tell the fleet to use the revised format", Target: &Target{"group", group.ID}})
			if change == "invented target" || change == "approval operation" || change == "view revoked during model" {
				if err == nil {
					t.Fatal("unsafe plan accepted", plan)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "send revoked before confirm", "revoked read":
				_, err = s.SaveRole(ctx, owner, Role{ID: role.ID, Name: role.Name, Rules: []Rule{{Action: "view", TargetKind: "agent", TargetID: one.ID}}})
			case "disabled before confirm":
				_, err = s.Pool.Exec(ctx, `UPDATE principals SET disabled=true WHERE id=$1`, person.ID)
			case "settings disabled":
				_, err = s.UpdateAssistance(ctx, owner, AssistanceUpdate{Overseer: boolValue(false)})
			case "expired preview":
				plan.ExpiresAt = time.Now().Add(-time.Second)
				_, err = s.Pool.Exec(ctx, `UPDATE overseer_plans SET plan=$2 WHERE id=$1`, plan.ID, JSON(plan))
			case "group removed":
				_, err = s.SaveGroup(ctx, owner, Group{ID: group.ID, Name: group.Name, Members: []string{one.ID}})
			case "partial rollback":
				_, err = s.SaveRole(ctx, owner, Role{ID: role.ID, Name: role.Name, Rules: []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "message", TargetKind: "agent", TargetID: one.ID}}})
			}
			if err != nil {
				t.Fatal(err)
			}
			if change == "revoked read" {
				if _, err = s.OverseerPlan(ctx, person, plan.ID); err == nil {
					t.Fatal("derived summary survived revocation")
				}
				return
			}
			if _, err = s.ConfirmOverseer(ctx, person, plan.ID); err == nil {
				t.Fatal("unsafe confirmation succeeded")
			}
			var count int
			_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&count)
			if count != 0 {
				t.Fatal("partial or unauthorized instruction escaped", count)
			}
		})
	}
}

func TestOverseerPermissionIntersectionAndReplySummary(t *testing.T) {
	for _, operation := range []string{"status", "question", "task", "message"} {
		t.Run(operation, func(t *testing.T) {
			s, owner := testStore(t)
			ctx := context.Background()
			one := testWorker(t, s, owner, "visible")
			hidden := testWorker(t, s, owner, "hidden")
			person := testPerson(t, s, owner, "operator")
			grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: one.ID}, {Action: "ask", TargetKind: "agent", TargetID: one.ID}, {Action: "message", TargetKind: "agent", TargetID: one.ID}})
			s.OverseerPlanner = func(_ context.Context, _ string, sources []FleetStatus) (OverseerDecision, error) {
				if len(sources) != 1 || sources[0].Agent.ID != one.ID {
					t.Error("unauthorized provider context", sources, hidden.ID)
				}
				return OverseerDecision{Operation: operation, AgentIDs: []string{one.ID}, Body: "Report progress", Summary: "Only the permitted agent is included."}, nil
			}
			enableAssistance(t, s, owner, false, true)
			plan, err := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Get my team's update"})
			if err != nil {
				t.Fatal(err)
			}
			if operation == "status" {
				if plan.RequiresConfirmation {
					t.Fatal(plan)
				}
				return
			}
			plan, err = s.ConfirmOverseer(ctx, person, plan.ID)
			if err != nil || len(plan.Dispatches) != 1 {
				t.Fatal(plan, err)
			}
			if operation == "message" {
				return
			}
			q, err := s.Request(ctx, one, plan.Dispatches[0].RequestID)
			if err != nil {
				t.Fatal(err)
			}
			// A worker's reply is separately authorized; viewing a user never grants reply rights.
			if _, err = s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE subject_id=$1`, one.ID); err != nil {
				t.Fatal(err)
			}
			grant(t, s, owner, one, []Rule{{Action: "view", TargetKind: "person", TargetID: person.ID}})
			outcome := "answered"
			if operation == "task" {
				outcome = "completed"
			}
			if _, err = s.Respond(ctx, one, q.ID, Response{Outcome: outcome, Answer: "Completed 7 non-overlapping pages", Version: 1}); ErrorStatus(err) != 403 {
				t.Fatal("view allowed a reply", err)
			}
			grant(t, s, owner, one, []Rule{{Action: "message", TargetKind: "person", TargetID: person.ID}})
			if _, err = s.Respond(ctx, one, q.ID, Response{Outcome: outcome, Answer: "Completed 7 non-overlapping pages", Version: 1}); err != nil {
				t.Fatal(err)
			}
			var calls int
			s.OverseerSummarizer = func(_ context.Context, _ string, replies []OverseerReply) (string, error) {
				calls++
				if len(replies) != 1 || replies[0].AgentID != one.ID || replies[0].Answer != "Completed 7 non-overlapping pages" {
					t.Error(replies)
				}
				return "Visible agent reports seven pages complete.", nil
			}
			plan, err = s.SummarizeOverseer(ctx, person, plan.ID)
			if err != nil || plan.ReplySummary == "" {
				t.Fatal(plan, err)
			}
			if _, err = s.SummarizeOverseer(ctx, person, plan.ID); err != nil || calls != 1 {
				t.Fatal("summary not cached", calls, err)
			}
			recent, err := s.OverseerPlans(ctx, person)
			if err != nil || len(recent) != 1 || recent[0].ReplySummary == "" {
				t.Fatal(recent, err)
			}
			if _, err = s.PlanOverseer(ctx, one, OverseerInput{Prompt: "Impersonate operator"}); ErrorStatus(err) != 403 {
				t.Fatal(err)
			}
			other, err := s.Bootstrap(ctx, "other/dev", "Other", "dev", Secret())
			if err != nil {
				t.Fatal(err)
			}
			enableAssistance(t, s, other, false, true)
			if _, err = s.OverseerPlan(ctx, other, plan.ID); ErrorStatus(err) != 404 {
				t.Fatal("cross workspace plan", err)
			}
		})
	}
}

func TestOverseerDeniesMissingAskAndRevokedContext(t *testing.T) {
	for _, change := range []string{"ask missing", "group changed during model", "nonselected context revoked", "summary view revoked", "summary credential revoked", "summary settings disabled", "reply changed during summary", "caller revoked before provider"} {
		t.Run(change, func(t *testing.T) {
			s, owner := testStore(t)
			ctx := context.Background()
			one := testWorker(t, s, owner, "one")
			two := testWorker(t, s, owner, "two")
			person := testPerson(t, s, owner, "reader")
			role := grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "ask", TargetKind: "agent", TargetID: "*"}})
			group, err := s.SaveGroup(ctx, owner, Group{Name: "Team", Members: []string{one.ID, two.ID}})
			if err != nil {
				t.Fatal(err)
			}
			s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) { return OverseerDecision{}, nil }
			enableAssistance(t, s, owner, false, true)
			if change == "ask missing" {
				_, err = s.SaveRole(ctx, owner, Role{ID: role.ID, Name: role.Name, Rules: []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}}})
			}
			if change == "caller revoked before provider" {
				_, err = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE id=$1`, person.CredentialID)
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
				calls++
				if change == "group changed during model" {
					_, err = s.SaveGroup(ctx, owner, Group{ID: group.ID, Name: group.Name, Members: []string{one.ID}})
					if err != nil {
						t.Error(err)
					}
				}
				return OverseerDecision{Operation: "question", AgentIDs: []string{one.ID}, Body: "How far along?", Summary: "Ask the first agent."}, nil
			}
			plan, err := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Ask the team", Target: &Target{"group", group.ID}})
			if change == "ask missing" || change == "group changed during model" || change == "caller revoked before provider" {
				if err == nil {
					t.Fatal("denied context accepted", plan)
				}
				if change == "caller revoked before provider" && calls != 0 {
					t.Fatal("revoked caller reached model")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if change == "nonselected context revoked" {
				_, err = s.SaveRole(ctx, owner, Role{ID: role.ID, Name: role.Name, Rules: []Rule{{Action: "view", TargetKind: "agent", TargetID: one.ID}, {Action: "ask", TargetKind: "agent", TargetID: one.ID}}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.ConfirmOverseer(ctx, person, plan.ID); err == nil {
					t.Fatal("hidden nonselected context retained")
				}
				return
			}
			plan, err = s.ConfirmOverseer(ctx, person, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			qid := plan.Dispatches[0].RequestID
			s.OverseerSummarizer = func(context.Context, string, []OverseerReply) (string, error) {
				switch change {
				case "summary view revoked":
					_, err = s.SaveRole(ctx, owner, Role{ID: role.ID, Name: role.Name, Rules: []Rule{}})
				case "summary credential revoked":
					_, err = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE id=$1`, person.CredentialID)
				case "summary settings disabled":
					_, err = s.UpdateAssistance(ctx, owner, AssistanceUpdate{Overseer: boolValue(false)})
				case "reply changed during summary":
					_, err = s.Respond(ctx, one, qid, Response{Outcome: "answered", Answer: "Now complete", Version: 1})
				}
				if err != nil {
					t.Error(err)
				}
				return "Unsafe stale summary", nil
			}
			if _, err = s.SummarizeOverseer(ctx, person, plan.ID); err == nil {
				t.Fatal("stale summary committed")
			}
			var summary string
			if err = s.Pool.QueryRow(ctx, `SELECT reply_summary FROM overseer_plans WHERE id=$1`, plan.ID).Scan(&summary); err != nil || summary != "" {
				t.Fatal(summary, err)
			}
		})
	}
}

func TestAssistanceDurableRestartAndRestoreHold(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	w, answer, in := publishedFixture(t, s, owner)
	matcher := func(context.Context, Request, []SharedAnswer) (AutomaticMatch, error) {
		return AutomaticMatch{answer.ID, true, 1, "Same scoped informational question"}, nil
	}
	planner := func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
		return OverseerDecision{Operation: "status", Summary: "One permitted agent."}, nil
	}
	s.AutomaticMatcher = matcher
	s.OverseerPlanner = planner
	enableAssistance(t, s, owner, true, true)
	q, err := s.Ask(ctx, w, in, "restart")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.PlanOverseer(ctx, owner, OverseerInput{Prompt: "Fleet status"})
	if err != nil {
		t.Fatal(err)
	}
	// A fresh runtime has no in-memory job, setting or plan state from the first process.
	schema := strings.Split(s.Pool.Config().ConnConfig.RuntimeParams["search_path"], ",")[0]
	second, err := OpenExisting(ctx, os.Getenv("NOTIF_TEST_DATABASE_URL"), schema, s.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.AutomaticMatcher = matcher
	second.OverseerPlanner = planner
	settings, err := second.AssistanceSettings(ctx, owner)
	if err != nil || !settings.AutomaticAnswers || !settings.Overseer {
		t.Fatal(settings, err)
	}
	if _, err = second.ProcessAutomaticAnswer(ctx); err != nil {
		t.Fatal(err)
	}
	q, err = second.Request(ctx, w, q.ID)
	if err != nil || q.Response == nil || q.Response.SourceAnswerID != answer.ID {
		t.Fatal(q, err)
	}
	if got, err := second.OverseerPlan(ctx, owner, plan.ID); err != nil || got.ID != plan.ID {
		t.Fatal(got, err)
	}
	in.Title = "Another tax-inclusive report question"
	if _, err = second.Ask(ctx, w, in, "before-restore"); err != nil {
		t.Fatal(err)
	}
	if err = second.ReconcileRestored(ctx, owner.Scope, Secret()); err != nil {
		t.Fatal(err)
	}
	settings, err = second.AssistanceSettings(ctx, owner)
	if err != nil || settings.AutomaticAnswers || settings.Overseer {
		t.Fatal(settings, err)
	}
	if worked, err := second.ProcessAutomaticAnswer(ctx); err != nil || worked {
		t.Fatal("restored job replayed", worked, err)
	}
	var pending int
	if err = second.Pool.QueryRow(ctx, `SELECT count(*) FROM answer_match_jobs WHERE state IN ('queued','leased')`).Scan(&pending); err != nil || pending != 0 {
		t.Fatal(pending, err)
	}
}

func TestAutomaticAnswersAcceptEquivalentEmptyChoiceRepresentations(t *testing.T) {
	for _, choices := range [][]string{nil, {}} {
		s, owner := testStore(t)
		ctx := context.Background()
		worker := testWorker(t, s, owner, "researcher")
		source, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Title: "Which locale is our report using?", SharedKey: "report/locale/v1", Choices: choices}, "source")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Respond(ctx, owner, source.ID, Response{Outcome: "answered", Answer: "English", Version: 1}); err != nil {
			t.Fatal(err)
		}
		answer, err := s.PublishAnswer(ctx, owner, PublishInput{RequestID: source.ID, Audience: []string{worker.ID}})
		if err != nil {
			t.Fatal(err)
		}
		s.AutomaticMatcher = func(_ context.Context, _ Request, candidates []SharedAnswer) (AutomaticMatch, error) {
			if len(candidates) != 1 {
				t.Error(candidates)
			}
			return AutomaticMatch{answer.ID, true, 1, "Same report locale question"}, nil
		}
		enableAssistance(t, s, owner, true, false)
		future := []string{}
		if choices != nil {
			future = nil
		}
		q, err := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Title: "What language should be used for the report?", SharedKey: "report/locale/v1", Choices: future}, "future")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.ProcessAutomaticAnswer(ctx); err != nil {
			t.Fatal(err)
		}
		q, err = s.Request(ctx, worker, q.ID)
		if err != nil || q.State != "resolved" {
			t.Fatal(q, err)
		}
	}
}
