package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDeliveryProofBoundsReadsAndRetainsCurrentAuthority(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, owner, "delivery-proof-budget")
	q, err := s.Ask(ctx, w, AskInput{Target: Target{"person", owner.ID}, Title: "Deploy staging?"}, "proof")
	if err != nil {
		t.Fatal(err)
	}
	d, err := deliveryScan(s.Pool.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 AND operation_id=$2 AND recipient_id=$3 AND connection_id=''`, owner.Scope, q.ID, owner.ID))
	if err != nil {
		t.Fatal(err)
	}
	counter := &queryCounter{}
	cfg := s.Pool.Config()
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	old := s.Pool
	s.Pool = pool
	old.Close()
	check := func(want bool, maxQueries int64) {
		t.Helper()
		counter.total.Store(0)
		got := s.currentDeliveryPolicy(ctx, s.Pool, owner.Scope, d)
		if got != want || counter.total.Load() > maxQueries {
			t.Fatal("delivery proof changed or repeats authority reads", got, want, counter.total.Load(), maxQueries)
		}
	}
	check(true, 2) // One joined validity proof and one current effective-role read.
	if _, err = s.Pool.Exec(ctx, `UPDATE requests SET expires_at=now()-interval '1 second' WHERE id=$1`, q.ID); err != nil {
		t.Fatal(err)
	}
	check(false, 1)
	if _, err = s.Pool.Exec(ctx, `UPDATE requests SET expires_at=now()+interval '1 hour' WHERE id=$1`, q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE credentials SET expires_at=now()-interval '1 second' WHERE id=$1`, w.CredentialID); err != nil {
		t.Fatal(err)
	}
	check(false, 1)
	if _, err = s.Pool.Exec(ctx, `UPDATE credentials SET expires_at=NULL WHERE id=$1`, w.CredentialID); err != nil {
		t.Fatal(err)
	}
	check(true, 2)
	_, browserID, err := issue(ctx, s.Pool, owner, owner.ID, "browser", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	d.Payload["_policy"] = map[string]any{"sender_id": owner.ID, "credential_id": browserID, "target": Target{"person", owner.ID}, "action": "message"}
	check(true, 1)
	if _, err = s.Pool.Exec(ctx, `UPDATE credentials SET revoked=true WHERE scope=$1 AND principal_id=$2 AND kind='admin'`, owner.Scope, owner.ID); err != nil {
		t.Fatal(err)
	}
	check(false, 2) // Browser identity must immediately lose inherited admin power.
}

func TestJSONColumnOwnsDecodedDataAcrossRowAndConnectionReuse(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	rows, err := s.Pool.Query(ctx, `SELECT jsonb_build_object('index',i,'body',repeat('x',8192)||i::text,'nested',jsonb_build_object('unicode','café ☃','values',jsonb_build_array(true,null,3))) FROM generate_series(1,40) i`)
	if err != nil {
		t.Fatal(err)
	}
	values := []map[string]any{}
	for rows.Next() {
		var value map[string]any
		if err = rows.Scan(jsonColumn{&value}); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for i := range 50 {
		var overwrite string
		if err = s.Pool.QueryRow(ctx, `SELECT repeat('z',65536)||$1::text`, fmt.Sprint(i)).Scan(&overwrite); err != nil {
			t.Fatal(err)
		}
	}
	for i, value := range values {
		if value["body"] != strings.Repeat("x", 8192)+fmt.Sprint(i+1) || value["index"] != float64(i+1) || value["nested"].(map[string]any)["unicode"] != "café ☃" {
			t.Fatal("decoded JSON retained a borrowed row buffer", i)
		}
	}
	// Match the existing tolerant readers for null and wrongly shaped JSON.
	for _, raw := range []string{"null", `{"body":"hello"}`, `[1,2]`, `"scalar"`} {
		var got, want map[string]any
		_ = json.Unmarshal([]byte(raw), &want)
		if err = s.Pool.QueryRow(ctx, `SELECT $1::jsonb`, raw).Scan(jsonColumn{&got}); err != nil || string(JSON(got)) != string(JSON(want)) {
			t.Fatal("JSON scan compatibility", raw, got, want, err)
		}
	}
}

func BenchmarkJSONColumnCopy(b *testing.B) {
	raw := JSON(map[string]any{"body": strings.Repeat("x", 8192), "nested": map[string]any{"value": "café"}})
	for _, direct := range []bool{false, true} {
		b.Run(fmt.Sprint("direct=", direct), func(b *testing.B) {
			m := pgtype.NewMap()
			var dst any = new([]byte)
			if direct {
				dst = jsonColumn{new(map[string]any)}
			}
			plan := m.PlanScan(pgtype.JSONBOID, pgtype.TextFormatCode, dst)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var value map[string]any
				if direct {
					if err := plan.Scan(raw, jsonColumn{&value}); err != nil {
						b.Fatal(err)
					}
				} else {
					var copy []byte
					if err := plan.Scan(raw, &copy); err != nil {
						b.Fatal(err)
					}
					if err := json.Unmarshal(copy, &value); err != nil {
						b.Fatal(err)
					}
				}
				if len(value["body"].(string)) != 8192 {
					b.Fatal("payload changed")
				}
			}
		})
	}
}

