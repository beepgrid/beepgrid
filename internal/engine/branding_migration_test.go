package engine

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestBrandingUpgradePreservesSavedFanoutReceipts(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	a := testWorker(t, s, owner, "rename-one")
	b := testWorker(t, s, owner, "rename-two")
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Upgrade recipients", Members: []string{a.ID, b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	input := SendInput{Target: Target{Kind: "group", ID: group.ID}, Body: "Keep the saved message intact"}
	before, err := s.Send(ctx, owner, input, "rename-replay")
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the persisted version-37 representation and function name.
	_, err = s.Pool.Exec(ctx, `UPDATE idempotency SET output = (output - '_beepgrid_body_v1') || jsonb_build_object('_notif_body_v1', output->'_beepgrid_body_v1') WHERE key='rename-replay'; ALTER FUNCTION beepgrid_meter_accepted() RENAME TO notif_meter_accepted; DELETE FROM schema_migrations WHERE version=38`)
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.TrimSuffix(s.Pool.Config().ConnConfig.RuntimeParams["search_path"], ",public")
	if err = Migrate(ctx, os.Getenv("BEEPGRID_TEST_DATABASE_URL"), schema); err != nil {
		t.Fatal(err)
	}
	after, err := s.Send(ctx, owner, input, "rename-replay")
	if err != nil || string(JSON(before)) != string(JSON(after)) {
		t.Fatal("upgrade changed the saved result", err)
	}
	var obsolete int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM idempotency WHERE output ? '_notif_body_v1'`).Scan(&obsolete); err != nil || obsolete != 0 {
		t.Fatal("obsolete persisted fields remain", obsolete, err)
	}
	var renamed bool
	if err = s.Pool.QueryRow(ctx, `SELECT to_regprocedure('beepgrid_meter_accepted()') IS NOT NULL`).Scan(&renamed); err != nil || !renamed {
		t.Fatal("metering function was not migrated", err)
	}
}
