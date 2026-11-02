package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Identical benchmark source also runs at d685daa. These component workloads
// supplement, rather than replace, unchanged production-container HTTP trials.
func BenchmarkChronologicalHistory(b *testing.B) {
	for _, count := range []int{10000, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s, owner := testStore(b)
			ctx := context.Background()
			worker := testWorker(b, s, owner, "seek-benchmark")
			sent, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "Seed"}, "seed")
			if err != nil {
				b.Fatal(err)
			}
			_, err = s.Pool.Exec(ctx, `INSERT INTO messages(id,scope,conversation_id,sender_id,body,created_at)
 SELECT 'history-'||lpad(i::text,6,'0'),$1,$2,$3,'Retained message',
 '2024-01-01T00:00:00Z'::timestamptz+i*interval '1 microsecond' FROM generate_series(0,$4::int-1) i`, owner.Scope, sent.ConversationID, owner.ID, count)
			if err != nil {
				b.Fatal(err)
			}
			if _, err = s.Pool.Exec(ctx, "ANALYZE messages"); err != nil {
				b.Fatal(err)
			}
			in := QueryInput{Collection: "messages", Limit: 12, Filters: map[string]string{"conversation_id": sent.ConversationID}}
			last := count - 1000
			stamp := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(last) * time.Microsecond)
			key := stamp.Format("2006-01-02T15:04:05.000000") + fmt.Sprintf("/history-%06d", last)
			raw, err := s.seal(JSON(queryCursor{owner.Scope, worker.ID, Digest(string(JSON(in))), key, time.Now().Add(time.Hour).Unix()}))
			if err != nil {
				b.Fatal(err)
			}
			in.Cursor = base64.RawURLEncoding.EncodeToString(raw)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				page, err := s.Query(ctx, worker, in)
				if err != nil || len(page.Items) != 12 || page.Items[0].(Message).ID != fmt.Sprintf("history-%06d", last+1) {
					b.Fatal("history contract changed", err)
				}
			}
		})
	}
}

func BenchmarkNativeFleetRoundTrip(b *testing.B) {
	for _, count := range []int{1, 30, 100} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s, owner := testStore(b)
			ctx := context.Background()
			workers := make([]Actor, count)
			ids := make([]string, count)
			for i := range count {
				workers[i] = testWorker(b, s, owner, fmt.Sprint("native-", i))
				ids[i] = workers[i].ID
			}
			group, err := s.SaveGroup(ctx, owner, Group{Name: "Native fleet", Members: ids})
			if err != nil {
				b.Fatal(err)
			}
			body := strings.Repeat("x", 8192)
			b.ReportAllocs()
			b.ResetTimer()
			for round := range b.N {
				out, err := s.Send(ctx, owner, SendInput{Target: Target{"group", group.ID}, Body: body}, fmt.Sprint("native-", round))
				if err != nil || len(out.Messages) != count {
					b.Fatal("fan-out changed", err)
				}
				results := make(chan error, count)
				for _, worker := range workers {
					go func() {
						jobs, err := s.Inbox(ctx, worker)
						if err == nil && (len(jobs) != 1 || jobs[0].Payload["message"].(map[string]any)["body"] != body) {
							err = fmt.Errorf("native body/count mismatch")
						}
						if err == nil {
							err = s.Ack(ctx, worker, jobs[0].ID, jobs[0].Generation)
						}
						results <- err
					}()
				}
				for range count {
					if err := <-results; err != nil {
						b.Fatal(err)
					}
				}
			}
			b.StopTimer()
			var received int
			if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE state='received'`).Scan(&received); err != nil || received != b.N*count {
				b.Fatal("lost/duplicate delivery", received, err)
			}
		})
	}
}
