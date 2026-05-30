package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
)

// SerializeMessage converts a schema.Message to a portable JSON structure.
func SerializeMessage(msg *schema.Message) ([]byte, error) {
	var toolCalls []toolCallJSON
	for _, tc := range msg.ToolCalls {
		toolCalls = append(toolCalls, toolCallJSON{
			ID:        tc.ID,
			Type:      tc.Type,
			Index:     tc.Index,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return json.Marshal(messageJSON{
		Role:       string(msg.Role),
		Content:    msg.Content,
		ToolCalls:  toolCalls,
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
	})
}

// DeserializeMessage restores a schema.Message from JSON.
func DeserializeMessage(data []byte) (*schema.Message, error) {
	var m messageJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	msg := &schema.Message{
		Role:       schema.RoleType(m.Role),
		Content:    m.Content,
		ToolCallID: m.ToolCallID,
		ToolName:   m.ToolName,
	}
	for _, tc := range m.ToolCalls {
		index := tc.Index
		msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
			ID:    tc.ID,
			Type:  tc.Type,
			Index: index,
			Function: schema.FunctionCall{
				Name:      tc.Name,
				Arguments: tc.Arguments,
			},
		})
	}
	return msg, nil
}

// ── JSON structures ────────────────────────────────────────────────

type messageJSON struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
}

type toolCallJSON struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Index     *int    `json:"index,omitempty"`
	Name      string  `json:"name"`
	Arguments string  `json:"arguments"`
}

// ── Session metadata ───────────────────────────────────────────────

type SessionMeta struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	ModelName    string    `json:"model_name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	TokenCount   int       `json:"token_count"`
	MessageCount int       `json:"message_count"`
}

type SessionData struct {
	Meta     SessionMeta      `json:"meta"`
	Messages []*schema.Message `json:"messages"`
}

// SerializeSession serializes a session with metadata.
func SerializeSession(meta SessionMeta, history []*schema.Message) ([]byte, error) {
	data := SessionData{
		Meta:     meta,
		Messages: history,
	}
	return json.Marshal(data)
}

// DeserializeSession restores session data from JSON.
func DeserializeSession(data []byte) (*SessionMeta, []*schema.Message, error) {
	var sd SessionData
	if err := json.Unmarshal(data, &sd); err != nil {
		return nil, nil, fmt.Errorf("deserialize session: %w", err)
	}
	return &sd.Meta, sd.Messages, nil
}
