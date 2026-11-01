package engine

import (
	"context"
	"testing"
)

func TestAppearanceIsScopedPresentationOnly(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "Builder")
	outsider := testPerson(t, s, owner, "Unprivileged")
	in := AppearanceInput{Target: Target{"agent", worker.ID}, Icon: "mark-4"}
	if _, err := s.SaveAppearance(ctx, outsider, in); ErrorStatus(err) != 403 {
		t.Fatal("unprivileged appearance write", err)
	}
	if _, err := s.SaveAppearance(ctx, owner, in); err != nil {
		t.Fatal(err)
	}
	var icon string
	if err := s.Pool.QueryRow(ctx, `SELECT metadata->>'appearance_icon' FROM principals WHERE scope=$1 AND id=$2`, owner.Scope, worker.ID).Scan(&icon); err != nil || icon != in.Icon {
		t.Fatal(icon, err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE principals SET metadata=metadata||'{"private_runtime_setting":"not-for-list"}'::jsonb WHERE scope=$1 AND id=$2`, owner.Scope, worker.ID); err != nil {
		t.Fatal(err)
	}
	sent, err := s.Send(ctx, owner, SendInput{Target: in.Target, Body: "Appearance projection"}, "appearance-projection")
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Query(ctx, owner, QueryInput{Collection: "conversations", Filters: map[string]string{"id": sent.ConversationID}})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	for _, p := range page.Items[0].(Conversation).Participants {
		if p.ID == worker.ID && (len(p.Metadata) != 1 || p.Metadata["appearance_icon"] != in.Icon) {
			t.Fatal("list appearance projection leaked unrelated metadata", p.Metadata)
		}
	}
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Builders", Purpose: "agent_role", Members: []string{worker.ID}})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.SaveAppearance(ctx, owner, AppearanceInput{Target: Target{"group", group.ID}, Icon: "role-2", Version: group.Version})
	if err != nil || changed.Version != group.Version+1 {
		t.Fatal(changed, err)
	}
	if _, err = s.SaveAppearance(ctx, owner, AppearanceInput{Target: Target{"group", group.ID}, Icon: "role-3", Version: group.Version}); ErrorStatus(err) != 409 {
		t.Fatal("stale role appearance", err)
	}
	group.Version = changed.Version
	group.Name = "Build team"
	saved, err := s.SaveGroup(ctx, owner, group)
	if err != nil || saved.Icon != "role-2" || len(saved.Members) != 1 || saved.Members[0] != worker.ID {
		t.Fatal("name or membership update erased appearance", saved, err)
	}
	groups, err := s.Groups(ctx, owner)
	if err != nil || len(groups) != 1 || groups[0].Icon != "role-2" {
		t.Fatal(groups, err)
	}
	in.Icon = ""
	if _, err = s.SaveAppearance(ctx, owner, in); err != nil {
		t.Fatal(err)
	}
	var retained string
	if err = s.Pool.QueryRow(ctx, `SELECT metadata->>'private_runtime_setting' FROM principals WHERE scope=$1 AND id=$2`, owner.Scope, worker.ID).Scan(&retained); err != nil || retained != "not-for-list" {
		t.Fatal("appearance overwrote runtime metadata", err)
	}
	var count int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM role_bindings WHERE scope=$1 AND subject_id=$2`, owner.Scope, worker.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("appearance changed authority", count, err)
	}
	for _, bad := range []string{"https://untrusted.test/icon.svg", "mark-12", "mark-01", "role-3", "mark--1"} {
		in.Icon = bad
		if _, err = s.SaveAppearance(ctx, owner, in); ErrorStatus(err) != 400 {
			t.Fatal(bad, err)
		}
	}
	in.Target.Kind = "person"
	in.Icon = ""
	if _, err = s.SaveAppearance(ctx, owner, in); ErrorStatus(err) != 400 {
		t.Fatal("person icon", err)
	}
	other, err := s.Bootstrap(ctx, owner.Scope+"other", "Other", "test", Secret())
	if err != nil {
		t.Fatal(err)
	}
	in.Target = Target{"agent", worker.ID}
	if _, err = s.SaveAppearance(ctx, other, in); ErrorStatus(err) != 404 {
		t.Fatal("cross workspace icon", err)
	}
}
