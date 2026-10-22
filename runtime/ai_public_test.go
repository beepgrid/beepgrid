package runtime_test

import (
	"encoding/json"
	"strings"
	"testing"

	server "github.com/notif-dev/notif/runtime"
)

func TestModelConfigurationIsUsableWithoutInternalPackages(t *testing.T) {
	values := map[string]string{"NOTIF_AI_PROVIDER": "google", "NOTIF_AI_MODEL": "explicit-selection", "NOTIF_AI_API_KEY": "fixture-private-key"}
	ai, err := server.AIConfigFromEnv(func(k string) string { return values[k] })
	if err != nil {
		t.Fatal(err)
	}
	c := server.Config{AI: server.AIConfig{Provider: ai.Provider, Model: ai.Model, APIKey: ai.APIKey}}
	if c.AI.Provider != "google" || c.AI.Model != "explicit-selection" {
		t.Fatal("public model configuration changed")
	}
	raw, _ := json.Marshal(c.AI)
	if strings.Contains(string(raw), "fixture-private-key") {
		t.Fatal("public config serialization exposed credentials")
	}
}
