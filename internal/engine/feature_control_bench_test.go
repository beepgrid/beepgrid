package engine

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkFeatureControls(b *testing.B) {
	for _, n := range []int{1, 30, 100} {
		b.Run(fmt.Sprint("Scale", n), func(b *testing.B) {
			s, owner := testStore(b)
			ctx := context.Background()
			workers := []Actor{}
			ids := []string{}
			for i := range n {
				w := testWorker(b, s, owner, fmt.Sprint("control-", i))
				workers = append(workers, w)
				ids = append(ids, w.ID)
			}
			g, e := s.SaveGroup(ctx, owner, Group{Name: "Control fleet", Members: ids})
			if e != nil {
				b.Fatal(e)
			}
			role := grant(b, s, owner, workers[0], []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}})
			checks := []PolicyCheck{}
			for _, id := range ids {
				checks = append(checks, PolicyCheck{SubjectID: workers[0].ID, Action: "message", Target: Target{"agent", id}})
			}
			in := AskInput{Target: Target{"person", owner.ID}, Title: "Which regional policy applies?", SharedKey: "control/region"}
			source, e := s.Ask(ctx, workers[0], in, "source")
			if e != nil {
				b.Fatal(e)
			}
			if _, e = s.Respond(ctx, owner, source.ID, Response{Outcome: "answered", Answer: "Regional v2", Version: 1}); e != nil {
				b.Fatal(e)
			}
			if _, e = s.PublishAnswer(ctx, owner, PublishInput{RequestID: source.ID, Audience: ids}); e != nil {
				b.Fatal(e)
			}
			counter := featureCounter(b, s)
			for _, name := range []string{"RolePreview", "DispatchPreview", "ExactReuse", "EnrollNew", "EnrollReconnect"} {
				b.Run(name, func(b *testing.B) {
					counter.total.Store(0)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						switch name {
						case "RolePreview":
							var out RolePreview
							out, e = s.PreviewRole(ctx, owner, RolePreviewInput{Role: role, Checks: checks})
							if e == nil && (!out.CanSave || len(out.Checks) != n) {
								b.Fatal("preview checks", len(out.Checks))
							}
						case "DispatchPreview":
							_, e = s.DispatchPreview(ctx, workers[0], DispatchPreviewInput{Targets: []Target{{"group", g.ID}}, Operation: "message"})
						case "ExactReuse":
							var out Request
							out, e = s.Ask(ctx, workers[0], in, ID("reuse"))
							if e == nil && (out.State != "resolved" || out.Response.SourceAnswerID == "") {
								b.Fatal("exact reuse changed", out)
							}
						case "EnrollNew":
							id := ID("enroll")
							_, e = s.Enroll(ctx, owner, EnrollInput{Agent: id, Name: id, Capabilities: []string{"message"}})
						case "EnrollReconnect":
							_, e = s.Enroll(ctx, owner, EnrollInput{Agent: workers[0].ExternalID, Name: workers[0].Name, Capabilities: []string{"message"}})
						}
						if e != nil {
							b.Fatal(e)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(counter.total.Load())/float64(b.N), "queries/op")
				})
			}
		})
	}
}
