package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func namedTool(name string) Tool { return fixedTool{name: name} }

type fixedTool struct{ name string }

func (f fixedTool) Name() string           { return f.name }
func (f fixedTool) Description() string     { return "desc of " + f.name }
func (f fixedTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f fixedTool) Execute(ctx context.Context, input json.RawMessage, tctx Context) (Result, error) {
	return TextResult("ran " + f.name), nil
}

func TestRegistryRegisterGet(t *testing.T) {
	r := NewRegistry()
	r.Register(namedTool("read"))
	got, ok := r.Get("read")
	if !ok || got.Name() != "read" {
		t.Fatalf("Get(read) = %v, %v", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get(missing) should be false")
	}
}

func TestRegistryAllSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(namedTool("write"))
	r.Register(namedTool("bash"))
	r.Register(namedTool("read"))
	all := r.All()
	got := []string{all[0].Name(), all[1].Name(), all[2].Name()}
	want := []string{"bash", "read", "write"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("All() order = %v, want %v", got, want)
		}
	}
}
