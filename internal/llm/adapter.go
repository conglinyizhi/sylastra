package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/conglinyizhi/sylastra/internal/config"
)

type Adapter struct {
	profile config.LLMProfile
	client  *http.Client
	tools   []*schema.ToolInfo
	apiKey  string
}

func Build(_ context.Context, profile config.LLMProfile) (model.ToolCallingChatModel, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	apiKey, err := profile.ResolvedAPIKey()
	if err != nil {
		return nil, err
	}

	return &Adapter{
		profile: profile,
		client: &http.Client{
			Timeout: profile.HTTPTimeout(),
		},
		apiKey: apiKey,
	}, nil
}

func (a *Adapter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	next := *a
	next.tools = append([]*schema.ToolInfo(nil), tools...)
	return &next, nil
}

func (a *Adapter) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	payload, endpoint, err := a.buildPayload(input, false, opts...)
	if err != nil {
		return nil, err
	}

	respBody, err := a.doJSONRequest(ctx, endpoint, payload)
	if err != nil {
		return nil, err
	}

	return a.parseNonStream(respBody)
}

func (a *Adapter) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	payload, endpoint, err := a.buildPayload(input, true, opts...)
	if err != nil {
		return nil, err
	}

	reader, writer := schema.Pipe[*schema.Message](32)
	go func() {
		defer writer.Close()
		req, err := a.newRequest(ctx, endpoint, payload)
		if err != nil {
			_ = writer.Send(nil, err)
			return
		}
		resp, err := a.client.Do(req)
		if err != nil {
			_ = writer.Send(nil, err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			_ = writer.Send(nil, fmt.Errorf("llm request failed: %s: %s", resp.Status, strings.TrimSpace(string(body))))
			return
		}
		if err := a.parseStream(resp.Body, writer); err != nil {
			_ = writer.Send(nil, err)
		}
	}()

	return reader, nil
}

func (a *Adapter) buildPayload(messages []*schema.Message, stream bool, opts ...model.Option) (any, string, error) {
	common := model.GetCommonOptions(nil, opts...)
	tools := common.Tools
	if tools == nil {
		tools = a.tools
	}

	switch a.profile.APIStyle {
	case config.APIStyleOpenAIChat:
		payload, err := a.buildOpenAIChatPayload(messages, tools, stream, common)
		return payload, resolveEndpoint(a.profile.BaseURL, "/chat/completions"), err
	case config.APIStyleOpenAIResponses:
		payload, err := a.buildOpenAIResponsesPayload(messages, tools, stream, common)
		return payload, resolveEndpoint(a.profile.BaseURL, "/responses"), err
	case config.APIStyleAnthropicMessages:
		payload, err := a.buildAnthropicPayload(messages, tools, stream, common)
		return payload, resolveEndpoint(a.profile.BaseURL, "/messages"), err
	default:
		return nil, "", fmt.Errorf("unsupported api style %q", a.profile.APIStyle)
	}
}

