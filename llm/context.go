package llm

import "encoding/json"

// Role identifies the author of a Message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one conversational turn: a role plus ordered content blocks.
type Message struct {
	Role    Role
	Content []Block
}

type messageWire struct {
	Role    Role              `json:"role"`
	Content []json.RawMessage `json:"content"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	w := messageWire{Role: m.Role}
	for _, b := range m.Content {
		raw, err := MarshalBlock(b)
		if err != nil {
			return nil, err
		}
		w.Content = append(w.Content, raw)
	}
	return json.Marshal(w)
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var w messageWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	m.Role = w.Role
	m.Content = nil
	for _, raw := range w.Content {
		b, err := UnmarshalBlock(raw)
		if err != nil {
			return err
		}
		m.Content = append(m.Content, b)
	}
	return nil
}

// ToolSchema is the provider-neutral description of a callable tool.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// blockSlice marshals a []Block using the block type discriminator.
type blockSlice []Block

func (bs blockSlice) MarshalJSON() ([]byte, error) {
	raws := make([]json.RawMessage, 0, len(bs))
	for _, b := range bs {
		raw, err := MarshalBlock(b)
		if err != nil {
			return nil, err
		}
		raws = append(raws, raw)
	}
	return json.Marshal(raws)
}

func (bs *blockSlice) UnmarshalJSON(data []byte) error {
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return err
	}
	out := make(blockSlice, 0, len(raws))
	for _, raw := range raws {
		b, err := UnmarshalBlock(raw)
		if err != nil {
			return err
		}
		out = append(out, b)
	}
	*bs = out
	return nil
}

// Context is the serializable source of truth for a conversation. Copying or
// serializing it is how state is persisted and handed to sub-agents.
type Context struct {
	System   blockSlice   `json:"system"`
	Messages []Message    `json:"messages"`
	Tools    []ToolSchema `json:"tools"`
}
