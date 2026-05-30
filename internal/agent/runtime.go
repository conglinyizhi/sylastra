package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/conglinyizhi/sylastra/internal/tools"
)

type EventType string

const (
	EventTextDelta EventType = "text_delta"
	EventStatus    EventType = "status"
	EventToolStart EventType = "tool_start"
	EventToolEnd   EventType = "tool_end"
	EventError     EventType = "error"
	EventDone      EventType = "done"
	EventNetworkStep EventType = "network_step"   // 新增: 网络请求状态
	EventTokenUsage  EventType = "token_usage"    // 新增: token 用量信息
)

type Event struct {
	Type       EventType
	Text       string
	Status     string
	ToolName   string
	ToolInput  string
	TokenInput   int    `json:"token_input,omitempty"`
	TokenOutput  int    `json:"token_output,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
	NetworkState string `json:"network_state,omitempty"`    // connecting/connected/error
	ToolOutput string
	Err        error
}

type Sink interface {
	Emit(Event)
}

type Session struct {
	History []*schema.Message
}

type Runtime struct {
	model      model.ToolCallingChatModel
	bridge     tools.ToolBridge
	tools      []*schema.ToolInfo
	systemText string
	toolText   string
}

func NewRuntime(chatModel model.ToolCallingChatModel, bridge tools.ToolBridge, toolInfos []*schema.ToolInfo, systemText, toolText string) *Runtime {
	return &Runtime{
		model:      chatModel,
		bridge:     bridge,
		tools:      toolInfos,
		systemText: systemText,
		toolText:   toolText,
	}
}

func (r *Runtime) RunTurn(ctx context.Context, session *Session, input string, sink Sink) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}

	session.History = append(session.History, schema.UserMessage(input))
	boundModel, err := r.model.WithTools(r.tools)
	if err != nil {
		return err
	}

	for round := 0; round < 8; round++ {
		msgs := r.buildMessages(session)
		sink.Emit(Event{Type: EventStatus, Status: fmt.Sprintf("requesting model (round %d)", round+1)})
		stream, err := boundModel.Stream(ctx, msgs)
		if err != nil {
			sink.Emit(Event{Type: EventError, Err: err})
			return err
		}
		sink.Emit(Event{Type: EventStatus, Status: fmt.Sprintf("streaming response (round %d)", round+1)})

		var chunks []*schema.Message
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				stream.Close()
				sink.Emit(Event{Type: EventError, Err: err})
				return err
			}
			if chunk == nil {
				continue
			}
			chunks = append(chunks, chunk)
			if strings.TrimSpace(chunk.Content) != "" {
				sink.Emit(Event{Type: EventTextDelta, Text: chunk.Content})
			}
		}
		stream.Close()

		assistantMsg, err := schema.ConcatMessages(chunks)
		if err != nil {
			sink.Emit(Event{Type: EventError, Err: err})
			return err
		}
		if assistantMsg == nil {
			return fmt.Errorf("model returned no message")
		}

		session.History = append(session.History, assistantMsg)
		if len(assistantMsg.ToolCalls) == 0 {
			sink.Emit(Event{Type: EventStatus, Status: "response complete"})
			sink.Emit(Event{Type: EventDone, Text: assistantMsg.Content})
			return nil
		}

		for _, call := range assistantMsg.ToolCalls {
			sink.Emit(Event{
				Type:      EventToolStart,
				ToolName:  call.Function.Name,
				ToolInput: call.Function.Arguments,
			})

			args := map[string]any{}
			if strings.TrimSpace(call.Function.Arguments) != "" {
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					args["raw"] = call.Function.Arguments
				}
			}

			output, toolErr := r.bridge.Call(ctx, call.Function.Name, args)
			if toolErr != nil {
				output = structuredToolError(toolErr, output)
			}

			session.History = append(session.History, schema.ToolMessage(output, call.ID, schema.WithToolName(call.Function.Name)))
			sink.Emit(Event{
				Type:       EventToolEnd,
				ToolName:   call.Function.Name,
				ToolOutput: output,
				Err:        toolErr,
			})
		}
	}

	return fmt.Errorf("tool loop exceeded limit")
}

func (r *Runtime) buildMessages(session *Session) []*schema.Message {
	msgs := make([]*schema.Message, 0, len(session.History)+2)
	if strings.TrimSpace(r.systemText) != "" {
		msgs = append(msgs, schema.SystemMessage(r.systemText))
	}
	if strings.TrimSpace(r.toolText) != "" {
		msgs = append(msgs, schema.SystemMessage(r.toolText))
	}
	msgs = append(msgs, session.History...)
	return msgs
}

func structuredToolError(err error, output string) string {
	payload := map[string]any{
		"ok":    false,
		"error": err.Error(),
	}
	if strings.TrimSpace(output) != "" {
		payload["output"] = output
	}
	data, _ := json.Marshal(payload)
	return string(data)
}
