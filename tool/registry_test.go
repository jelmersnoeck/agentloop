package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jelmersnoeck/agentloop/llm"
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

func TestRegistrySchemasSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(namedTool("write"))
	r.Register(namedTool("read"))
	schemas := r.Schemas()
	if len(schemas) != 2 || schemas[0].Name != "read" || schemas[1].Name != "write" {
		t.Fatalf("Schemas() = %+v, want sorted [read write]", schemas)
	}
	if schemas[0].Description != "desc of read" {
		t.Fatalf("schema description = %q", schemas[0].Description)
	}
}

func TestRegistryExecute(t *testing.T) {
	r := NewRegistry()
	r.Register(namedTool("read"))
	res, err := r.Execute(context.Background(), "read", nil, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Content[0].(llm.TextBlock).Text; got != "ran read" {
		t.Fatalf("Execute result = %q, want %q", got, "ran read")
	}
	if _, err := r.Execute(context.Background(), "nope", nil, Context{}); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestRegistryFiltered(t *testing.T) {
	r := NewRegistry()
	r.Register(namedTool("read"))
	r.Register(namedTool("write"))
	r.Register(namedTool("bash"))

	// allow-list only
	allow := r.Filtered([]string{"read", "write"}, nil)
	if len(allow.All()) != 2 {
		t.Fatalf("allow-list size = %d, want 2", len(allow.All()))
	}
	if _, ok := allow.Get("bash"); ok {
		t.Fatal("bash should be excluded by allow-list")
	}

	// deny wins over allow
	both := r.Filtered([]string{"read", "write"}, []string{"write"})
	if _, ok := both.Get("write"); ok {
		t.Fatal("write should be denied")
	}
	if _, ok := both.Get("read"); !ok {
		t.Fatal("read should remain")
	}

	// empty allow = all (minus deny)
	denyOnly := r.Filtered(nil, []string{"bash"})
	if len(denyOnly.All()) != 2 {
		t.Fatalf("deny-only size = %d, want 2", len(denyOnly.All()))
	}
}
