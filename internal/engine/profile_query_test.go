package engine

import (
	"context"
	"testing"
)

func TestGroupMemberFilterUsesCurrentIdentityAndGroupVisibility(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	reader := testPerson(t, s, owner, "reader")
	member := testPerson(t, s, owner, "member")
	group, err := s.SaveGroup(ctx, owner, Group{Name: "Engineering", Purpose: "team", Members: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.SaveGroup(ctx, owner, Group{Name: "Other", Purpose: "team", Members: []string{owner.ID}})
	if err != nil {
		t.Fatal(err)
	}
	in := QueryInput{Collection: "groups", Filters: map[string]string{"member_id": member.ID, "purpose": "team"}}
	page, err := s.Query(ctx, owner, in)
	if err != nil || len(page.Items) != 1 || page.Items[0].(Group).ID != group.ID {
		t.Fatal(page, err)
	}
	if _, err = s.Query(ctx, reader, in); ErrorStatus(err) != 404 {
		t.Fatal("hidden identity must not expose memberships", err)
	}
	grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "person", TargetID: member.ID}})
	page, err = s.Query(ctx, reader, in)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("seeing member does not reveal hidden groups", page, err)
	}
	policy := grant(t, s, owner, reader, []Rule{{Action: "view", TargetKind: "group", TargetID: group.ID}})
	page, err = s.Query(ctx, reader, in)
	if err != nil || len(page.Items) != 1 || page.Items[0].(Group).ID == other.ID {
		t.Fatal(page, err)
	}
	if err = s.BindRole(ctx, owner, Binding{RoleID: policy.ID, SubjectKind: "principal", SubjectID: reader.ID}, true); err != nil {
		t.Fatal(err)
	}
	page, err = s.Query(ctx, reader, in)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("revoked group visibility must take effect", page, err)
	}
	in.Filters["member_id"] = "missing"
	if _, err = s.Query(ctx, owner, in); ErrorStatus(err) != 404 {
		t.Fatal(err)
	}
}