func TestSearchColumnsPreserveLegacyTextForEveryCollection(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	legacy := `concat_ws(' ',to_jsonb(t)->>'id',to_jsonb(t)->>'name',to_jsonb(t)->>'title',to_jsonb(t)->>'body',to_jsonb(t)->>'question',to_jsonb(t)->>'external_id',to_jsonb(t)->'plan'->>'prompt',to_jsonb(t)->'metadata'->>'department',to_jsonb(t)->'metadata'->>'email')`
	for name, source := range querySources {
		for _, fields := range []map[string]any{
			{"id": "record", "name": "Café  studio", "title": "A release", "body": "Line one\nline two", "question": "Which region?", "external_id": "external/id", "plan": map[string]any{"prompt": "Check staging"}, "metadata": map[string]any{"department": "Engineering", "email": "māya@example.com"}},
			{"id": "record", "name": "", "title": nil, "body": "", "question": nil, "external_id": "", "plan": map[string]any{"prompt": nil}, "metadata": map[string]any{"department": nil, "email": map[string]any{"value": "unusual metadata"}}},
			{"id": "record"},
		} {
			var before, after string
			sql := "SELECT " + legacy + "," + querySearchText(source.table) + " FROM jsonb_populate_record(NULL::" + source.table + ",$1::jsonb) t"
			if err := s.Pool.QueryRow(ctx, sql, JSON(fields)).Scan(&before, &after); err != nil || before != after {
				t.Fatal("searchable fields, ordering or null behavior changed", name, before, after, err)
			}
		}
	}
}

func BenchmarkSearchRetainedHistory(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s, owner := testStore(b)
			ctx := context.Background()
			w := testWorker(b, s, owner, "search-history")
			sent, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", w.ID}, Body: "Seed"}, "seed")
			if err != nil {
				b.Fatal(err)
			}
			_, err = s.Pool.Exec(ctx, `INSERT INTO messages(id,scope,conversation_id,sender_id,body,created_at) SELECT 'search_'||lpad(i::text,6,'0'),$1,$2,$3,repeat('x',8192)||CASE WHEN i=$4 THEN 'reconciliation-needle' ELSE 'ordinary' END,'2024-01-01T00:00:00Z'::timestamptz+i*interval '1 microsecond' FROM generate_series(1,$4::int) i`, owner.Scope, sent.ConversationID, owner.ID, count)
			if err != nil {
				b.Fatal(err)
			}
			if _, err = s.Pool.Exec(ctx, "ANALYZE messages"); err != nil {
				b.Fatal(err)
			}
			in := QueryInput{Collection: "messages", Search: "reconciliation-needle", Limit: 12, Filters: map[string]string{"conversation_id": sent.ConversationID}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				page, err := s.Query(ctx, owner, in)
				if err != nil || len(page.Items) != 1 || page.Items[0].(Message).ID != fmt.Sprintf("search_%06d", count) {
					b.Fatal("search contract changed", page, err)
				}
			}
		})
	}
}
