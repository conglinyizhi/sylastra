package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/schema"
	jsonschema "github.com/eino-contrib/jsonschema"

	"github.com/conglinyizhi/sylastra/internal/appmeta"
	"github.com/conglinyizhi/sylastra/internal/config"
)

type ToolBridge interface {
	List(context.Context) ([]*schema.ToolInfo, error)
	Call(context.Context, string, map[string]any) (string, error)
	Close() error
}

type StdioBridge struct {
	cfg config.MCPConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu     sync.Mutex
	nextID int64
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type listToolsResult struct {
	Tools []toolSpec `json:"tools"`
}

type toolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type callToolResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string         `json:"type"`
		Text string         `json:"text"`
		Data map[string]any `json:"_data"`
	} `json:"content"`
}

func NewStdioBridge(ctx context.Context, cfg config.MCPConfig) (*StdioBridge, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), flattenEnv(cfg.Env)...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp: %w", err)
	}

	bridge := &StdioBridge{
		cfg:    cfg,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}

	if err := bridge.initialize(ctx); err != nil {
		_ = bridge.Close()
		return nil, err
	}

	return bridge, nil
}

func (b *StdioBridge) List(ctx context.Context) ([]*schema.ToolInfo, error) {
	var result listToolsResult
	if err := b.call(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}

	out := make([]*schema.ToolInfo, 0, len(result.Tools))
	for _, tool := range result.Tools {
		info := &schema.ToolInfo{
			Name: tool.Name,
			Desc: tool.Description,
		}
		if len(tool.InputSchema) > 0 {
			var js jsonschema.Schema
			raw, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("marshal input schema for %s: %w", tool.Name, err)
			}
			if err := json.Unmarshal(raw, &js); err != nil {
				return nil, fmt.Errorf("decode input schema for %s: %w", tool.Name, err)
			}
			info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(&js)
		}
		out = append(out, info)
	}

	return out, nil
}

func (b *StdioBridge) Call(ctx context.Context, name string, input map[string]any) (string, error) {
	var result callToolResult
	params := map[string]any{
		"name":      name,
		"arguments": input,
	}
	if err := b.call(ctx, "tools/call", params, &result); err != nil {
		return "", err
	}
	texts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		if strings.TrimSpace(content.Text) != "" {
			texts = append(texts, content.Text)
		}
	}
	joined := strings.Join(texts, "\n")
	if result.IsError {
		if joined == "" {
			joined = "tool call failed"
		}
		return joined, fmt.Errorf("%s", joined)
	}
	return joined, nil
}

func (b *StdioBridge) Close() error {
	if b.cmd == nil {
		return nil
	}
	_ = b.stdin.Close()
	err := b.cmd.Process.Kill()
	_ = b.cmd.Wait()
	b.cmd = nil
	if err != nil && !strings.Contains(err.Error(), "process already finished") {
		return err
	}
	return nil
}

func (b *StdioBridge) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    appmeta.AppName,
			"version": "0.1.0",
		},
	}
	var result map[string]any
	if err := b.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize mcp: %w", err)
	}
	return nil
}

func (b *StdioBridge) call(ctx context.Context, method string, params any, out any) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := atomic.AddInt64(&b.nextID, 1)
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := b.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	type responseWrap struct {
		resp rpcResponse
		err  error
	}

	respCh := make(chan responseWrap, 1)
	go func() {
		line, err := b.stdout.ReadBytes('\n')
		if err != nil {
			respCh <- responseWrap{err: err}
			return
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			respCh <- responseWrap{err: fmt.Errorf("decode response: %w", err)}
			return
		}
		respCh <- responseWrap{resp: resp}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case wrapped := <-respCh:
		if wrapped.err != nil {
			return wrapped.err
		}
		if wrapped.resp.Error != nil {
			return fmt.Errorf("mcp error %d: %s", wrapped.resp.Error.Code, wrapped.resp.Error.Message)
		}
		if out == nil {
			return nil
		}
		if len(wrapped.resp.Result) == 0 {
			return nil
		}
		return json.Unmarshal(wrapped.resp.Result, out)
	}
}

func flattenEnv(env map[string]string) []string {
	items := make([]string, 0, len(env))
	for key, value := range env {
		items = append(items, key+"="+value)
	}
	return items
}
