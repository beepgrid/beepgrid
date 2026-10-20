package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/notif-dev/notif/sdk/go/notif"
	"io"
)

type tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

func schema(required []string, properties map[string]any) any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func textType() any { return map[string]string{"type": "string"} }
func Tools() any {
	return []tool{
		{"list_agents", "List agents visible to the authenticated caller.", schema(nil, map[string]any{})},
		{"send", "Send a message to a person, agent or group, or reply to a conversation.", schema([]string{"body"}, map[string]any{"body": textType(), "target": map[string]any{"type": "object", "properties": map[string]any{"kind": textType(), "id": textType()}, "required": []string{"kind", "id"}}, "conversation_id": textType(), "title": textType()})},
		{"ask", "Create a durable question or approval and return its handle. The caller may continue working.", schema([]string{"target", "title"}, map[string]any{"target": map[string]any{"type": "object"}, "title": textType(), "body": textType(), "kind": textType(), "choices": map[string]any{"type": "array", "items": textType()}, "action": map[string]any{"type": "object"}, "shared_key": textType()})},
		{"get_request", "Read the current result of a request without cancelling it.", schema([]string{"id"}, map[string]any{"id": textType()})},
		{"reply", "Respond to an eligible question, approval or task. Current permissions are enforced.", schema([]string{"id", "outcome", "version"}, map[string]any{"id": textType(), "outcome": textType(), "version": map[string]string{"type": "integer"}, "answer": map[string]any{}})},
		{"request_status", "List requests visible to this caller.", schema(nil, map[string]any{})},
	}
}
func Serve(ctx context.Context, c *notif.Client, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 131072)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if e := json.Unmarshal(scanner.Bytes(), &req); e != nil {
			if e = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "Invalid JSON"}}); e != nil {
				return e
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		var result any
		var rpcError any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]bool{"listChanged": false}}, "serverInfo": map[string]string{"name": "notif", "version": "0.1.0-dev"}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": Tools(), "resultType": "complete"}
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(req.Params, &p) != nil {
				rpcError = map[string]any{"code": -32602, "message": "Invalid tool parameters"}
				break
			}
			value, e := call(ctx, c, p.Name, p.Arguments)
			if e != nil {
				result = map[string]any{"resultType": "complete", "isError": true, "content": []any{map[string]string{"type": "text", "text": e.Error()}}}
			} else {
				b, _ := json.Marshal(value)
				result = map[string]any{"resultType": "complete", "isError": false, "content": []any{map[string]string{"type": "text", "text": string(b)}}}
			}
		default:
			rpcError = map[string]any{"code": -32601, "message": "Method not found"}
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcError != nil {
			response["error"] = rpcError
		} else {
			response["result"] = result
		}
		if e := enc.Encode(response); e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return scanner.Err()
}
func call(ctx context.Context, c *notif.Client, name string, args json.RawMessage) (any, error) {
	var value any
	switch name {
	case "list_agents":
		e := c.Do(ctx, "GET", "/api/v1/agents", nil, &value, "")
		return value, e
	case "request_status":
		e := c.Do(ctx, "GET", "/api/v1/requests", nil, &value, "")
		return value, e
	case "send":
		var in notif.SendInput
		if e := json.Unmarshal(args, &in); e != nil {
			return nil, e
		}
		return c.Send(ctx, in, notif.Key())
	case "ask":
		var in notif.AskInput
		if e := json.Unmarshal(args, &in); e != nil {
			return nil, e
		}
		return c.Ask(ctx, in, notif.Key())
	case "get_request":
		var in struct {
			ID string `json:"id"`
		}
		if e := json.Unmarshal(args, &in); e != nil {
			return nil, e
		}
		if in.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		return c.GetRequest(ctx, in.ID)
	case "reply":
		var in struct {
			ID string `json:"id"`
			notif.Response
		}
		if e := json.Unmarshal(args, &in); e != nil {
			return nil, e
		}
		return c.Respond(ctx, in.ID, in.Response)
	}
	return nil, fmt.Errorf("unknown tool")
}
