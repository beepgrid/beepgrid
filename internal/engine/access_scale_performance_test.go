package engine

import (
	"context"
	"encoding/json"
	"testing"
)

func measuredPlanRows(t *testing.T, s *Store, sql string, args ...any) map[string]float64 {
	t.Helper()
	var raw []byte
	if err := s.Pool.QueryRow(context.Background(), `EXPLAIN (ANALYZE,FORMAT JSON) `+sql, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plans []map[string]any
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	var visit func(map[string]any)
	visit = func(node map[string]any) {
		if name, ok := node["Relation Name"].(string); ok {
			n, _ := node["Actual Rows"].(float64)
			removed, _ := node["Rows Removed by Filter"].(float64)
			loops, _ := node["Actual Loops"].(float64)
			out[name] += (n + removed) * loops
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			visit(child.(map[string]any))
		}
	}
	visit(plans[0]["Plan"].(map[string]any))
	return out
}

func TestDirectoryBoundsHistoricalInstances(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	selected := testWorker(t, s, owner, "history-selected")
	noise := testWorker(t, s, owner, "history-unrelated")
	if _, err := s.Pool.Exec(ctx, `INSERT INTO credentials(id,scope,principal_id,digest,kind)
 SELECT 'history-key-'||n,$1,CASE WHEN n<=10000 THEN $2 ELSE $3 END,md5($1||'history-'||n),'worker' FROM generate_series(1,20000) n`, owner.Scope, selected.ID, noise.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO instances(id,scope,agent_id,credential_id,capabilities,created_at)
 SELECT 'history-instance-'||lpad(n::text,5,'0'),$1,CASE WHEN n<=10000 THEN $2 ELSE $3 END,'history-key-'||n,'["message"]'::jsonb,to_timestamp(1900000000)+n*interval '1 microsecond' FROM generate_series(1,20000) n`, owner.Scope, selected.ID, noise.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `ANALYZE instances; ANALYZE credentials; ANALYZE principals`); err != nil {
		t.Fatal(err)
	}
	rows, err := s.instancesForAgents(ctx, owner.Scope, []string{selected.ID, selected.ID})
	if err != nil || len(rows[selected.ID]) != 1000 || rows[selected.ID][0].ID != "history-instance-10000" {
		t.Fatal("instance bound, deduplication or newest-first ordering changed", len(rows[selected.ID]), err)
	}
	plan := measuredPlanRows(t, s, agentInstancesSQL, owner.Scope, []string{selected.ID})
	t.Log("rows visited", plan)
	if plan["instances"] > 1100 || plan["credentials"] > 1100 {
		t.Fatal("directory scans historical or unrelated instances before its per-agent bound", plan)
	}
}

func TestPolicyLookupBoundsUnrelatedBindings(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	viewer := testPerson(t, s, owner, "indexed-policy-viewer")
	role := grant(t, s, owner, viewer, []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}})
	if _, err := s.Pool.Exec(ctx, `INSERT INTO principals(id,scope,kind,external_id,name)
 SELECT 'policy-noise-'||n,$1,'person','noise/'||n,'Unrelated '||n FROM generate_series(1,10000) n`, owner.Scope); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO role_bindings(scope,role_id,subject_kind,subject_id)
 SELECT $1,$2,'principal','policy-noise-'||n FROM generate_series(1,10000) n`, owner.Scope, role.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `ANALYZE role_bindings; ANALYZE memberships; ANALYZE roles`); err != nil {
		t.Fatal(err)
	}
	rules, err := roleRules(ctx, s.Pool, viewer)
	if err != nil || len(rules) != 1 {
		t.Fatal("effective permission changed", len(rules), err)
	}
	plan := measuredPlanRows(t, s, roleRulesSQL, owner.Scope, viewer.ID)
	t.Log("rows visited", plan)
	if plan["role_bindings"] > 10 {
		t.Fatal("policy lookup scans unrelated identity bindings", plan)
	}
}
