package llm

import (
	"encoding/json"
	"fmt"
)

// BlockType is the discriminator for a content Block in JSON.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockThinking   BlockType = "thinking"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
)

// Block is one unit of message content. Concrete types are TextBlock,
// ThinkingBlock, ToolUseBlock, and ToolResultBlock.
type Block interface {
	Type() BlockType
}

type TextBlock struct {
	Text string
}

func (TextBlock) Type() BlockType { return BlockText }

// ThinkingBlock carries model reasoning. Signature MUST be preserved and
// replayed verbatim when the block is sent back to the provider.
type ThinkingBlock struct {
	Text      string
	Signature string
}

func (ThinkingBlock) Type() BlockType { return BlockThinking }

type ToolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolUseBlock) Type() BlockType { return BlockToolUse }

type ToolResultBlock struct {
	ToolUseID string
	Content   []Block
	IsError   bool
}

func (ToolResultBlock) Type() BlockType { return BlockToolResult }

// wire mirrors every block field for JSON. Content is encoded via MarshalBlock.
type wire struct {
	Type      BlockType         `json:"type"`
	Text      string            `json:"text,omitempty"`
	Signature string            `json:"signature,omitempty"`
	ID        string            `json:"id,omitempty"`
	Name      string            `json:"name,omitempty"`
	Input     json.RawMessage   `json:"input,omitempty"`
	ToolUseID string            `json:"tool_use_id,omitempty"`
	Content   []json.RawMessage `json:"content,omitempty"`
	IsError   bool              `json:"is_error,omitempty"`
}

// MarshalBlock encodes a Block to JSON with its type discriminator.
func MarshalBlock(b Block) ([]byte, error) {
	w := wire{Type: b.Type()}
	switch v := b.(type) {
	case TextBlock:
		w.Text = v.Text
	case ThinkingBlock:
		w.Text = v.Text
		w.Signature = v.Signature
	case ToolUseBlock:
		w.ID = v.ID
		w.Name = v.Name
		w.Input = v.Input
	case ToolResultBlock:
		w.ToolUseID = v.ToolUseID
		w.IsError = v.IsError
		for _, c := range v.Content {
			raw, err := MarshalBlock(c)
			if err != nil {
				return nil, err
			}
			w.Content = append(w.Content, raw)
		}
	default:
		return nil, fmt.Errorf("llm: unknown block type %T", b)
	}
	return json.Marshal(w)
}

// UnmarshalBlock decodes a Block from JSON using its type discriminator.
func UnmarshalBlock(data []byte) (Block, error) {
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	switch w.Type {
	case BlockText:
		return TextBlock{Text: w.Text}, nil
	case BlockThinking:
		return ThinkingBlock{Text: w.Text, Signature: w.Signature}, nil
	case BlockToolUse:
		return ToolUseBlock{ID: w.ID, Name: w.Name, Input: w.Input}, nil
	case BlockToolResult:
		out := ToolResultBlock{ToolUseID: w.ToolUseID, IsError: w.IsError}
		for _, raw := range w.Content {
			c, err := UnmarshalBlock(raw)
			if err != nil {
				return nil, err
			}
			out.Content = append(out.Content, c)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("llm: unknown block type %q", w.Type)
	}
}
