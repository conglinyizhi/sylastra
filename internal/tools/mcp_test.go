package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/conglinyizhi/sylastra/internal/config"
)

func TestStdioBridge(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_HELPER") == "1" {
		runMCPHelper()
		return
	}

	ctx := context.Background()
	bridge, err := NewStdioBridge(ctx, config.MCPConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestStdioBridge"},
		Env:     map[string]string{"GO_WANT_MCP_HELPER": "1"},
	})
	if err != nil {
		t.Fatalf("NewStdioBridge() error = %v", err)
	}
	defer bridge.Close()

	infos, err := bridge.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", infos)
	}

	out, err := bridge.Call(ctx, "echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if out != "hello" {
		t.Fatalf("Call() output = %q", out)
	}
}

func runMCPHelper() {
	enc := json.NewEncoder(os.Stdout)
	var req rpcRequest
	dec := json.NewDecoder(os.Stdin)
	for dec.Decode(&req) == nil {
		switch req.Method {
		case "initialize":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"ok": true}})
		case "tools/list":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "echo",
						"description": "echo tool",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"text": map[string]any{"type": "string"},
							},
						},
					}},
				},
			})
		case "tools/call":
			var params map[string]any
			_ = json.Unmarshal(mustJSON(req.Params), &params)
			args := params["arguments"].(map[string]any)
			text, _ := args["text"].(string)
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": text}},
				},
			})
		default:
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": fmt.Sprintf("unknown method %s", req.Method)}})
		}
	}
	os.Exit(0)
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

var _ = exec.ErrNotFound
