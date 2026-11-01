package httpapi

import (
	"context"
	"testing"

	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func TestAppearanceHTTPRequiresTargetManagement(t *testing.T) {
	_, admin, _ := setup(t)
	ctx := context.Background()
	worker, session, err := admin.Enroll(ctx, notif.EnrollInput{Agent: "appearance/builder", Name: "Builder"})
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"target": notif.Target{Kind: "agent", ID: session.Agent.ID}, "icon": "mark-5"}
	if err = worker.Do(ctx, "POST", "/api/v1/appearance", input, nil, ""); err == nil {
		t.Fatal("unmanaged worker changed its appearance")
	}
	if err = admin.Do(ctx, "POST", "/api/v1/appearance", input, nil, ""); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"items"`
	}
	if err = admin.Do(ctx, "POST", "/api/v1/query", map[string]any{"collection": "agents", "filters": map[string]string{"id": session.Agent.ID}}, &page, ""); err != nil || len(page.Items) != 1 || page.Items[0].Metadata["appearance_icon"] != "mark-5" {
		t.Fatal(page, err)
	}
}
