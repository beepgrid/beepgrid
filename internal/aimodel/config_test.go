package aimodel

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestExplicitConfigurationAndEnvironment(t *testing.T) {
	if !(Config{}).Empty() {
		t.Fatal("unconfigured assistance must stay absent")
	}
	values := map[string]string{"NOTIF_AI_PROVIDER": "openai-compatible", "NOTIF_AI_MODEL": "chosen/model", "NOTIF_AI_BASE_URL": "http://localhost:11434/v1", "NOTIF_AI_ALLOW_PRIVATE": "true", "NOTIF_AI_OPTIONS": `{"temperature":0.2,"max_tokens":2048}`}
	c, err := FromEnv(func(k string) string { return values[k] })
	if err != nil || c.Model != "chosen/model" || !c.AllowPrivate {
		t.Fatal("environment configuration failed", err)
	}
	p, err := New(c)
	if err != nil || p.Endpoint() != "http://localhost:11434/v1/chat/completions" || p.config.OutputMode != "json" {
		t.Fatal("compatible endpoint failed", err)
	}
	delete(values, "NOTIF_AI_MODEL")
	c, _ = FromEnv(func(k string) string { return values[k] })
	if _, err := New(c); err == nil {
		t.Fatal("selected a default model")
	}
	values["NOTIF_AI_ALLOW_PRIVATE"] = "sometimes"
	if _, err := FromEnv(func(k string) string { return values[k] }); err == nil {
		t.Fatal("invalid private-address choice accepted")
	}
}

func TestInvalidConfigurationDoesNotExposeCredentials(t *testing.T) {
	base := Config{Provider: "openai", Model: "selected-model", APIKey: "fixture-secret"}
	cases := map[string]func(*Config){
		"no provider":               func(c *Config) { c.Provider = "" },
		"unknown provider":          func(c *Config) { c.Provider = "magic" },
		"no model":                  func(c *Config) { c.Model = "" },
		"model whitespace":          func(c *Config) { c.Model = " chosen " },
		"model control":             func(c *Config) { c.Model = "a\nb" },
		"no key":                    func(c *Config) { c.APIKey = "" },
		"header injection":          func(c *Config) { c.APIKey = "fixture-secret\r\nx: y" },
		"url credentials":           func(c *Config) { c.BaseURL = "https://fixture-secret@example.com/v1" },
		"url query":                 func(c *Config) { c.BaseURL = "https://example.com/v1?key=fixture-secret" },
		"url fragment":              func(c *Config) { c.BaseURL = "https://example.com/#fixture-secret" },
		"url scheme":                func(c *Config) { c.BaseURL = "file:///tmp/model" },
		"missing compatible base":   func(c *Config) { c.Provider = "openai-compatible" },
		"invalid mode":              func(c *Config) { c.OutputMode = "auto-fallback" },
		"anthropic json mode":       func(c *Config) { c.Provider = "anthropic"; c.OutputMode = "json" },
		"google model path":         func(c *Config) { c.Provider = "google"; c.Model = "../elsewhere" },
		"options null":              func(c *Config) { c.Options = []byte(`null`) },
		"options array":             func(c *Config) { c.Options = []byte(`[]`) },
		"options trailing":          func(c *Config) { c.Options = []byte(`{} {}`) },
		"options duplicate":         func(c *Config) { c.Options = []byte(`{"temperature":0,"temperature":1}`) },
		"model override":            func(c *Config) { c.Options = []byte(`{"model":"fixture-secret"}`) },
		"input override":            func(c *Config) { c.Options = []byte(`{"input":"fixture-secret"}`) },
		"hidden conversation":       func(c *Config) { c.Options = []byte(`{"previous_response_id":"fixture-secret"}`) },
		"tools":                     func(c *Config) { c.Options = []byte(`{"tools":[]}`) },
		"key whitespace trick":      func(c *Config) { c.Options = []byte(`{"temperature top_p":0}`) },
		"schema override":           func(c *Config) { c.Options = []byte(`{"text":{"format":{}}}`) },
		"store override":            func(c *Config) { c.Options = []byte(`{"store":true}`) },
		"invalid generation object": func(c *Config) { c.Options = []byte(`{"reasoning":false}`) },
		"invalid max tokens":        func(c *Config) { c.Options = []byte(`{"max_output_tokens":1.5}`) },
		"excess max tokens":         func(c *Config) { c.Options = []byte(`{"max_output_tokens":65537}`) },
		"negative max tokens":       func(c *Config) { c.Options = []byte(`{"max_output_tokens":-1}`) },
		"options too large":         func(c *Config) { c.Options = []byte(strings.Repeat(" ", 8193)) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			c := base
			change(&c)
			_, err := New(c)
			if err == nil || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("invalid configuration accepted or credential exposed")
			}
		})
	}
	raw, _ := json.Marshal(base)
	if strings.Contains(string(raw), base.APIKey) || strings.Contains(fmt.Sprint(base), base.APIKey) {
		t.Fatal("configuration formatting exposed a credential")
	}
}

func TestOfficialEndpointsAndNativeOptions(t *testing.T) {
	for _, tc := range []struct{ provider, endpoint, options string }{
		{"openai", "https://api.openai.com/v1/responses", `{"reasoning":{"effort":"high"},"text":{"verbosity":"low"}}`},
		{"anthropic", "https://api.anthropic.com/v1/messages", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},"max_tokens":8192}`},
		{"google", "https://generativelanguage.googleapis.com/v1beta/models/chosen:generateContent", `{"thinkingConfig":{"thinkingLevel":"high"},"maxOutputTokens":8192}`},
	} {
		p, err := New(Config{Provider: tc.provider, Model: "chosen", APIKey: "fixture", Options: []byte(tc.options)})
		if err != nil || p.Endpoint() != tc.endpoint || p.config.Model != "chosen" {
			t.Fatal(tc.provider, err)
		}
	}
}
