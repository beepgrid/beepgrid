package httpapi

import (
	"context"
	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/sdk/go"
	"testing"
)

func TestAnycastHTTPWithExistingSDKTransport(t *testing.T) {
	_, admin, _ := setup(t)
	ctx := context.Background()
	worker, session, e := admin.Enroll(ctx, beepgrid.EnrollInput{Agent: "builder", Name: "Builder"})
	if e != nil {
		t.Fatal(e)
	}
	var role engine.Group
	if e = admin.Do(ctx, "POST", "/api/v1/groups", map[string]any{"name": "Builder", "purpose": "agent_role", "members": []string{session.Agent.ID}}, &role, ""); e != nil {
		t.Fatal(e)
	}
	if e = worker.Heartbeat(ctx, map[string]any{"anycast": map[string]any{"capacity": 1}}); e != nil {
		t.Fatal(e)
	}
	var dispatch engine.AnycastDispatch
	input := map[string]any{"role_id": role.ID, "body": "Inspect"}
	if e = admin.Do(ctx, "POST", "/api/v1/anycast", input, &dispatch, "http-one"); e != nil || dispatch.State != "assigned" {
		t.Fatal(dispatch, e)
	}
	var copy engine.AnycastDispatch
	if e = admin.Do(ctx, "POST", "/api/v1/anycast", input, &copy, "http-one"); e != nil || copy.ID != dispatch.ID {
		t.Fatal(copy, e)
	}
	var inbox []struct {
		ID         string `json:"id"`
		Generation int64  `json:"generation"`
		Payload    struct {
			AnycastID string `json:"anycast_id"`
		} `json:"payload"`
	}
	e = worker.Do(ctx, "GET", "/api/v1/worker/inbox", nil, &inbox, "")
	if e != nil || len(inbox) != 1 || inbox[0].Payload.AnycastID != dispatch.ID {
		t.Fatal(inbox, e)
	}
	if e = worker.Ack(ctx, beepgrid.Delivery{ID: inbox[0].ID, Generation: inbox[0].Generation}); e != nil {
		t.Fatal(e)
	}
	if e = worker.Do(ctx, "POST", "/api/v1/worker/anycast/"+dispatch.ID+"/complete", map[string]any{}, nil, ""); e != nil {
		t.Fatal(e)
	}
	if e = admin.Do(ctx, "GET", "/api/v1/anycast/"+dispatch.ID, nil, &dispatch, ""); e != nil || dispatch.State != "completed" {
		t.Fatal(dispatch, e)
	}
	// Restricted credentials cannot use the new endpoint to bypass token limits.
	var me struct {
		Person struct {
			ID string `json:"id"`
		} `json:"person"`
	}
	if e = admin.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); e != nil {
		t.Fatal(e)
	}
	var issued struct {
		Token string `json:"token"`
	}
	if e = admin.Do(ctx, "POST", "/api/v1/credentials", map[string]any{"principal_id": me.Person.ID, "kind": "api", "permissions": []string{"read"}}, &issued, ""); e != nil {
		t.Fatal(e)
	}
	readOnly := beepgrid.New(admin.BaseURL, issued.Token)
	if e = readOnly.Do(ctx, "POST", "/api/v1/anycast", input, &copy, "denied"); e == nil {
		t.Fatal("read-only token sent work")
	}
}
