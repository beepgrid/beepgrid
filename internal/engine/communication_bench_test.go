package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Real PostgreSQL, complete canonical writes and reads. Authentication is
// represented by scoped actors; HTTP and authentication costs are measured by
// the separate container driver rather than hidden in this component profile.
func BenchmarkCommunicationPaths(b *testing.B) {
	for _, size := range []int{1024, 8192} {
		for _, path := range []string{"NewDirect", "ExistingReply", "ReadHistory", "Fleet30", "Directory30"} {
			b.Run(fmt.Sprintf("%s/%d", path, size), func(b *testing.B) {
				s, owner := testStore(b)
				ctx := context.Background()
				worker := testWorker(b, s, owner, "profile-worker")
				body := strings.Repeat("b", size)
				seed, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: body}, "seed")
				if err != nil {
					b.Fatal(err)
				}
				members := []string{worker.ID}
				for i := 1; i < 30; i++ {
					members = append(members, testWorker(b, s, owner, fmt.Sprint("profile-", i)).ID)
				}
				group, err := s.SaveGroup(ctx, owner, Group{Name: "Profile fleet", Members: members})
				if err != nil {
					b.Fatal(err)
				}
				if path == "ReadHistory" {
					for i := range 12 {
						if _, err = s.Send(ctx, worker, SendInput{ConversationID: seed.ConversationID, Body: body}, fmt.Sprint("history-", i)); err != nil {
							b.Fatal(err)
						}
					}
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
				queries.total.Store(0)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					switch path {
					case "NewDirect":
						_, err = s.Send(ctx, worker, SendInput{Target: Target{"person", owner.ID}, Body: body}, fmt.Sprint("direct-", i))
					case "ExistingReply":
						_, err = s.Send(ctx, worker, SendInput{ConversationID: seed.ConversationID, Body: body}, fmt.Sprint("reply-", i))
					case "ReadHistory":
						var c Conversation
						c, err = s.Conversation(ctx, worker, seed.ConversationID)
						if err == nil && len(c.Messages) != 13 {
							b.Fatal("history changed", len(c.Messages))
						}
					case "Fleet30":
						var out SendResult
						out, err = s.Send(ctx, worker, SendInput{Target: Target{"group", group.ID}, Body: body}, fmt.Sprint("fleet-", i))
						if err == nil && len(out.Messages) != 30 {
							b.Fatal("fanout changed", len(out.Messages))
						}
					case "Directory30":
						var rows []Agent
						rows, err = s.Agents(ctx, worker)
						if err == nil && len(rows) != 30 {
							b.Fatal("directory changed", len(rows))
						}
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(queries.total.Load())/float64(b.N), "queries/op")
			})
		}
	}
}
