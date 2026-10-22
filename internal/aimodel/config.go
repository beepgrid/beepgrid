// Package aimodel adapts bounded, tool-free assistance calls to model APIs.
// Authorization and durable effects remain in the engine.
package aimodel

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Config is operator configuration. There is deliberately no default provider or model.
// Options contains generation settings, never prompts, tools or credentials.
type Config struct {
	Provider     string
	Model        string
	APIKey       string `json:"-"`
	BaseURL      string
	Options      json.RawMessage
	OutputMode   string
	AllowPrivate bool
}

func (c Config) String() string { return "AI configuration (credentials redacted)" }

func FromEnv(get func(string) string) (Config, error) {
	c := Config{Provider: get("NOTIF_AI_PROVIDER"), Model: get("NOTIF_AI_MODEL"), APIKey: get("NOTIF_AI_API_KEY"), BaseURL: get("NOTIF_AI_BASE_URL"), Options: json.RawMessage(get("NOTIF_AI_OPTIONS")), OutputMode: get("NOTIF_AI_OUTPUT_MODE")}
	if v := get("NOTIF_AI_ALLOW_PRIVATE"); v != "" {
		var err error
		c.AllowPrivate, err = strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("NOTIF_AI_ALLOW_PRIVATE must be a boolean")
		}
	}
	return c, nil
}

func (c Config) Empty() bool {
	return c.Provider == "" && c.Model == "" && c.APIKey == "" && c.BaseURL == "" && len(c.Options) == 0 && c.OutputMode == "" && !c.AllowPrivate
}

type Provider struct {
	config   Config
	endpoint string
	options  map[string]any
}

// New validates configuration without network I/O or model discovery.
func New(c Config) (*Provider, error) {
	if !plain(c.Model, 256) || strings.TrimSpace(c.Model) != c.Model {
		return nil, fmt.Errorf("NOTIF_AI_MODEL must name an explicitly selected model")
	}
	defaults := map[string]string{"openai": "https://api.openai.com/v1", "anthropic": "https://api.anthropic.com/v1", "google": "https://generativelanguage.googleapis.com/v1beta", "openai-compatible": ""}
	base, ok := defaults[c.Provider]
	if !ok {
		return nil, fmt.Errorf("NOTIF_AI_PROVIDER must be openai, anthropic, google or openai-compatible")
	}
	if c.APIKey == "" && c.Provider != "openai-compatible" {
		return nil, fmt.Errorf("NOTIF_AI_API_KEY is required for this provider")
	}
	if c.APIKey != "" && !plain(c.APIKey, 8192) {
		return nil, fmt.Errorf("NOTIF_AI_API_KEY must be a single-line credential")
	}
	if c.BaseURL != "" {
		base = c.BaseURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("NOTIF_AI_BASE_URL must be an HTTP(S) base URL without credentials, query or fragment")
	}
	if c.OutputMode == "" {
		c.OutputMode = "schema"
		if c.Provider == "openai-compatible" {
			c.OutputMode = "json"
		}
	}
	if c.OutputMode != "schema" && c.OutputMode != "json" && c.OutputMode != "text" {
		return nil, fmt.Errorf("NOTIF_AI_OUTPUT_MODE must be schema, json or text")
	}
	if c.Provider == "anthropic" && c.OutputMode == "json" {
		return nil, fmt.Errorf("Anthropic output mode must be schema or text")
	}
	options := map[string]any{}
	if len(c.Options) != 0 {
		if len(c.Options) > 8192 {
			return nil, fmt.Errorf("NOTIF_AI_OPTIONS exceeds 8 KiB")
		}
		v, e := decode(c.Options)
		if e != nil {
			return nil, fmt.Errorf("NOTIF_AI_OPTIONS must be one JSON object without duplicate keys")
		}
		var object bool
		options, object = v.(map[string]any)
		if !object {
			return nil, fmt.Errorf("NOTIF_AI_OPTIONS must be a JSON object")
		}
	}
	allowed := map[string]string{
		"openai":            "max_output_tokens temperature top_p reasoning service_tier text",
		"anthropic":         "max_tokens temperature top_p top_k thinking output_config",
		"google":            "maxOutputTokens temperature topP topK seed thinkingConfig stopSequences",
		"openai-compatible": "max_tokens max_completion_tokens temperature top_p top_k min_p seed frequency_penalty presence_penalty reasoning_effort reasoning thinking repeat_penalty stop",
	}
	for k, v := range options {
		if !slices.Contains(strings.Fields(allowed[c.Provider]), k) {
			return nil, fmt.Errorf("NOTIF_AI_OPTIONS contains an unsupported or reserved setting")
		}
		// These API objects also contain protected formatting fields. Only the
		// generation controls below may be merged with our engine-owned schema.
		if k == "text" || k == "output_config" || k == "reasoning" || k == "thinking" || k == "thinkingConfig" {
			nested, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("NOTIF_AI_OPTIONS contains an invalid generation object")
			}
			keys := map[string]string{"text": "verbosity", "output_config": "effort", "reasoning": "effort summary enabled max_tokens", "thinking": "type budget_tokens", "thinkingConfig": "thinkingBudget thinkingLevel includeThoughts"}
			if c.Provider == "openai" {
				keys["reasoning"] = "effort summary"
			}
			for name := range nested {
				if !slices.Contains(strings.Fields(keys[k]), name) {
					return nil, fmt.Errorf("NOTIF_AI_OPTIONS attempts to override a protected generation field")
				}
			}
		}
		if strings.HasPrefix(k, "max_") || k == "maxOutputTokens" {
			n, ok := v.(json.Number)
			if !ok {
				return nil, fmt.Errorf("Output token limits must be positive integers")
			}
			limit, e := n.Int64()
			if e != nil || limit < 1 || limit > 65536 {
				return nil, fmt.Errorf("Output token limits must be between 1 and 65536")
			}
		}
	}
	path := strings.TrimRight(u.Path, "/")
	switch c.Provider {
	case "openai":
		u.Path = path + "/responses"
	case "anthropic":
		u.Path = path + "/messages"
	case "google":
		model := strings.TrimPrefix(c.Model, "models/")
		if strings.ContainsAny(model, "/%?#:") || model == "" || model == "." || model == ".." {
			return nil, fmt.Errorf("Google model must be a model ID, optionally prefixed by models/")
		}
		u.Path = path + "/models/" + model + ":generateContent"
	case "openai-compatible":
		u.Path = path + "/chat/completions"
	}
	u.RawPath = ""
	return &Provider{config: c, endpoint: u.String(), options: options}, nil
}

func (p *Provider) Endpoint() string { return p.endpoint }

func plain(v string, max int) bool {
	return v != "" && len(v) <= max && utf8.ValidString(v) && !strings.ContainsFunc(v, unicode.IsControl)
}
