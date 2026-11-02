package runtime

import (
	"context"
	"github.com/beepgrid/beepgrid/internal/engine"
	sdk "github.com/beepgrid/beepgrid/sdk/go"
	"net/http/httptest"
	"testing"
)

func TestBudgetFanoutAttachmentsAndDeniedWorkAreAtomic(t *testing.T) {
	s, _ := service(t)
	ctx := context.Background()
	token := engine.Secret()
	if e := s.ProvisionWorkspace(ctx, "budget_bounds", "Bounds", token); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	owner := sdk.New(server.URL, token)
	_, one, e := owner.Enroll(ctx, sdk.EnrollInput{Agent: "one", Name: "One"})
	if e != nil {
		t.Fatal(e)
	}
	_, two, e := owner.Enroll(ctx, sdk.EnrollInput{Agent: "two", Name: "Two"})
	if e != nil {
		t.Fatal(e)
	}
	actor, e := s.store.Authenticate(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	group := engine.ID("grp")
	if _, e = s.store.Pool.Exec(ctx, `INSERT INTO groups(id,scope,name) VALUES($1,$2,'Both');`, group, actor.Scope); e != nil {
		t.Fatal(e)
	}
	for _, pid := range []string{one.Agent.ID, two.Agent.ID} {
		if _, e = s.store.Pool.Exec(ctx, `INSERT INTO memberships VALUES($1,$2,$3)`, actor.Scope, group, pid); e != nil {
			t.Fatal(e)
		}
	}
	b := WorkspaceBudget{Version: 1, Messages: 1, Requests: 0, UploadBytes: 3}
	if e = s.ApplyWorkspaceBudget(ctx, "budget_bounds", b); e != nil {
		t.Fatal(e)
	}
	if _, e = owner.Send(ctx, sdk.SendInput{Target: sdk.Target{Kind: "group", ID: group}, Body: "two recipients"}, "bulk"); e == nil {
		t.Fatal("partial fanout accepted")
	}
	u, e := s.WorkspaceUsage(ctx, "budget_bounds")
	if e != nil || len(u) != 0 {
		t.Fatal("failed fanout charged", u, e)
	}
	denied := sdk.New(server.URL, one.Token)
	if _, e = denied.Send(ctx, sdk.SendInput{Target: sdk.Target{Kind: "agent", ID: two.Agent.ID}, Body: "unauthorized"}, "denied"); e == nil {
		t.Fatal("missing permission accepted")
	}
	u, e = s.WorkspaceUsage(ctx, "budget_bounds")
	if e != nil || len(u) != 0 {
		t.Fatal("denied work charged", u, e)
	}
	msg, e := owner.Send(ctx, sdk.SendInput{Target: sdk.Target{Kind: "agent", ID: one.Agent.ID}, Body: "one"}, "one")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.store.SaveContactPermissions(ctx, actor, engine.ContactPermissionsInput{Subject: engine.Target{Kind: "agent", ID: one.Agent.ID}, Rules: []engine.Rule{{Action: "view", TargetKind: "person", TargetID: actor.ID}}}); e != nil {
		t.Fatal(e)
	}
	s.store.AttachmentsRoot = t.TempDir()
	if _, e = s.store.UploadAttachment(ctx, actor, msg.ConversationID, "too-big.txt", "text/plain", []byte("four")); engine.ErrorStatus(e) != 429 {
		t.Fatal("upload over limit", e)
	}
	if _, e = s.store.UploadAttachment(ctx, actor, msg.ConversationID, "fits.txt", "text/plain", []byte("yes")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.store.UploadAttachment(ctx, actor, msg.ConversationID, "over.txt", "text/plain", []byte("!")); engine.ErrorStatus(e) != 429 {
		t.Fatal("cumulative limit ignored", e)
	}
	files, e := s.store.Attachments(ctx, actor, msg.ConversationID)
	if e != nil || len(files) != 1 || files[0].Size != 3 {
		t.Fatal(files, e)
	}
	// A zero request allowance rejects the entire request and its event/audience.
	if _, e = owner.Ask(ctx, sdk.AskInput{Target: sdk.Target{Kind: "agent", ID: one.Agent.ID}, Title: "Question", Choices: []string{"Yes"}}, "q"); e == nil {
		t.Fatal("zero request allowance ignored")
	}
	b.Version = 2
	b.Messages = -1
	b.Requests = 1
	if e = s.ApplyWorkspaceBudget(ctx, "budget_bounds", b); e != nil {
		t.Fatal(e)
	}
	if _, e = owner.Ask(ctx, sdk.AskInput{Target: sdk.Target{Kind: "agent", ID: one.Agent.ID}, Title: "Question", Choices: []string{"Yes"}}, "q"); e != nil {
		t.Fatal(e)
	}
	if _, e = owner.Ask(ctx, sdk.AskInput{Target: sdk.Target{Kind: "agent", ID: one.Agent.ID}, Title: "Next", Choices: []string{"Yes"}}, "q2"); e == nil {
		t.Fatal("request quota ignored")
	}
	u, e = s.WorkspaceUsage(ctx, "budget_bounds")
	if e != nil {
		t.Fatal(e)
	}
	units := map[string]int64{}
	for _, item := range u {
		units[item.Metric] = item.Units
	}
	if units["requests"] != 1 || units["upload_bytes"] != 3 {
		t.Fatal(units)
	}
}
