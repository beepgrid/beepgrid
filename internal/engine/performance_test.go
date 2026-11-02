package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryCounter struct{ total atomic.Int64 }

func (q *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.total.Add(1)
	return ctx
}
func (q *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestConversationListBoundsUnrelatedHistoryAndPreservesVisibility(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	reader := testPerson(t, s, owner, "history-reader")
	outsider := testPerson(t, s, owner, "unrelated-reader")
	role := grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}})
	if _, err := s.Pool.Exec(ctx, `INSERT INTO principals(id,scope,kind,external_id,name) SELECT 'directory_noise_'||n,$1,'agent','noise/'||n,'Unrelated agent '||n FROM generate_series(1,10000) n`, owner.Scope); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO conversations(id,scope,title,created_by,created_at) SELECT 'visible_'||n,$1,'Visible '||n,$2,to_timestamp(1700000000)+n*interval '1 microsecond' FROM generate_series(1,250) n`,
		`INSERT INTO conversations(id,scope,title,created_by,created_at) SELECT 'noise_'||n,$1,'Unrelated '||n,$2,to_timestamp(1800000000)+n*interval '1 microsecond' FROM generate_series(1,20000) n`,
		`INSERT INTO participants(scope,conversation_id,principal_id) SELECT $1,id,$2 FROM conversations UNION ALL SELECT $1,id,CASE WHEN id LIKE 'visible_%' THEN $3 ELSE $4 END FROM conversations`,
	} {
		// Separate argument lists avoid untyped unused parameters in PostgreSQL.
		args := []any{owner.Scope, owner.ID}
		if strings.HasPrefix(statement, "INSERT INTO participants") {
			args = append(args, reader.ID, outsider.ID)
		}
		if _, err := s.Pool.Exec(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Conversations(ctx, reader)
	if err != nil || len(rows) != 200 {
		t.Fatal(len(rows), err)
	}
	for i, row := range rows {
		if row.ID != fmt.Sprint("visible_", 250-i) {
			t.Fatal("private list, ordering or limit changed", i, row.ID)
		}
	}
	adminRows, err := s.Conversations(ctx, owner)
	if err != nil || len(adminRows) != 200 || adminRows[0].ID != "noise_20000" {
		t.Fatal("administrator history changed", len(adminRows), err)
	}
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `EXPLAIN (ANALYZE,FORMAT JSON) `+conversationListSQL(false), owner.Scope, reader.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []map[string]any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	scanned := map[string]float64{}
	var visit func(map[string]any)
	visit = func(node map[string]any) {
		if relation, ok := node["Relation Name"].(string); ok {
			count, _ := node["Actual Rows"].(float64)
			removed, _ := node["Rows Removed by Filter"].(float64)
			loops, _ := node["Actual Loops"].(float64)
			scanned[relation] += (count + removed) * loops
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			visit(child.(map[string]any))
		}
	}
	visit(plan[0]["Plan"].(map[string]any))
	if scanned["conversations"] > 250 || scanned["principals"] > 400 {
		t.Fatal("list scans unrelated workspace history/directory", scanned)
	}
	role.Rules = []Rule{}
	if _, err := s.SaveRole(ctx, owner, role); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.Conversations(ctx, reader); err != nil || len(rows) != 0 {
		t.Fatal("history snapshot retained revoked visibility", len(rows), err)
	}
}

func TestIndividualContactFanoutUsesBoundedQueriesAndFreshAuthority(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	sender := testPerson(t, s, owner, "individual-contact-sender")
	role := grant(t, s, owner, sender, []Rule{{Action: "message", TargetKind: "person", TargetID: "*"}})
	ids := []string{}
	for i := range 32 {
		ids = append(ids, testPerson(t, s, owner, fmt.Sprint("individual-contact-", i)).ID)
	}
	g, err := s.SaveGroup(ctx, owner, Group{Name: "individually permitted audience", Members: ids})
	if err != nil {
		t.Fatal(err)
	}
	config := s.Pool.Config()
	queries := &queryCounter{}
	config.ConnConfig.Tracer = queries
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	old := s.Pool
	s.Pool = pool
	old.Close()
	in := SendInput{Target: Target{"group", g.ID}, Body: "private, individually permitted message"}
	out, err := s.Send(ctx, sender, in, "bounded-contact-rules")
	if err != nil || len(out.Messages) != 32 {
		t.Fatal(len(out.Messages), err)
	}
	if n := queries.total.Load(); n > 12 {
		t.Fatal("database work scales with recipient role checks", n)
	}
	role.Rules = []Rule{}
	if _, err = s.SaveRole(ctx, owner, role); err != nil {
		t.Fatal(err)
	}
	queries.total.Store(0)
	if _, err = s.Send(ctx, sender, in, "revoked-contact-rules"); ErrorStatus(err) != 403 {
		t.Fatal("policy snapshot escaped its operation", err)
	}
	if n := queries.total.Load(); n > 12 {
		t.Fatal("denied fanout repeats role reads", n)
	}
	var count int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&count); err != nil || count != 32 {
		t.Fatal("denial wrote partial history", count, err)
	}
}

// These benchmarks include real PostgreSQL round trips and assert the expected result counts.
// Synthetic directory rows isolate read-path cost; enrollment and lifecycle correctness are tested separately.
func BenchmarkCorePaths(b *testing.B) {
	s, owner := testStore(b)
	ctx := context.Background()
	reader := testPerson(b, s, owner, "directory-reader")
	grant(b, s, owner, reader, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "view", TargetKind: "person", TargetID: owner.ID}})
	for _, sql := range []string{
		`INSERT INTO principals(id,scope,kind,external_id,name) SELECT 'bench_agent_'||n,$1,'agent','bench/'||n,'Agent '||n FROM generate_series(1,1000) n`,
		`INSERT INTO credentials(id,scope,principal_id,digest,kind,expires_at) SELECT 'bench_key_'||n,$1,'bench_agent_'||n,md5($1||n::text),'worker',now()+interval '1 day' FROM generate_series(1,1000) n`,
		`INSERT INTO instances(id,scope,agent_id,credential_id,capabilities) SELECT 'bench_instance_'||n,$1,'bench_agent_'||n,'bench_key_'||n,'["message","status"]'::jsonb FROM generate_series(1,1000) n`,
	} {
		if _, e := s.Pool.Exec(ctx, sql, owner.Scope); e != nil {
			b.Fatal(e)
		}
	}
	for i := 0; i < 200; i++ {
		if _, e := s.Ask(ctx, owner, AskInput{Target: Target{"person", reader.ID}, Title: fmt.Sprintf("Question %d", i)}, fmt.Sprintf("seed/%d", i)); e != nil {
			b.Fatal(e)
		}
	}
	members := []string{}
	for i := 1; i <= 100; i++ {
		members = append(members, fmt.Sprintf("bench_agent_%d", i))
	}
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Fleet", Members: members})
	if e != nil {
		b.Fatal(e)
	}
	config := s.Pool.Config()
	queries := &queryCounter{}
	config.ConnConfig.Tracer = queries
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		b.Fatal(e)
	}
	old := s.Pool
	s.Pool = pool
	old.Close()
	cases := []struct {
		name string
		run  func(int) error
	}{
		{"AgentsAdmin1000", func(_ int) error {
			r, e := s.Agents(ctx, owner)
			if e == nil && (len(r) != 1000 || len(r[0].Instances) != 1) {
				return fmt.Errorf("incorrect directory: %d", len(r))
			}
			return e
		}},
		{"AgentsRole1000", func(_ int) error {
			r, e := s.Agents(ctx, reader)
			if e == nil && (len(r) != 1000 || len(r[0].Instances) != 1) {
				return fmt.Errorf("incorrect restricted directory: %d", len(r))
			}
			return e
		}},
		{"RequestsRole200", func(_ int) error {
			r, e := s.Requests(ctx, reader)
			if e == nil && len(r) != 200 {
				return fmt.Errorf("incorrect requests: %d", len(r))
			}
			return e
		}},
		{"ConversationsRole200", func(_ int) error {
			r, e := s.Conversations(ctx, reader)
			if e == nil && len(r) != 200 {
				return fmt.Errorf("incorrect conversations: %d", len(r))
			}
			return e
		}},
		{"PrivateFanout100", func(i int) error {
			r, e := s.Send(ctx, owner, SendInput{Target: Target{"group", group.ID}, Body: "private benchmark message"}, fmt.Sprintf("fanout/%d/%s", i, ID("run")))
			if e == nil && len(r.Messages) != 100 {
				return fmt.Errorf("incorrect fanout: %d", len(r.Messages))
			}
			return e
		}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			queries.total.Store(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if e := c.run(i); e != nil {
					b.Fatal(e)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(queries.total.Load())/float64(b.N), "queries/op")
		})
	}
}