func (a *Adapter) doJSONRequest(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	req, err := a.newRequest(ctx, endpoint, payload)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (a *Adapter) newRequest(ctx context.Context, endpoint string, payload any) (*http.Request, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	switch a.profile.APIStyle {
	case config.APIStyleAnthropicMessages:
		req.Header.Set("x-api-key", a.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	for key, value := range a.profile.Headers {
		req.Header.Set(key, value)
	}

	return req, nil
}

func (a *Adapter) parseNonStream(body []byte) (*schema.Message, error) {
	switch a.profile.APIStyle {
	case config.APIStyleOpenAIChat:
		var resp openAIChatResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		if len(resp.Choices) == 0 {
			return nil, errors.New("openai chat response has no choices")
		}
		return resp.Choices[0].Message.toSchemaMessage(), nil
	case config.APIStyleOpenAIResponses:
		var resp openAIResponsesResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		return resp.toSchemaMessage()
	case config.APIStyleAnthropicMessages:
		var resp anthropicResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		return resp.toSchemaMessage()
	default:
		return nil, fmt.Errorf("unsupported api style %q", a.profile.APIStyle)
	}
}

func (a *Adapter) parseStream(body io.Reader, writer *schema.StreamWriter[*schema.Message]) error {
	return consumeSSE(body, func(eventType string, data []byte) error {
		if len(data) == 0 || string(data) == "[DONE]" {
			return nil
		}
		switch a.profile.APIStyle {
		case config.APIStyleOpenAIChat:
			return a.handleOpenAIChatStream(data, writer)
		case config.APIStyleOpenAIResponses:
			return a.handleOpenAIResponsesStream(eventType, data, writer)
		case config.APIStyleAnthropicMessages:
			return a.handleAnthropicStream(eventType, data, writer)
		default:
			return fmt.Errorf("unsupported api style %q", a.profile.APIStyle)
		}
	})
}

func (a *Adapter) buildOpenAIChatPayload(messages []*schema.Message, tools []*schema.ToolInfo, stream bool, opts *model.Options) (map[string]any, error) {
	payload := map[string]any{
		"model":    chooseString(opts.Model, a.profile.Model),
		"messages": toOpenAIChatMessages(messages),
		"stream":   stream,
	}
	a.applyGenerationSettings(payload, opts, false)
	if len(tools) > 0 {
		converted, err := toOpenAIChatTools(tools)
		if err != nil {
			return nil, err
		}
		payload["tools"] = converted
	}
	return payload, nil
}

func (a *Adapter) buildOpenAIResponsesPayload(messages []*schema.Message, tools []*schema.ToolInfo, stream bool, opts *model.Options) (map[string]any, error) {
	payload := map[string]any{
		"model":  chooseString(opts.Model, a.profile.Model),
		"input":  toOpenAIResponsesInput(messages),
		"stream": stream,
	}
	a.applyGenerationSettings(payload, opts, true)
	if len(tools) > 0 {
		converted, err := toOpenAIResponsesTools(tools)
		if err != nil {
			return nil, err
		}
		payload["tools"] = converted
	}
	return payload, nil
}

func (a *Adapter) buildAnthropicPayload(messages []*schema.Message, tools []*schema.ToolInfo, stream bool, opts *model.Options) (map[string]any, error) {
	systemText, chatMessages, err := toAnthropicMessages(messages)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"model":      chooseString(opts.Model, a.profile.Model),
		"system":     systemText,
		"messages":   chatMessages,
		"stream":     stream,
		"max_tokens": chooseInt(opts.MaxTokens, a.profile.MaxTokens, 1024),
	}
	if temp := chooseFloat(opts.Temperature, a.profile.Temperature); temp != nil {
		payload["temperature"] = *temp
	}
	if len(tools) > 0 {
		converted, err := toAnthropicTools(tools)
		if err != nil {
			return nil, err
		}
		payload["tools"] = converted
	}
	return payload, nil
}

func (a *Adapter) applyGenerationSettings(payload map[string]any, opts *model.Options, responsesStyle bool) {
	if temp := chooseFloat(opts.Temperature, a.profile.Temperature); temp != nil {
		payload["temperature"] = *temp
	}
	if maxTokens := chooseIntPtr(opts.MaxTokens, a.profile.MaxTokens); maxTokens != nil {
		if responsesStyle {
			payload["max_output_tokens"] = *maxTokens
		} else {
			payload["max_tokens"] = *maxTokens
		}
	}
}

func (a *Adapter) handleOpenAIChatStream(data []byte, writer *schema.StreamWriter[*schema.Message]) error {
	var chunk openAIChatStreamChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return err
	}
	for _, choice := range chunk.Choices {
		msg := &schema.Message{
			Role:    schema.Assistant,
			Content: choice.Delta.Content,
		}
		if len(choice.Delta.ToolCalls) > 0 {
			msg.ToolCalls = make([]schema.ToolCall, 0, len(choice.Delta.ToolCalls))
			for _, call := range choice.Delta.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, call.toSchemaToolCall())
			}
		}
		if msg.Content != "" || len(msg.ToolCalls) > 0 {
			writer.Send(msg, nil)
		}
	}
	return nil
}

func (a *Adapter) handleOpenAIResponsesStream(eventType string, data []byte, writer *schema.StreamWriter[*schema.Message]) error {
	if eventType == "" {
		var fallback struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &fallback); err == nil {
			eventType = fallback.Type
		}
	}

	switch eventType {
	case "response.output_text.delta":
		var event struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		if event.Delta != "" {
			writer.Send(schema.AssistantMessage(event.Delta, nil), nil)
		}
	case "response.output_item.added":
		var event struct {
			Item openAIResponsesOutputItem `json:"item"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		if event.Item.Type == "function_call" {
			writer.Send(schema.AssistantMessage("", []schema.ToolCall{event.Item.toSchemaToolCall()}), nil)
		}
	case "response.function_call_arguments.delta":
		var event struct {
			CallID string `json:"call_id"`
			Delta  string `json:"delta"`
			Name   string `json:"name"`
			Index  *int   `json:"output_index"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		call := schema.ToolCall{
			ID:   event.CallID,
			Type: "function",
			Function: schema.FunctionCall{
				Name:      event.Name,
				Arguments: event.Delta,
			},
			Index: event.Index,
		}
		writer.Send(schema.AssistantMessage("", []schema.ToolCall{call}), nil)
	case "response.completed", "response.failed":
		return nil
	}

	return nil
}

