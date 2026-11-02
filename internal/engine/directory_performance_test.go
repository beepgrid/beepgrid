package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentDirectoryBoundsPolicyReadsAndRefreshesMembership(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	viewer := testPerson(t, s, owner, "directory-observer")
	ids := []string{}
	for i := range 32 {
		session, err := s.Enroll(ctx, owner, EnrollInput{Agent: fmt.Sprint("directory/", i), Name: fmt.Sprint("Agent ", i), Capabilities: []string{"message"}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, session.Agent.ID)
	}
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Visible agents", Members: ids[:30]})
	if err != nil {
		t.Fatal(err)
	}
	grant(t, s, owner, viewer, []Rule{
		{Action: "view", TargetKind: "group", TargetID: group.ID},
		{Action: "manage", TargetKind: "agent", TargetID: ids[30]},
	})
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
	check := func(want int, removed string) {
		t.Helper()
		queries.total.Store(0)
		rows, err := s.Agents(ctx, viewer)
		if err != nil || len(rows) != want {
			t.Fatal("directory visibility changed", len(rows), err)
		}
		for _, row := range rows {
			if row.ID == ids[31] || row.ID == removed {
				t.Fatal("hidden or revoked agent disclosed", row.ID)
			}
			if row.ID == ids[30] {
				if len(row.Instances) != 0 {
					t.Fatal("management-only visibility disclosed runtime instances")
				}
			} else if len(row.Instances) != 1 || len(row.Instances[0].Capabilities) != 1 {
				t.Fatal("visible agent lost its runtime details", row)
			}
		}
		count := queries.total.Load()
		t.Logf("visible identities=%d, policy and directory queries=%d", len(rows), count)
		if count > 12 {
			t.Fatal("agent directory repeats policy reads per agent", count)
		}
	}
	check(31, "")
	group.Members = ids[1:30]
	if _, err = s.SaveGroup(ctx, owner, group); err != nil {
		t.Fatal(err)
	}
	// Reusing the caller's context must not retain the previous operation's
	// membership snapshot or disclose an instance after its view grant is gone.
	check(30, ids[0])
}
