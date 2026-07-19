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
