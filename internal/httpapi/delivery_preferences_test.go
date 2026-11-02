package httpapi

import (
	"context"
	"testing"

	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func TestDeliveryPreferencesSDKAndHeadlessContract(t *testing.T) {
	_, client, _ := setup(t)
	ctx := context.Background()
	var me struct {
		Person notif.Person `json:"person"`
	}
	if err := client.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil {
		t.Fatal(err)
	}
	p, err := client.DeliveryPreferences(ctx, me.Person.ID)
	if err != nil || p.Version != 0 || p.Default != nil {
		t.Fatal(p, err)
	}
	p.Default = &notif.ChannelPolicy{Mode: "preferred", Destinations: []string{}, FollowupSeconds: 0}
	updated, err := client.SaveDeliveryPreferences(ctx, me.Person.ID, p)
	if err != nil || updated.Version != 1 || updated.Default == nil {
		t.Fatal(updated, err)
	}
	read, err := client.DeliveryPreferences(ctx, me.Person.ID)
	if err != nil || read.Version != 1 || len(read.Default.Destinations) != 0 {
		t.Fatal(read, err)
	}
	if _, err = client.SaveDeliveryPreferences(ctx, me.Person.ID, p); err == nil {
		t.Fatal("stale SDK write accepted")
	}
}
