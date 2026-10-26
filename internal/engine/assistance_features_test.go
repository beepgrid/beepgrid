package engine

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func boolValue(v bool) *bool { return &v }
func enableAssistance(t *testing.T, s *Store, a Actor, overseer bool) {
	t.Helper()
	if _, err := s.UpdateAssistance(context.Background(), a, AssistanceUpdate{Overseer: boolValue(overseer)}); err != nil {
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
	enableAssistance(t, s, owner, true)
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
			enableAssistance(t, s, owner, true)
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
			enableAssistance(t, s, owner, true)
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
			enableAssistance(t, s, other, true)
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
			enableAssistance(t, s, owner, true)
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

func TestAssistanceDefaultsAndOwnerControls(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "scout")
	settings, err := s.AssistanceSettings(ctx, owner)
	if err != nil || settings.Overseer || settings.OverseerConfigured {
		t.Fatal(settings, err)
	}
	if _, err = s.UpdateAssistance(ctx, worker, AssistanceUpdate{Overseer: boolValue(true)}); ErrorStatus(err) != 403 {
		t.Fatal(err)
	}
	if _, err = s.UpdateAssistance(ctx, owner, AssistanceUpdate{Overseer: boolValue(true)}); ErrorStatus(err) != 409 {
		t.Fatal("unconfigured Coordinator enabled", err)
	}
	if _, err = s.PlanOverseer(ctx, owner, OverseerInput{Prompt: "Status?"}); ErrorStatus(err) != 409 {
		t.Fatal(err)
	}
}

func TestCoordinatorDurableRestartAndRestoreHold(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	testWorker(t, s, owner, "scout")
	planner := func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
		return OverseerDecision{Operation: "status", Summary: "One permitted agent."}, nil
	}
	s.OverseerPlanner = planner
	enableAssistance(t, s, owner, true)
	plan, err := s.PlanOverseer(ctx, owner, OverseerInput{Prompt: "Fleet status"})
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.Split(s.Pool.Config().ConnConfig.RuntimeParams["search_path"], ",")[0]
	second, err := OpenExisting(ctx, os.Getenv("NOTIF_TEST_DATABASE_URL"), schema, s.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.OverseerPlanner = planner
	settings, err := second.AssistanceSettings(ctx, owner)
	if err != nil || !settings.Overseer {
		t.Fatal(settings, err)
	}
	if got, err := second.OverseerPlan(ctx, owner, plan.ID); err != nil || got.ID != plan.ID {
		t.Fatal(got, err)
	}
	if err = second.ReconcileRestored(ctx, owner.Scope, Secret()); err != nil {
		t.Fatal(err)
	}
	settings, err = second.AssistanceSettings(ctx, owner)
	if err != nil || settings.Overseer {
		t.Fatal(settings, err)
	}
	if _, err = second.OverseerPlan(ctx, owner, plan.ID); ErrorStatus(err) < 400 {
		t.Fatal("restored plan accessible", err)
	}
}
