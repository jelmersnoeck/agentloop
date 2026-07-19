package llm

import (
	"encoding/json"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	m := Message{
		Role: RoleAssistant,
		Content: []Block{
			ThinkingBlock{Text: "hmm", Signature: "s1"},
			TextBlock{Text: "the answer"},
		},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Role != RoleAssistant {
		t.Fatalf("role: got %q", got.Role)
	}
	if len(got.Content) != 2 {
		t.Fatalf("content len: got %d", len(got.Content))
	}
	if got.Content[0].(ThinkingBlock).Signature != "s1" {
		t.Fatalf("thinking signature lost after message round-trip")
	}
}

func TestContextSerializable(t *testing.T) {
	c := Context{
		System:   []Block{TextBlock{Text: "you are helpful"}},
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}},
		Tools:    []ToolSchema{{Name: "read", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got Context
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.System) != 1 || got.System[0].(TextBlock).Text != "you are helpful" {
		t.Fatalf("system not preserved: %+v", got.System)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "read" {
		t.Fatalf("tools not preserved: %+v", got.Tools)
	}
}
