package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jelmersnoeck/agentloop/llm"
)

// staticTool is a minimal Tool used to check interface satisfaction.
type staticTool struct{ readOnly bool }

func (staticTool) Name() string            { return "static" }
func (staticTool) Description() string     { return "a static tool" }
func (staticTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (staticTool) Execute(ctx context.Context, input json.RawMessage, tctx Context) (Result, error) {
	return TextResult("ok"), nil
}
func (s staticTool) ReadOnly() bool { return s.readOnly }

func TestToolInterfaceAndReadOnly(t *testing.T) {
	var tool Tool = staticTool{readOnly: true}
	if !IsReadOnly(tool) {
		t.Fatal("expected staticTool{readOnly:true} to be read-only")
	}
	if IsReadOnly(staticTool{readOnly: false}) {
		t.Fatal("expected staticTool{readOnly:false} to be mutating")
	}
}

func TestMissingReadOnlyIsMutating(t *testing.T) {
	// Embed but shadow so the type does NOT satisfy ReadOnly.
	var tool Tool = plainTool{}
	if IsReadOnly(tool) {
		t.Fatal("a tool without ReadOnly() must be treated as mutating")
	}
}

type plainTool struct{}

func (plainTool) Name() string            { return "plain" }
func (plainTool) Description() string     { return "" }
func (plainTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (plainTool) Execute(ctx context.Context, input json.RawMessage, tctx Context) (Result, error) {
	return Result{}, nil
}

func TestResultConstructors(t *testing.T) {
	r := TextResult("hi")
	if r.IsError || len(r.Content) != 1 || r.Content[0].(llm.TextBlock).Text != "hi" {
		t.Fatalf("TextResult wrong: %+v", r)
	}
	e := ErrorResult("boom")
	if !e.IsError || e.Content[0].(llm.TextBlock).Text != "boom" {
		t.Fatalf("ErrorResult wrong: %+v", e)
	}
}
