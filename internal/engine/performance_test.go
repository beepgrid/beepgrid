package engine

import (
	"context"
	"fmt"
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
