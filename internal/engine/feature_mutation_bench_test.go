package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// External model/provider service time is excluded by deterministic local
// fixtures. Complete database mutations, authorization, notifications, fsync,
// lease acknowledgements and provider HTTP calls remain in the timed region.
func BenchmarkFeatureMutations(b *testing.B) {
	for _, n := range []int{1, 30, 100} {
		b.Run(fmt.Sprint("Scale", n), func(b *testing.B) {
			s, owner := testStore(b)
			ctx := context.Background()
			s.AttachmentsRoot = b.TempDir()
			s.AllowLocal = true
			workers := []Actor{}
			ids := []string{}
			for i := range n {
				w := testWorker(b, s, owner, fmt.Sprint("mutation-", i))
				workers = append(workers, w)
				ids = append(ids, w.ID)
			}
			group, e := s.SaveGroup(ctx, owner, Group{Name: "Fleet", Members: ids})
			if e != nil {
				b.Fatal(e)
			}
			q, e := s.Ask(ctx, workers[0], AskInput{Target: Target{"person", owner.ID}, Title: "Tax policy?", SharedKey: "mutation/policy"}, "source")
			if e != nil {
				b.Fatal(e)
			}
			if _, e = s.Respond(ctx, owner, q.ID, Response{Outcome: "answered", Answer: "Inclusive", Version: 1}); e != nil {
				b.Fatal(e)
			}
			s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
				return OverseerDecision{Operation: "message", AgentIDs: ids[:min(n, 2)], Body: "Review release blockers", Summary: "Contact selected agents"}, nil
			}
			on := true
			if _, e = s.UpdateAssistance(ctx, owner, AssistanceUpdate{Overseer: &on}); e != nil {
				b.Fatal(e)
			}
			coordinatorGroup, e := s.SaveGroup(ctx, owner, Group{Name: "Coordinator scope", Members: ids[:min(n, 30)]})
			if e != nil {
				b.Fatal(e)
			}
			key, e := s.NewCredential(ctx, owner, workers[0].ID, "api", "")
			if e != nil {
				b.Fatal(e)
			}
			token := key["token"].(string)
			var calls atomic.Int64
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusAccepted) }))
			defer provider.Close()
			conn, e := s.SaveConnection(ctx, owner, Connection{Name: "Local fixture", Kind: "webhook", Enabled: true, Config: map[string]string{"url": provider.URL}, Secret: "feature-fixture-secret"})
			if e != nil {
				b.Fatal(e)
			}
			if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: conn.ID, PrincipalID: owner.ID, Address: "local-fixture"}); e != nil {
				b.Fatal(e)
			}
			counter := featureCounter(b, s)
			for _, name := range []string{"Authenticate", "Heartbeat", "Access", "SaveGroup", "PublishAudience", "GroupQuestion", "GroupTask", "GroupDecision", "InboxLeaseAck", "Upload256KiB", "CoordinatorConfirm", "ProviderDispatch"} {
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					totalQueries := int64(0)
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						id := ID("mutation")
						var plan OverseerPlan
						var decision Request
						if name == "GroupDecision" {
							decision, e = s.Ask(ctx, owner, AskInput{Target: Target{"group", group.ID}, Title: "Which region?"}, id)
							if e != nil {
								b.Fatal(e)
							}
						}
						if name == "InboxLeaseAck" {
							if _, e = s.Pool.Exec(ctx, `UPDATE deliveries SET state='received' WHERE scope=$1 AND recipient_id=$2 AND connection_id=''`, owner.Scope, workers[0].ID); e != nil {
								b.Fatal(e)
							}
							if _, e = s.Send(ctx, owner, SendInput{Target: Target{"agent", workers[0].ID}, Body: "Review release"}, id); e != nil {
								b.Fatal(e)
							}
						}
						if name == "CoordinatorConfirm" {
							plan, e = s.PlanOverseer(ctx, owner, OverseerInput{Prompt: "Check blockers", Target: &Target{"group", coordinatorGroup.ID}})
							if e != nil {
								b.Fatal(e)
							}
						}
						if name == "ProviderDispatch" {
							if _, e = s.Pool.Exec(ctx, `UPDATE deliveries SET state='accepted' WHERE scope=$1 AND connection_id=$2`, owner.Scope, conn.ID); e != nil {
								b.Fatal(e)
							}
							for j := range n {
								if _, e = s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "Provider event"}, fmt.Sprintf("%s-%d", id, j)); e != nil {
									b.Fatal(e)
								}
							}
							calls.Store(0)
						}
						counter.total.Store(0)
						b.StartTimer()
						switch name {
						case "Authenticate":
							_, e = s.Authenticate(ctx, token)
						case "Heartbeat":
							e = s.Heartbeat(ctx, workers[0], map[string]any{"phase": "review", "files": 3})
						case "Access":
							_, e = s.Access(ctx, workers[0], "")
						case "SaveGroup":
							_, e = s.SaveGroup(ctx, owner, Group{Name: id, Members: ids})
						case "PublishAudience":
							_, e = s.PublishAnswer(ctx, owner, PublishInput{RequestID: q.ID, Audience: ids})
						case "GroupQuestion", "GroupTask":
							kind := "question"
							if name == "GroupTask" {
								kind = "task"
							}
							_, e = s.Ask(ctx, owner, AskInput{Kind: kind, Target: Target{"group", group.ID}, Title: "Review release blockers"}, id)
						case "InboxLeaseAck":
							var jobs []Delivery
							jobs, e = s.Inbox(ctx, workers[0])
							if e == nil {
								if len(jobs) != 1 {
									b.Fatal("lease count", len(jobs))
								}
								e = s.Ack(ctx, workers[0], jobs[0].ID, jobs[0].Generation)
							}
						case "GroupDecision":
							_, e = s.Respond(ctx, workers[0], decision.ID, Response{Outcome: "answered", Answer: "Europe", Version: 1})
						case "Upload256KiB":
							_, e = s.UploadAttachment(ctx, workers[0], q.ConversationID, "evidence.txt", "text/plain", []byte(strings.Repeat("x", 256*1024)))
						case "CoordinatorConfirm":
							var out OverseerPlan
							out, e = s.ConfirmOverseer(ctx, owner, plan.ID)
							if e == nil && len(out.Dispatches) != min(n, 2) {
								b.Fatal("dispatch count", len(out.Dispatches))
							}
						case "ProviderDispatch":
							var done int
							done, e = s.DispatchBatch(ctx, "http://localhost", n)
							if e == nil && (done != n || calls.Load() != int64(n)) {
								b.Fatal("provider count", done, calls.Load())
							}
						}
						b.StopTimer()
						totalQueries += counter.total.Load()
						if e != nil {
							b.Fatal(e)
						}
					}
					b.ReportMetric(float64(totalQueries)/float64(b.N), "queries/op")
				})
			}
		})
	}
}
