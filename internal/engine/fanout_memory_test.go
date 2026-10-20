package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fanoutInputTrace struct {
	bytes int
	calls int
}

func (f *fanoutInputTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if d.SQL == nativeFanoutSQL {
		f.calls++
		for _, arg := range d.Args {
			switch v := arg.(type) {
			case []byte:
				f.bytes += len(v)
			case string:
				f.bytes += len(v)
			}
		}
	}
	return ctx
}
func (*fanoutInputTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestFanoutTransfersSharedBodyOnceAndPreservesCanonicalContent(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	ids := make([]string, 32)
	for i := range ids {
		ids[i] = testPerson(t, s, owner, fmt.Sprint("compact-", i)).ID
	}
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Shared body", Members: ids})
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.Pool.Config()
	trace := &fanoutInputTrace{}
	cfg.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	old := s.Pool
	s.Pool = pool
	defer func() { s.Pool = old; pool.Close() }()
	body := strings.Repeat("<tag>\n\"é\\", 2000)
	input := SendInput{Target: Target{"group", group.ID}, Body: body}
	sent, err := s.Send(ctx, owner, input, "shared-body")
	if err != nil || len(sent.Messages) != len(ids) {
		t.Fatal(len(sent.Messages), err)
	}
	if trace.calls != 1 || trace.bytes > len(body)+32768 {
		t.Fatal("body repeated across DB boundary", trace.calls, trace.bytes, len(body))
	}
	for _, m := range sent.Messages {
		c, err := s.Conversation(ctx, owner, m.ConversationID)
		if err != nil || len(c.Messages) != 1 || c.Messages[0].Body != body || c.Messages[0].ID != m.ID {
			t.Fatal("canonical content changed", err)
		}
		// The stored full event and queued provider/native message retain exactly the
		// public Message fields, including nanosecond timestamp and optional instance.
		var event, delivery []byte
		if err = s.Pool.QueryRow(ctx, `SELECT data FROM events WHERE scope=$1 AND conversation_id=$2 AND kind='message.created'`, owner.Scope, m.ConversationID).Scan(&event); err != nil {
			t.Fatal(err)
		}
		if err = s.Pool.QueryRow(ctx, `SELECT payload->'message' FROM deliveries WHERE scope=$1 AND operation_id=$2 LIMIT 1`, owner.Scope, m.ID).Scan(&delivery); err != nil {
			t.Fatal(err)
		}
		var want, e, d map[string]any
		json.Unmarshal(JSON(m), &want)
		json.Unmarshal(event, &e)
		json.Unmarshal(delivery, &d)
		if string(JSON(want)) != string(JSON(e)) || string(JSON(want)) != string(JSON(d)) {
			t.Fatal("event or delivery changed")
		}
	}
	replay, err := s.Send(ctx, owner, input, "shared-body")
	if err != nil || string(JSON(replay)) != string(JSON(sent)) || trace.calls != 1 {
		t.Fatal("replay changed", err)
	}
}

func TestLargeBodyFanoutRollsBackEveryChunkBeforeAcknowledgement(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	ids := []string{}
	for i := range 96 {
		ids = append(ids, testPerson(t, s, owner, fmt.Sprint("large-body-", i)).ID)
	}
	g, err := s.SaveGroup(ctx, owner, Group{Name: "Chunked body", Members: ids})
	if err != nil {
		t.Fatal(err)
	}
	var sequence int64
	if err = s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `CREATE FUNCTION reject_late_chunk() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF (SELECT count(*) FROM messages)>=63 THEN RAISE EXCEPTION 'injected late batch failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_late BEFORE INSERT ON messages FOR EACH ROW EXECUTE FUNCTION reject_late_chunk()`); err != nil {
		t.Fatal(err)
	}
	in := SendInput{Target: Target{"group", g.ID}, Body: strings.Repeat("x", 32768)}
	if _, err = s.Send(ctx, owner, in, "late-chunk"); err == nil {
		t.Fatal("late chunk failure acknowledged")
	}
	for _, table := range []string{"messages", "conversations", "deliveries", "idempotency"} {
		var count int
		if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE scope=$1`, owner.Scope).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial fanout", table, count, err)
		}
	}
	var after int64
	if err = s.Pool.QueryRow(ctx, `SELECT sequence FROM scopes WHERE id=$1`, owner.Scope).Scan(&after); err != nil || after != sequence {
		t.Fatal("partial event range", after, sequence, err)
	}
	if _, err = s.Pool.Exec(ctx, `DROP FUNCTION reject_late_chunk() CASCADE`); err != nil {
		t.Fatal(err)
	}
	sent, err := s.Send(ctx, owner, in, "late-chunk")
	if err != nil || len(sent.Messages) != 96 {
		t.Fatal(len(sent.Messages), err)
	}
	again, err := s.Send(ctx, owner, in, "late-chunk")
	if err != nil || string(JSON(sent)) != string(JSON(again)) {
		t.Fatal("chunk replay changed", err)
	}
}
