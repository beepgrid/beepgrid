package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type chronologicalQueryTrace struct {
	sql  string
	args []any
}

func (q *chronologicalQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "SELECT t.id,") && strings.Contains(d.SQL, " FROM messages t ") {
		q.sql = d.SQL
		q.args = append([]any(nil), d.Args...)
	}
	return ctx
}
func (*chronologicalQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestMessageCursorSeeksRetainedHistoryAndAcceptsLegacyCursor(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "indexed-history")
	sent, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "Seed"}, "seed")
	if err != nil {
		t.Fatal(err)
	}
	// Timestamp ties exercise the complete cursor key. Fixture insertion is not
	// a timed send and deliberately avoids thousands of unrelated deliveries.
	_, err = s.Pool.Exec(ctx, `INSERT INTO messages(id,scope,conversation_id,sender_id,body,created_at)
 SELECT 'history-'||lpad(i::text,5,'0'),$1,$2,$3,'Retained message',
 '2024-01-01T00:00:00Z'::timestamptz+(i/3)*interval '1 microsecond'
 FROM generate_series(0,9999) i`, owner.Scope, sent.ConversationID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "ANALYZE messages"); err != nil {
		t.Fatal(err)
	}
	in := QueryInput{Collection: "messages", Filters: map[string]string{"conversation_id": sent.ConversationID}, Limit: 12}
	legacy := queryCursor{Scope: owner.Scope, Actor: worker.ID, Query: Digest(string(JSON(in))),
		Last: "2024-01-01T00:00:00.003000/history-09000", Until: time.Now().Add(time.Hour).Unix()}
	raw, err := s.seal(JSON(legacy))
	if err != nil {
		t.Fatal(err)
	}
	in.Cursor = base64.RawURLEncoding.EncodeToString(raw)
	trace := &chronologicalQueryTrace{}
	config := s.Pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	old := s.Pool
	s.Pool = pool
	old.Close()
	// Exercise cached plans too, rather than only one favorable custom plan.
	for range 7 {
		page, err := s.Query(ctx, worker, in)
		if err != nil || len(page.Items) != 12 || page.NextCursor == "" || page.Order != "created_at_id_ascending" {
			t.Fatal("legacy cursor or shape changed", page, err)
		}
		for i, item := range page.Items {
			if item.(Message).ID != fmt.Sprintf("history-%05d", 9001+i) {
				t.Fatal("timestamp-tie ordering changed", i, item)
			}
		}
	}
	var plan []byte
	if err = s.Pool.QueryRow(ctx, "EXPLAIN (ANALYZE,FORMAT JSON) "+trace.sql, trace.args...).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	var tree any
	if err = json.Unmarshal(plan, &tree); err != nil {
		t.Fatal(err)
	}
	indexed := false
	var visit func(any)
	visit = func(node any) {
		switch x := node.(type) {
		case []any:
			for _, child := range x {
				visit(child)
			}
		case map[string]any:
			if x["Relation Name"] == "messages" {
				if x["Index Name"] != "messages_conversation" || (x["Node Type"] != "Index Scan" && x["Node Type"] != "Index Only Scan") {
					t.Fatal("history still scans or sorts the retained prefix", string(plan))
				}
				removed, _ := x["Rows Removed by Filter"].(float64)
				if x["Actual Rows"].(float64) > 501 || removed > 1 {
					t.Fatal("cursor did not bound retained-history work", string(plan))
				}
				indexed = true
			}
			for _, child := range x {
				visit(child)
			}
		}
	}
	visit(tree)
	if !indexed {
		t.Fatal("no chronological index scan", string(plan))
	}
	// A valid cursor never retains authority after a role change.
	roles, err := s.Roles(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		if err = s.BindRole(ctx, owner, Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: worker.ID}, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Query(ctx, worker, in); ErrorStatus(err) != 404 {
		t.Fatal("cursor retained revoked history access", err)
	}
}
