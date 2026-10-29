package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSlackManifestUsesStableCallbacksAndMinimalScopes(t *testing.T) {
	out, err := BuildProviderSetup(Connection{ID: "chn_1", Kind: "slack", Secret: "never-export-this", Config: map[string]string{"bot_token": "nor-this"}}, "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	raw := string(JSON(out))
	if strings.Contains(raw, "never-export") || strings.Contains(raw, "nor-this") || strings.Contains(raw, "chat:write.public") {
		t.Fatal(raw)
	}
	settings := out.Manifest["settings"].(map[string]any)
	if settings["event_subscriptions"].(map[string]any)["request_url"] != "https://example.com/integrations/slack/chn_1/events" || out.Callbacks["interactivity"] != "https://example.com/integrations/slack/chn_1" {
		t.Fatal(out)
	}
	for _, base := range []string{"https://u:pass@example.com", "javascript:foo", "https://example.com?token=x", "https://example.com#x"} {
		if _, err := BuildProviderSetup(Connection{Kind: "slack"}, base); err == nil {
			t.Fatal(base)
		}
	}
	for _, p := range ProviderCatalog() {
		if p.Kind != "native" && p.Attachments {
			t.Fatal("native files overclaimed", p)
		}
		if len(p.Directions) == 0 || len(p.SetupModes) == 0 {
			t.Fatal(p)
		}
	}
}

func TestSetupDiagnosticNeverPromotesSavedCredentialsToConnected(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	c, err := s.SaveConnection(ctx, a, Connection{Name: "Slack", Kind: "slack", Enabled: true, Config: map[string]string{"team_id": "T1"}, Secret: string(JSON(map[string]string{"bot_token": "test-bot", "signing_secret": "test-signing-secret"}))})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.ConnectionSetup(ctx, a, c.ID, "https://example.com")
	if err != nil || out.Diagnostic.State != "configured" || out.Diagnostic.Qualification != "not_verified" || out.Diagnostic.Sends {
		t.Fatal(out, err)
	}
	person, err := s.CreatePerson(ctx, a, CreatePerson{ExternalID: "ordinary", Name: "Ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConnectionSetup(ctx, Actor{Principal: person, Scope: a.Scope}, c.ID, "https://example.com"); ErrorStatus(err) != 404 {
		t.Fatal("setup exposed", err)
	}
	c.Enabled = false
	c, err = s.SaveConnection(ctx, a, c)
	if err != nil {
		t.Fatal(err)
	}
	out, err = s.ConnectionSetup(ctx, a, c.ID, "https://example.com")
	if err != nil || out.Diagnostic.State != "disconnected" {
		t.Fatal(out, err)
	}
	var raw map[string]any
	if err = json.Unmarshal(JSON(out), &raw); err != nil {
		t.Fatal(err)
	}
}
