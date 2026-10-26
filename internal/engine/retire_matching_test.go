package engine

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuestionsNeverInvokeCoordinatorOrHoldAlerts(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker, _, in := publishedFixture(t, s, owner)
	var calls atomic.Int32
	s.OverseerPlanner = func(context.Context, string, []FleetStatus) (OverseerDecision, error) {
		calls.Add(1)
		return OverseerDecision{}, nil
	}
	enableAssistance(t, s, owner, true)
	q, err := s.Ask(ctx, worker, in, "ordinary-question")
	if err != nil || q.State != "open" || q.Response != nil || calls.Load() != 0 {
		t.Fatal("question triggered model reasoning or reused a paraphrase", q, err)
	}
	var held int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE operation_id=$1 AND next_at>now()+interval '1 second'`, q.ID).Scan(&held); err != nil || held != 0 {
		t.Fatal("question alert was delayed", held, err)
	}
}

func TestRetireMatchingUpgradeReleasesOnlyHeldAlerts(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker, publication, in := publishedFixture(t, s, owner)
	q, err := s.Ask(ctx, worker, in, "legacy-waiter")
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct precisely the retired v22 tables in this isolated test schema.
	legacy, err := migrations.ReadFile("migrations/013.sql")
	if err != nil {
		t.Fatal(err)
	}
	legacy = []byte(strings.Split(string(legacy), "CREATE TABLE overseer_plans")[0])
	ddl := strings.ReplaceAll(string(legacy), "ALTER TABLE scopes ADD COLUMN overseer_enabled boolean NOT NULL DEFAULT false;", "")
	ddl = strings.ReplaceAll(ddl, "ALTER TABLE scopes ADD COLUMN assistance_version bigint NOT NULL DEFAULT 0;", "")
	proposals, _ := migrations.ReadFile("migrations/002.sql")
	if _, err = s.Pool.Exec(ctx, ddl+string(proposals)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version=23; UPDATE scopes SET automatic_answers=true`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `INSERT INTO answer_match_jobs(scope,request_id,version,state) VALUES($1,$2,1,'leased')`, owner.Scope, q.ID); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{q.ID, "unrelated-retry"} {
		if _, err = s.Pool.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,payload,expires_at,next_at) VALUES($1,$2,$3,$4,'{}',now()+interval '1 day',now()+interval '25 seconds') ON CONFLICT DO NOTHING`, ID("del"), owner.Scope, op, owner.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE deliveries SET next_at=now()+interval '25 seconds' WHERE operation_id=$1`, q.ID); err != nil {
		t.Fatal(err)
	}
	schema := strings.Split(s.Pool.Config().ConnConfig.RuntimeParams["search_path"], ",")[0]
	if err = Migrate(ctx, os.Getenv("NOTIF_TEST_DATABASE_URL"), schema); err != nil {
		t.Fatal(err)
	}
	// Reapplying migration is safe; the ordinary request and publication survive.
	if err = Migrate(ctx, os.Getenv("NOTIF_TEST_DATABASE_URL"), schema); err != nil {
		t.Fatal(err)
	}
	var retired int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name IN ('answer_match_jobs','answer_proposals')`, schema).Scan(&retired); err != nil || retired != 0 {
		t.Fatal("retired tables remain", retired, err)
	}
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='scopes' AND column_name='automatic_answers'`, schema).Scan(&retired); err != nil || retired != 0 {
		t.Fatal("retired setting remains", retired, err)
	}
	for _, op := range []string{q.ID, "unrelated-retry"} {
		var next time.Time
		if err = s.Pool.QueryRow(ctx, `SELECT next_at FROM deliveries WHERE operation_id=$1 LIMIT 1`, op).Scan(&next); err != nil {
			t.Fatal(err)
		}
		if (op == q.ID && next.After(time.Now())) || (op != q.ID && !next.After(time.Now())) {
			t.Fatal("migration changed the wrong delivery schedule", op, next)
		}
	}
	if got, err := s.Request(ctx, worker, q.ID); err != nil || got.State != "open" {
		t.Fatal("request history lost", got, err)
	}
	answers, err := s.Answers(ctx, owner)
	if err != nil || len(answers) != 1 || answers[0].ID != publication.ID {
		t.Fatal("exact publication lost", answers, err)
	}
}
