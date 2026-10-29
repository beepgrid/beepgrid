package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This profile measures complete engine operations against real PostgreSQL.
// Fixture creation is excluded. It supplements, rather than replaces, the
// container HTTP/resource benchmark and the adversarial correctness suite.
func BenchmarkFeatureSurface(b *testing.B) {
	for _, n := range []int{1, 30, 100} {
		b.Run(fmt.Sprint("Scale", n), func(b *testing.B) {
			s, owner := testStore(b)
			s.AttachmentsRoot = b.TempDir()
			ctx := context.Background()
			reader := testPerson(b, s, owner, "feature-reader")
			grant(b, s, owner, reader, []Rule{
				{Action: "view", TargetKind: "*", TargetID: "*"},
				{Action: "message", TargetKind: "*", TargetID: "*"},
				{Action: "ask", TargetKind: "*", TargetID: "*"},
				{Action: "decide", TargetKind: "*", TargetID: "*"},
				{Action: "manage", TargetKind: "answer", TargetID: "$self"},
			})
			workers := []Actor{}
			ids := []string{}
			for i := range n {
				w := testWorker(b, s, owner, fmt.Sprint("feature-", i))
				workers = append(workers, w)
				ids = append(ids, w.ID)
				if _, err := s.SaveGroup(ctx, owner, Group{Name: fmt.Sprint("Team ", i), Members: []string{reader.ID, w.ID}}); err != nil {
					b.Fatal(err)
				}
			}
			group, err := s.SaveGroup(ctx, owner, Group{Name: "Coordinator scope", Members: ids[:min(n, 30)]})
			if err != nil {
				b.Fatal(err)
			}
			in := AskInput{Target: Target{"person", reader.ID}, Title: "Which regional tax policy applies?", SharedKey: "feature/region/v1"}
			source, err := s.Ask(ctx, workers[0], in, "source")
			if err != nil {
				b.Fatal(err)
			}
			if _, err = s.Respond(ctx, reader, source.ID, Response{Outcome: "answered", Answer: "Tax inclusive", Version: 1}); err != nil {
				b.Fatal(err)
			}
			for i := range n {
				if _, err = s.PublishAnswer(ctx, reader, PublishInput{RequestID: source.ID, Audience: ids, ValidUntil: time.Now().Add(time.Hour)}); err != nil {
					b.Fatal(err)
				}
				if q, e := s.Ask(ctx, workers[i], in, fmt.Sprint("reuse-", i)); e != nil || q.State != "resolved" || q.Response.SourceAnswerID == "" {
					b.Fatal(q, e)
				}
				if _, err = s.Send(ctx, workers[i], SendInput{Target: Target{"person", reader.ID}, Body: "Review the release evidence."}, fmt.Sprint("chat-", i)); err != nil {
					b.Fatal(err)
				}
				if _, err = s.UploadAttachment(ctx, workers[0], source.ConversationID, fmt.Sprintf("evidence-%d.txt", i), "text/plain", []byte("Release evidence")); err != nil {
					b.Fatal(err)
				}
			}
			blob := strings.Repeat("x", 256*1024)
			file, err := s.UploadAttachment(ctx, workers[0], source.ConversationID, "large.txt", "text/plain", []byte(blob))
			if err != nil {
				b.Fatal(err)
			}
			if _, err = s.Pool.Exec(ctx, `UPDATE deliveries SET state='acked' WHERE scope=$1 AND recipient_id=$2`, owner.Scope, workers[0].ID); err != nil {
				b.Fatal(err)
			}
			s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
				return OverseerDecision{Operation: "question", AgentIDs: ids[:min(n, 2)], Body: "Which release checks are blocked?", Summary: "Ask the selected agents."}, nil
			}
			on := true
			if _, err = s.UpdateAssistance(ctx, owner, AssistanceUpdate{Overseer: &on}); err != nil {
				b.Fatal(err)
			}
			plan, err := s.PlanOverseer(ctx, reader, OverseerInput{Prompt: "Check the release blockers", Target: &Target{"group", group.ID}})
			if err != nil {
				b.Fatal(err)
			}
			config := s.Pool.Config()
			queries := &queryCounter{}
			config.ConnConfig.Tracer = queries
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				b.Fatal(err)
			}
			old := s.Pool
			s.Pool = pool
			old.Close()
			cases := []struct {
				name string
				run  func(int) error
			}{
				{"Groups", func(int) error {
					x, e := s.Groups(ctx, reader)
					if e == nil && len(x) != n+1 {
						return fmt.Errorf("group count %d", len(x))
					}
					return e
				}},
				{"Roles", func(int) error {
					x, e := s.Roles(ctx, reader)
					if e == nil && len(x) != n+1 {
						return fmt.Errorf("role count %d", len(x))
					}
					return e
				}},
				{"Bindings", func(int) error {
					x, e := s.Bindings(ctx, reader)
					if e == nil && len(x) != 1 {
						return fmt.Errorf("binding count %d", len(x))
					}
					return e
				}},
				{"Answers", func(int) error {
					x, e := s.Answers(ctx, reader)
					if e == nil && len(x) != n {
						return fmt.Errorf("answer count %d", len(x))
					}
					return e
				}},
				{"Requests", func(int) error {
					x, e := s.Requests(ctx, reader)
					if e == nil && len(x) != n+1 {
						return fmt.Errorf("request count %d", len(x))
					}
					return e
				}},
				{"Conversations", func(int) error {
					x, e := s.Conversations(ctx, reader)
					if e == nil && len(x) != min(2*n+1, 200) {
						return fmt.Errorf("conversation count %d", len(x))
					}
					return e
				}},
				{"EventPage", func(int) error { _, _, e := s.EventPage(ctx, reader, 0, false); return e }},
				{"EventSummary", func(int) error { _, _, e := s.EventSummaryPage(ctx, reader, 0); return e }},
				{"FleetStatus", func(int) error {
					x, e := s.FleetStatus(ctx, reader, nil)
					if e == nil && x["total"] != n {
						return fmt.Errorf("fleet count %v", x["total"])
					}
					return e
				}},
				{"Capabilities", func(int) error { _, e := s.Capabilities(ctx, reader, CapabilityInput{}); return e }},
				{"ConversationCapabilities", func(int) error {
					_, e := s.Capabilities(ctx, reader, CapabilityInput{ConversationID: source.ConversationID})
					return e
				}},
				{"Attachments", func(int) error {
					x, e := s.Attachments(ctx, reader, source.ConversationID)
					if e == nil && len(x) != n+1 {
						return fmt.Errorf("attachment count %d", len(x))
					}
					return e
				}},
				{"Download256KiB", func(int) error {
					_, f, e := s.DownloadAttachment(ctx, reader, file.ID)
					if e != nil {
						return e
					}
					defer f.Close()
					k, e := io.Copy(io.Discard, f)
					if e == nil && k != int64(len(blob)) {
						return fmt.Errorf("bytes %d", k)
					}
					return e
				}},
				{"InboxIdle", func(int) error {
					x, e := s.Inbox(ctx, workers[0])
					if e == nil && len(x) != 0 {
						return fmt.Errorf("idle inbox %d", len(x))
					}
					return e
				}},
				{"Deliveries", func(int) error { _, e := s.Deliveries(ctx, owner); return e }},
				{"CoordinatorRead", func(int) error { _, e := s.OverseerPlan(ctx, reader, plan.ID); return e }},
			}
			for _, collection := range []string{"agents", "groups", "roles", "requests", "conversations", "answers", "messages", "attachments", "instances", "credentials", "deliveries", "overseer_plans"} {
				cases = append(cases, struct {
					name string
					run  func(int) error
				}{"Query_" + collection, func(int) error {
					filters := map[string]string{}
					if collection == "messages" || collection == "attachments" {
						filters["conversation_id"] = source.ConversationID
					}
					x, e := s.Query(ctx, reader, QueryInput{Collection: collection, Limit: 200, Filters: filters})
					if e == nil && (collection == "agents" || collection == "answers") && len(x.Items) != n {
						return fmt.Errorf("query %s count %d", collection, len(x.Items))
					}
					return e
				}})
			}
			cases = append(cases, struct {
				name string
				run  func(int) error
			}{"CoordinatorPlan", func(int) error {
				_, e := s.PlanOverseer(ctx, reader, OverseerInput{Prompt: "Check release blockers", Target: &Target{"group", group.ID}})
				return e
			}})
			for _, tc := range cases {
				b.Run(tc.name, func(b *testing.B) {
					if e := tc.run(-1); e != nil {
						b.Fatal(e)
					}
					queries.total.Store(0)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if e := tc.run(i); e != nil {
							b.Fatal(e)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(queries.total.Load())/float64(b.N), "queries/op")
				})
			}
			for _, kind := range []string{"question", "approval", "task", "approval_execution", "cancel", "replay"} {
				b.Run("Lifecycle_"+kind, func(b *testing.B) {
					queries.total.Store(0)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						key := ID("lifecycle")
						in := AskInput{Kind: kind, Target: Target{"person", reader.ID}, Title: "Release staging v2"}
						if kind == "approval_execution" || kind == "approval" {
							in.Kind = "approval"
							in.Action = &Action{Name: "deploy", ExecutorID: workers[0].ID, Resource: "staging", ResourceVersion: "v2", OperationKey: key}
						}
						if kind == "cancel" || kind == "replay" {
							in.Kind = "question"
						}
						q, e := s.Ask(ctx, workers[0], in, key)
						if e != nil {
							b.Fatal(e)
						}
						if kind == "replay" {
							again, e := s.Ask(ctx, workers[0], in, key)
							if e != nil || again.ID != q.ID {
								b.Fatal(again, e)
							}
							continue
						}
						if kind == "cancel" {
							_, e = s.EndRequest(ctx, workers[0], q.ID, "cancelled", 1)
						} else {
							outcome := map[string]string{"question": "answered", "approval": "approved", "approval_execution": "approved", "task": "completed"}[kind]
							_, e = s.Respond(ctx, reader, q.ID, Response{Outcome: outcome, Answer: "Done", Version: 1})
						}
						if e != nil {
							b.Fatal(e)
						}
						if kind == "approval_execution" {
							x, e := s.Redeem(ctx, workers[0], q.ID, *in.Action)
							if e != nil {
								b.Fatal(e)
							}
							if _, e = s.ReportExecution(ctx, workers[0], x.ID, "succeeded", map[string]any{"release": "v2"}); e != nil {
								b.Fatal(e)
							}
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(queries.total.Load())/float64(b.N), "queries/op")
				})
			}
		})
	}
}
