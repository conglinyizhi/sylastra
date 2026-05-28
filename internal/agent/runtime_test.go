package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/conglinyizhi/sylastra/internal/tools"
)

type fakeModel struct {
	responses []*schema.Message
	index     int
}

func (f *fakeModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func (f *fakeModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, nil
}

func (f *fakeModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg := f.responses[f.index]
	f.index++
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

type fakeBridge struct{}

func (fakeBridge) List(context.Context) ([]*schema.ToolInfo, error) { return nil, nil }
func (fakeBridge) Close() error                                     { return nil }
func (fakeBridge) Call(_ context.Context, name string, args map[string]any) (string, error) {
	return `{"ok":true,"name":"` + name + `"}`, nil
}

type collectingSink struct {
	events []Event
}

func (s *collectingSink) Emit(event Event) {
	s.events = append(s.events, event)
}

func TestRuntimeToolLoop(t *testing.T) {
	rt := NewRuntime(&fakeModel{
		responses: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: schema.FunctionCall{
					Name:      "be-read",
					Arguments: `{"file":"main.go"}`,
				},
			}}),
			schema.AssistantMessage("final answer", nil),
		},
	}, fakeBridge{}, []*schema.ToolInfo{{Name: "be-read"}}, "sys", "tool")

	session := &Session{}
	sink := &collectingSink{}
	if err := rt.RunTurn(context.Background(), session, "hello", sink); err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if len(session.History) != 4 {
		t.Fatalf("history length = %d", len(session.History))
	}
	last := session.History[len(session.History)-1]
	if last.Content != "final answer" {
		t.Fatalf("last content = %q", last.Content)
	}
}

var _ tools.ToolBridge = fakeBridge{}
