package httpapi

import (
	"context"
	"errors"
	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/sdk/go"
	"net/http"
	"strings"
	"testing"
)

func TestWorkspaceAccountBridgeScopeAndSuspension(t *testing.T) {
	store, admin, server := setup(t)
	ctx := context.Background()
	owner, e := store.Authenticate(ctx, admin.Token)
	if e != nil {
		t.Fatal(e)
	}
	api := server.Config.Handler.(*Server)
	api.Deployment = "managed"
	calls := 0
	api.AccountReader = func(ctx context.Context, subject ManagedAccountSubject) (ManagedAccount, error) {
		calls++
		if subject.Scope != owner.Scope || subject.PersonID != owner.ID {
			t.Fatal(subject)
		}
		return ManagedAccount{ID: "test", Company: "Company", State: "active"}, nil
	}
	var result map[string]any
	if e = admin.Do(ctx, "GET", "/api/v1/workspace/account", nil, &result, ""); e != nil || result["deployment"] != "managed" || calls != 1 {
		t.Fatal(result, e, calls)
	}
	person, e := store.CreatePerson(ctx, owner, engine.CreatePerson{ExternalID: "employee", Name: "Employee"})
	if e != nil {
		t.Fatal(e)
	}
	credential, e := store.NewCredential(ctx, owner, person.ID, "api", "")
	if e != nil {
		t.Fatal(e)
	}
	employee := beepgrid.New(server.URL, credential["token"].(string))
	if e = employee.Do(ctx, "GET", "/api/v1/workspace/account", nil, &result, ""); e == nil || calls != 1 {
		t.Fatal("ordinary employee triggered server bridge", e, calls)
	}
	if e = store.ApplyWorkspaceAccess(ctx, owner.Scope, 1, true, owner.CreatedAt); e != nil {
		t.Fatal(e)
	}
	if e = admin.Do(ctx, "GET", "/api/v1/people", nil, &result, ""); e == nil {
		t.Fatal("suspension bypassed normal route")
	}
	if e = admin.Do(ctx, "GET", "/api/v1/workspace/account", nil, &result, ""); e != nil || calls != 2 {
		t.Fatal("account unavailable during suspension", e)
	}
	api.AccountReader = func(context.Context, ManagedAccountSubject) (ManagedAccount, error) {
		return ManagedAccount{}, errors.New("private provider failure")
	}
	if e = admin.Do(ctx, "GET", "/api/v1/workspace/account", nil, &result, ""); e == nil || !strings.Contains(e.Error(), "ACCOUNT_UNAVAILABLE") || strings.Contains(e.Error(), "private provider") {
		t.Fatal(e)
	}
	// Browser query parameters never select another account or enable mutations.
	request, _ := http.NewRequest("POST", server.URL+"/api/v1/workspace/account", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer "+admin.Token)
	response, e := server.Client().Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode == 200 {
		t.Fatal("mutable account proxy")
	}
}
func TestAccountBridgeRechecksRevocationBeforeReturning(t *testing.T) {
	store, admin, server := setup(t)
	ctx := context.Background()
	owner, e := store.Authenticate(ctx, admin.Token)
	if e != nil {
		t.Fatal(e)
	}
	p, e := store.CreatePerson(ctx, owner, engine.CreatePerson{Name: "Account reader", ExternalID: "reader"})
	if e != nil {
		t.Fatal(e)
	}
	role, e := store.SaveRole(ctx, owner, engine.Role{Name: "Read own account", Rules: []engine.Rule{{Action: "view", TargetKind: "account", TargetID: "workspace"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = store.BindRole(ctx, owner, engine.Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: p.ID}, false); e != nil {
		t.Fatal(e)
	}
	key, e := store.NewCredential(ctx, owner, p.ID, "api", "")
	if e != nil {
		t.Fatal(e)
	}
	api := server.Config.Handler.(*Server)
	api.Deployment = "managed"
	api.AccountReader = func(context.Context, ManagedAccountSubject) (ManagedAccount, error) {
		if e := store.Revoke(ctx, owner, key["id"].(string)); e != nil {
			t.Fatal(e)
		}
		return ManagedAccount{ID: "only-local"}, nil
	}
	reader := beepgrid.New(server.URL, key["token"].(string))
	var out map[string]any
	if e = reader.Do(ctx, "GET", "/api/v1/workspace/account", nil, &out, ""); e == nil {
		t.Fatal("revoked credential received late managed data")
	}
}

func TestWorkspaceSDKQueriesCapabilitiesAndPreviewedBatches(t *testing.T) {
	_, admin, _ := setup(t)
	ctx := context.Background()
	_, session, e := admin.Enroll(ctx, beepgrid.EnrollInput{Agent: "workspace-sdk", Name: "Workspace worker"})
	if e != nil {
		t.Fatal(e)
	}
	target := beepgrid.Target{Kind: "agent", ID: session.Agent.ID}
	preview, e := admin.DispatchPreview(ctx, beepgrid.DispatchPreviewInput{Targets: []beepgrid.Target{target}, Operation: "message"})
	if e != nil || preview.Sends || preview.Token == "" {
		t.Fatal(preview, e)
	}
	sent, e := admin.SendBatch(ctx, beepgrid.SendBatchInput{Targets: []beepgrid.Target{target, target}, Body: "one logical recipient", PreviewToken: preview.Token}, "sdk-batch")
	if e != nil || len(sent) != 1 {
		t.Fatal(sent, e)
	}
	deliveries, e := admin.Query(ctx, beepgrid.QueryInput{Collection: "deliveries", Filters: map[string]string{"operation_id": sent[0].Result.Messages[0].ID}})
	if e != nil || len(deliveries.Items) != 1 {
		t.Fatal(deliveries, e)
	}
	page, e := admin.Query(ctx, beepgrid.QueryInput{Collection: "messages", Filters: map[string]string{"conversation_id": sent[0].Result.ConversationID}, Limit: 1})
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	var contactPolicy map[string]any
	if e = admin.Do(ctx, "POST", "/api/v1/contact-permissions", map[string]any{"subject": target, "rules": []map[string]string{{"action": "view", "target_kind": "person", "target_id": adminPersonID(t, admin)}}, "version": 0}, &contactPolicy, ""); e != nil {
		t.Fatal(e)
	}
	requests, e := admin.AskBatch(ctx, beepgrid.AskBatchInput{Targets: []beepgrid.Target{target}, Request: beepgrid.AskInput{Title: "Progress?"}}, "sdk-ask-batch")
	if e != nil || len(requests) != 1 {
		t.Fatal(requests, e)
	}
	caps, e := admin.Capabilities(ctx, beepgrid.CapabilityInput{RequestID: requests[0].Request.ID})
	if e != nil || !caps.Operations["cancel"] || caps.RequestVersion != 1 {
		t.Fatal(caps, e)
	}
	if _, e = admin.RequestTrace(ctx, requests[0].Request.ID, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Workspace(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Access(ctx, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Account(ctx); e != nil {
		t.Fatal(e)
	}
}

func adminPersonID(t *testing.T, client *beepgrid.Client) string {
	t.Helper()
	var me struct {
		Person struct {
			ID string `json:"id"`
		} `json:"person"`
	}
	if err := client.Do(context.Background(), "GET", "/api/v1/me", nil, &me, ""); err != nil {
		t.Fatal(err)
	}
	return me.Person.ID
}