func (a *Adapter) handleAnthropicStream(eventType string, data []byte, writer *schema.StreamWriter[*schema.Message]) error {
	switch eventType {
	case "content_block_start":
		var event anthropicContentBlockStart
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		switch event.ContentBlock.Type {
		case "text":
			if event.ContentBlock.Text != "" {
				writer.Send(schema.AssistantMessage(event.ContentBlock.Text, nil), nil)
			}
		case "tool_use":
			args, _ := json.Marshal(event.ContentBlock.Input)
			index := event.Index
			call := schema.ToolCall{
				ID:   event.ContentBlock.ID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      event.ContentBlock.Name,
					Arguments: string(args),
				},
				Index: &index,
			}
			writer.Send(schema.AssistantMessage("", []schema.ToolCall{call}), nil)
		}
	case "content_block_delta":
		var event anthropicContentBlockDelta
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		switch event.Delta.Type {
		case "text_delta":
			if event.Delta.Text != "" {
				writer.Send(schema.AssistantMessage(event.Delta.Text, nil), nil)
			}
		case "input_json_delta":
			call := schema.ToolCall{
				ID:   event.Delta.ToolUseID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      event.Delta.Name,
					Arguments: event.Delta.PartialJSON,
				},
			}
			index := event.Index
			call.Index = &index
			writer.Send(schema.AssistantMessage("", []schema.ToolCall{call}), nil)
		}
	}

	return nil
}

func consumeSSE(r io.Reader, handle func(eventType string, data []byte) error) error {
	scanner := bufio.NewScanner(r)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	var eventType string
	var payload []string
	flush := func() error {
		if len(payload) == 0 {
			eventType = ""
			return nil
		}
		data := []byte(strings.Join(payload, "\n"))
		err := handle(eventType, data)
		eventType = ""
		payload = nil
		return err
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload = append(payload, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func resolveEndpoint(baseURL, suffix string) string {
	baseURL = normalizeEndpointBaseURL(baseURL)
	if strings.HasSuffix(baseURL, suffix) {
		return baseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + suffix
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		path = strings.TrimSuffix(path, "/chat/completions")
	case strings.HasSuffix(path, "/responses"):
		path = strings.TrimSuffix(path, "/responses")
	case strings.HasSuffix(path, "/messages"):
		path = strings.TrimSuffix(path, "/messages")
	}
	if suffix == "/chat/completions" || suffix == "/responses" {
		if path == "" {
			path = "/v1"
		} else if !strings.HasSuffix(path, "/v1") {
			path += "/v1"
		}
	}
	u.Path = path + suffix
	return u.String()
}

func normalizeEndpointBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return ""
	}
	if strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://") {
		return baseURL
	}
	return "https://" + baseURL
}

func chooseString(ptr *string, fallback string) string {
	if ptr != nil && strings.TrimSpace(*ptr) != "" {
		return *ptr
	}
	return fallback
}

func chooseInt(ptr *int, fallback int, zeroDefault int) int {
	if ptr != nil && *ptr > 0 {
		return *ptr
	}
	if fallback > 0 {
		return fallback
	}
	return zeroDefault
}

func chooseIntPtr(ptr *int, fallback int) *int {
	if ptr != nil && *ptr > 0 {
		return ptr
	}
	if fallback > 0 {
		v := fallback
		return &v
	}
	return nil
}

func chooseFloat(ptr *float32, fallback *float32) *float32 {
	if ptr != nil {
		return ptr
	}
	return fallback
}

func stringifyContent(content string) []map[string]any {
	return []map[string]any{{
		"type": "input_text",
		"text": content,
	}}
}

func toOpenAIChatMessages(messages []*schema.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		item := map[string]any{
			"role":    string(msg.Role),
			"content": msg.Content,
		}
		if msg.Name != "" {
			item["name"] = msg.Name
		}
		if len(msg.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				calls = append(calls, map[string]any{
					"id":   call.ID,
					"type": "function",
					"function": map[string]any{
						"name":      call.Function.Name,
						"arguments": call.Function.Arguments,
					},
				})
			}
			item["tool_calls"] = calls
		}
		if msg.Role == schema.Tool {
			item["tool_call_id"] = msg.ToolCallID
			if msg.ToolName != "" {
				item["name"] = msg.ToolName
			}
		}
		out = append(out, item)
	}
	return out
}

func toOpenAIResponsesInput(messages []*schema.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case schema.Tool:
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  msg.Content,
			})
		case schema.Assistant:
			if msg.Content != "" {
				out = append(out, map[string]any{
					"type":    "message",
					"role":    "assistant",
					"content": stringifyContent(msg.Content),
				})
			}
			for _, call := range msg.ToolCalls {
				out = append(out, map[string]any{
					"type":      "function_call",
					"call_id":   call.ID,
					"name":      call.Function.Name,
					"arguments": call.Function.Arguments,
				})
			}
		default:
			out = append(out, map[string]any{
				"type":    "message",
				"role":    string(msg.Role),
				"content": stringifyContent(msg.Content),
			})
		}
	}
	return out
}

