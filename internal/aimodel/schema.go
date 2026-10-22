package aimodel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// decode rejects ambiguous JSON, including duplicate keys, trailing values and
// excessive nesting. It is used for provider envelopes as well as model output.
func decode(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := value(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	return v, nil
}

func value(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, fmt.Errorf("JSON nesting limit")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t {
	case json.Delim('{'):
		out := map[string]any{}
		for d.More() {
			token, e := d.Token()
			key, ok := token.(string)
			if e != nil || !ok {
				return nil, fmt.Errorf("invalid object key")
			}
			if _, exists := out[key]; exists {
				return nil, fmt.Errorf("duplicate key")
			}
			out[key], e = value(d, depth+1)
			if e != nil {
				return nil, e
			}
		}
		if _, err = d.Token(); err != nil {
			return nil, err
		}
		return out, nil
	case json.Delim('['):
		out := []any{}
		for d.More() {
			v, e := value(d, depth+1)
			if e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		if _, err = d.Token(); err != nil {
			return nil, err
		}
		return out, nil
	case json.Delim('}'), json.Delim(']'):
		return nil, fmt.Errorf("unexpected delimiter")
	default:
		return t, nil
	}
}

func object(properties map[string]any) map[string]any {
	keys := make([]string, 0, len(properties))
	// Stable order avoids sending a different schema on each model call.
	for _, key := range []string{"answer_id", "equivalent", "confidence", "reason", "operation", "agent_ids", "body", "summary", "suggestions"} {
		if _, ok := properties[key]; ok {
			keys = append(keys, key)
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": keys, "additionalProperties": false}
}

func taskSchema(task string) (map[string]any, error) {
	text := map[string]any{"type": "string"}
	switch task {
	case "answer_suggestions":
		return object(map[string]any{"suggestions": map[string]any{"type": "array", "items": object(map[string]any{"answer_id": text, "reason": text})}}), nil
	case "automatic_answer_match":
		return object(map[string]any{"answer_id": text, "equivalent": map[string]any{"type": "boolean"}, "confidence": map[string]any{"type": "number"}, "reason": text}), nil
	case "overseer_plan":
		return object(map[string]any{"operation": map[string]any{"type": "string", "enum": []string{"status", "message", "question", "task"}}, "agent_ids": map[string]any{"type": "array", "items": text}, "body": text, "summary": text}), nil
	case "overseer_summary":
		return object(map[string]any{"summary": text}), nil
	default:
		return nil, fmt.Errorf("unsupported assistance task")
	}
}

// validate enforces the small schema subset owned by taskSchema, independently
// of provider decoding guarantees. Semantic and permission checks follow in the engine.
func validate(v any, schema map[string]any) bool {
	switch schema["type"] {
	case "object":
		m, ok := v.(map[string]any)
		props := schema["properties"].(map[string]any)
		if !ok || len(m) != len(props) {
			return false
		}
		for key, property := range props {
			v, exists := m[key]
			if !exists || !validate(v, property.(map[string]any)) {
				return false
			}
		}
		return true
	case "array":
		a, ok := v.([]any)
		if !ok || len(a) > 100 {
			return false
		}
		for _, item := range a {
			if !validate(item, schema["items"].(map[string]any)) {
				return false
			}
		}
		return true
	case "string":
		s, ok := v.(string)
		if !ok || len(s) > 32768 {
			return false
		}
		if choices, exists := schema["enum"].([]string); exists {
			for _, choice := range choices {
				if s == choice {
					return true
				}
			}
			return false
		}
		return true
	case "number":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		f, err := n.Float64()
		return err == nil && f >= 0 && f <= 1
	case "boolean":
		_, ok := v.(bool)
		return ok
	}
	return false
}
