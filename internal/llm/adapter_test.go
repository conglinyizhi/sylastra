package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/conglinyizhi/sylastra/internal/config"
)

func TestOpenAIChatGenerate(t *testing.T) {
	profile := config.LLMProfile{
		Name:     "demo",
		APIStyle: config.APIStyleOpenAIChat,
		BaseURL:  "https://example.test/v1",
		Model:    "test-model",
		APIKey:   "secret",
	}
	modelImpl, err := Build(context.Background(), profile)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	adapter := modelImpl.(*Adapter)
	adapter.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return jsonResponse(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`), nil
	})}
	msg, err := modelImpl.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if msg.Content != "ok" {
		t.Fatalf("content = %q", msg.Content)
	}
}

func TestOpenAIChatGenerateThirdPartyBaseURL(t *testing.T) {
	profile := config.LLMProfile{
		Name:     "demo",
		APIStyle: config.APIStyleOpenAIChat,
		BaseURL:  "https://token.memoh.net",
		Model:    "test-model",
		APIKey:   "secret",
	}
	modelImpl, err := Build(context.Background(), profile)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	adapter := modelImpl.(*Adapter)
	adapter.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://token.memoh.net/v1/chat/completions" {
			t.Fatalf("unexpected url %s", r.URL.String())
		}
		return jsonResponse(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`), nil
	})}
	msg, err := modelImpl.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if msg.Content != "ok" {
		t.Fatalf("content = %q", msg.Content)
	}
}

func TestOpenAIResponsesStream(t *testing.T) {
	profile := config.LLMProfile{
		Name:     "demo",
		APIStyle: config.APIStyleOpenAIResponses,
		BaseURL:  "https://example.test",
		Model:    "test-model",
		APIKey:   "secret",
	}
	modelImpl, err := Build(context.Background(), profile)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	adapter := modelImpl.(*Adapter)
	adapter.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return sseResponse(strings.Join([]string{
			"event: response.output_text.delta",
			`data: {"delta":"hel"}`,
			"",
			"event: response.output_text.delta",
			`data: {"delta":"lo"}`,
			"",
			"event: response.completed",
			"data: {}",
			"",
		}, "\n")), nil
	})}
	stream, err := modelImpl.Stream(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	full, err := schema.ConcatMessageStream(stream)
	if err != nil {
		t.Fatalf("ConcatMessageStream() error = %v", err)
	}
	if full.Content != "hello" {
		t.Fatalf("stream content = %q", full.Content)
	}
}

func TestAnthropicNonStreamToolCall(t *testing.T) {
	profile := config.LLMProfile{
		Name:     "demo",
		APIStyle: config.APIStyleAnthropicMessages,
		BaseURL:  "https://example.test",
		Model:    "test-model",
		APIKey:   "secret",
	}
	modelImpl, err := Build(context.Background(), profile)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	adapter := modelImpl.(*Adapter)
	adapter.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"system":"sys"`) {
			t.Fatalf("request body missing system prompt: %s", string(body))
		}
		return jsonResponse(`{"content":[{"type":"text","text":"done"},{"type":"tool_use","id":"call_1","name":"be-read","input":{"file":"main.go"}}]}`), nil
	})}
	msg, err := modelImpl.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("hi"),
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if msg.Content != "done" || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "be-read" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func sseResponse(body string) *http.Response {
	resp := jsonResponse(body)
	resp.Header.Set("Content-Type", "text/event-stream")
	return resp
}