func toAnthropicMessages(messages []*schema.Message) (string, []map[string]any, error) {
	var systemParts []string
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case schema.System:
			if strings.TrimSpace(msg.Content) != "" {
				systemParts = append(systemParts, msg.Content)
			}
		case schema.User:
			out = append(out, map[string]any{
				"role":    "user",
				"content": []map[string]any{{"type": "text", "text": msg.Content}},
			})
		case schema.Assistant:
			content := make([]map[string]any, 0, 1+len(msg.ToolCalls))
			if msg.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": msg.Content})
			}
			for _, call := range msg.ToolCalls {
				input := map[string]any{}
				if strings.TrimSpace(call.Function.Arguments) != "" {
					if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
						input["raw"] = call.Function.Arguments
					}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    call.ID,
					"name":  call.Function.Name,
					"input": input,
				})
			}
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": content,
			})
		case schema.Tool:
			out = append(out, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": msg.ToolCallID,
					"content":     msg.Content,
				}},
			})
		}
	}
	return strings.Join(systemParts, "\n\n"), out, nil
}

func toOpenAIChatTools(tools []*schema.ToolInfo) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		js, err := tool.ParamsOneOf.ToJSONSchema()
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Desc,
				"parameters":  js,
			},
		})
	}
	return out, nil
}

func toOpenAIResponsesTools(tools []*schema.ToolInfo) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		js, err := tool.ParamsOneOf.ToJSONSchema()
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Desc,
			"parameters":  js,
		})
	}
	return out, nil
}

func toAnthropicTools(tools []*schema.ToolInfo) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		js, err := tool.ParamsOneOf.ToJSONSchema()
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"name":         tool.Name,
			"description":  tool.Desc,
			"input_schema": js,
		})
	}
	return out, nil
}

type openAIChatMessage struct {
	Role      string               `json:"role"`
	Content   string               `json:"content"`
	ToolCalls []openAIChatToolCall `json:"tool_calls"`
}

func (m openAIChatMessage) toSchemaMessage() *schema.Message {
	msg := &schema.Message{
		Role:    schema.RoleType(m.Role),
		Content: m.Content,
	}
	for _, call := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, call.toSchemaToolCall())
	}
	if msg.Role == "" {
		msg.Role = schema.Assistant
	}
	return msg
}

type openAIChatToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (c openAIChatToolCall) toSchemaToolCall() schema.ToolCall {
	return schema.ToolCall{
		Index: c.Index,
		ID:    c.ID,
		Type:  chooseNonEmpty(c.Type, "function"),
		Function: schema.FunctionCall{
			Name:      c.Function.Name,
			Arguments: c.Function.Arguments,
		},
	}
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

type openAIChatStreamChunk struct {
	Choices []struct {
		Delta openAIChatMessage `json:"delta"`
	} `json:"choices"`
}

type openAIResponsesResponse struct {
	Output []openAIResponsesOutputItem `json:"output"`
}

type openAIResponsesOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Role      string `json:"role"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (r openAIResponsesResponse) toSchemaMessage() (*schema.Message, error) {
	msg := schema.AssistantMessage("", nil)
	for _, item := range r.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Text != "" {
					if msg.Content != "" {
						msg.Content += "\n"
					}
					msg.Content += part.Text
				}
			}
		case "function_call":
			msg.ToolCalls = append(msg.ToolCalls, item.toSchemaToolCall())
		}
	}
	return msg, nil
}

func (i openAIResponsesOutputItem) toSchemaToolCall() schema.ToolCall {
	return schema.ToolCall{
		ID:   i.CallID,
		Type: "function",
		Function: schema.FunctionCall{
			Name:      i.Name,
			Arguments: i.Arguments,
		},
	}
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

func (r anthropicResponse) toSchemaMessage() (*schema.Message, error) {
	msg := schema.AssistantMessage("", nil)
	for _, block := range r.Content {
		switch block.Type {
		case "text":
			msg.Content += block.Text
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		}
	}
	return msg, nil
}

type anthropicContentBlockStart struct {
	Index        int                   `json:"index"`
	ContentBlock anthropicContentBlock `json:"content_block"`
}

type anthropicContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		ToolUseID   string `json:"tool_use_id"`
		Name        string `json:"name"`
	} `json:"delta"`
}

func chooseNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func messageToPrettyJSON(msg *schema.Message) string {
	if msg == nil {
		return ""
	}
	data, _ := json.Marshal(msg)
	return string(data)
}

func intPtr(v int) *int {
	return &v
}

func stringToInt(value string) *int {
	if value == "" {
		return nil
	}
	v, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &v
}
