package llm

import (
	"encoding/json"
	"testing"
)

func TestBlockRoundTrip(t *testing.T) {
	blocks := []Block{
		TextBlock{Text: "hello"},
		ThinkingBlock{Text: "reasoning", Signature: "sig-abc"},
		ToolUseBlock{ID: "t1", Name: "read", Input: json.RawMessage(`{"path":"x"}`)},
		ToolResultBlock{ToolUseID: "t1", Content: []Block{TextBlock{Text: "file body"}}, IsError: false},
		ToolResultBlock{ToolUseID: "t1", Content: []Block{TextBlock{Text: "file body"}}, IsError: true},
	}
	for _, b := range blocks {
		data, err := MarshalBlock(b)
		if err != nil {
			t.Fatalf("marshal %T: %v", b, err)
		}
		got, err := UnmarshalBlock(data)
		if err != nil {
			t.Fatalf("unmarshal %T: %v", b, err)
		}
		if got.Type() != b.Type() {
			t.Fatalf("type mismatch: got %q want %q", got.Type(), b.Type())
		}

		// Assert field values survive round trip
		switch v := got.(type) {
		case TextBlock:
			want := b.(TextBlock)
			if v.Text != want.Text {
				t.Fatalf("TextBlock.Text mismatch: got %q want %q", v.Text, want.Text)
			}
		case ThinkingBlock:
			want := b.(ThinkingBlock)
			if v.Text != want.Text {
				t.Fatalf("ThinkingBlock.Text mismatch: got %q want %q", v.Text, want.Text)
			}
			if v.Signature != want.Signature {
				t.Fatalf("ThinkingBlock.Signature mismatch: got %q want %q", v.Signature, want.Signature)
			}
		case ToolUseBlock:
			want := b.(ToolUseBlock)
			if v.ID != want.ID {
				t.Fatalf("ToolUseBlock.ID mismatch: got %q want %q", v.ID, want.ID)
			}
			if v.Name != want.Name {
				t.Fatalf("ToolUseBlock.Name mismatch: got %q want %q", v.Name, want.Name)
			}
			if string(v.Input) != string(want.Input) {
				t.Fatalf("ToolUseBlock.Input mismatch: got %s want %s", string(v.Input), string(want.Input))
			}
		case ToolResultBlock:
			want := b.(ToolResultBlock)
			if v.ToolUseID != want.ToolUseID {
				t.Fatalf("ToolResultBlock.ToolUseID mismatch: got %q want %q", v.ToolUseID, want.ToolUseID)
			}
			if v.IsError != want.IsError {
				t.Fatalf("ToolResultBlock.IsError mismatch: got %v want %v", v.IsError, want.IsError)
			}
			if len(v.Content) != 1 {
				t.Fatalf("ToolResultBlock.Content length mismatch: got %d want 1", len(v.Content))
			}
			contentText := v.Content[0].(TextBlock).Text
			wantText := want.Content[0].(TextBlock).Text
			if contentText != wantText {
				t.Fatalf("ToolResultBlock.Content[0].Text mismatch: got %q want %q", contentText, wantText)
			}
		}
	}
}

func TestUnmarshalBlockUnknownType(t *testing.T) {
	_, err := UnmarshalBlock([]byte(`{"type":"bogus"}`))
	if err == nil {
		t.Fatalf("expected error for unknown block type, got nil")
	}
}

func TestThinkingSignaturePreserved(t *testing.T) {
	data, err := MarshalBlock(ThinkingBlock{Text: "t", Signature: "keep-me"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalBlock(data)
	if err != nil {
		t.Fatal(err)
	}
	tb, ok := got.(ThinkingBlock)
	if !ok {
		t.Fatalf("got %T, want ThinkingBlock", got)
	}
	if tb.Signature != "keep-me" {
		t.Fatalf("signature lost: got %q", tb.Signature)
	}
}

func TestToolResultNestedContent(t *testing.T) {
	in := ToolResultBlock{ToolUseID: "t9", Content: []Block{TextBlock{Text: "nested"}}}
	data, err := MarshalBlock(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalBlock(data)
	if err != nil {
		t.Fatal(err)
	}
	tr := got.(ToolResultBlock)
	if len(tr.Content) != 1 || tr.Content[0].(TextBlock).Text != "nested" {
		t.Fatalf("nested content not preserved: %+v", tr.Content)
	}
}
