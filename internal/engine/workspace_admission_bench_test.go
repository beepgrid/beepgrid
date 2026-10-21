package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

// Real PostgreSQL benchmark: accepted sends and canonical row counts must agree.
// Compare the same file at the baseline revision and candidate revision.
func BenchmarkWorkspaceTraffic(b *testing.B) {
	for _, scenario := range []string{"Sequential", "HotParallel", "FourTenantsParallel"} {
		b.Run(scenario, func(b *testing.B) {
			s, owner := testStore(b)
			ctx := context.Background()
			owners := []Actor{owner}
			conversations := []string{}
			n := 1
			if scenario == "FourTenantsParallel" {
				n = 4
			}
			for i := 0; i < n; i++ {
				if i > 0 {
					a, e := s.Bootstrap(ctx, fmt.Sprintf("bench_%s/%d", ID("scope"), i), "Benchmark", "production", Secret())
					if e != nil {
						b.Fatal(e)
					}
					owners = append(owners, a)
				}
				worker := testWorker(b, s, owners[i], fmt.Sprint("worker-", i))
				chat, e := s.Send(ctx, owners[i], SendInput{Target: Target{"agent", worker.ID}, Body: "Seed canonical conversation"}, fmt.Sprint("seed-", i))
				if e != nil {
					b.Fatal(e)
				}
				conversations = append(conversations, chat.ConversationID)
			}
			var next atomic.Int64
			send := func() {
				i := next.Add(1)
				index := int(i) % n
				out, e := s.Send(ctx, owners[index], SendInput{ConversationID: conversations[index], Body: "Measured canonical message"}, fmt.Sprintf("bench-%d", i))
				if e != nil || len(out.Messages) != 1 {
					b.Errorf("accepted send failed: %v", e)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			if scenario == "Sequential" {
				for i := 0; i < b.N; i++ {
					send()
				}
			} else {
				b.SetParallelism(2)
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						send()
					}
				})
			}
			b.StopTimer()
			var count int
			if e := s.Pool.QueryRow(ctx, "SELECT count(*) FROM messages").Scan(&count); e != nil || count != b.N+n {
				b.Fatalf("canonical count %d want %d: %v", count, b.N+n, e)
			}
		})
	}
}
