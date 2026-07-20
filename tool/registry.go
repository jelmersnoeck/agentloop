package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/jelmersnoeck/agentloop/llm"
)

// Registry is a concurrency-safe collection of tools keyed by name.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds or replaces a tool, keyed by its Name.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	r.tools[t.Name()] = t
	r.mu.Unlock()
}

// Get returns the tool registered under name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns every registered tool, sorted by name for deterministic ordering.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Schemas returns the provider-neutral schema for every tool, sorted by name.
// Deterministic ordering keeps prompt-cache prefixes stable.
func (r *Registry) Schemas() []llm.ToolSchema {
	tools := r.All()
	out := make([]llm.ToolSchema, 0, len(tools))
	for _, t := range tools {
		out = append(out, llm.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Schema(),
		})
	}
	return out
}

// Execute runs the named tool, returning an error if the tool is unknown.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage, tctx Context) (Result, error) {
	t, ok := r.Get(name)
	if !ok {
		return Result{}, fmt.Errorf("tool: unknown tool %q", name)
	}
	return t.Execute(ctx, input, tctx)
}

// Filtered returns a new Registry containing only the allowed tools, minus any
// denied. An empty allow list means "all tools"; deny always wins.
func (r *Registry) Filtered(allow, deny []string) *Registry {
	allowSet := toSet(allow)
	denySet := toSet(deny)
	out := NewRegistry()
	for _, t := range r.All() {
		if _, denied := denySet[t.Name()]; denied {
			continue
		}
		if len(allowSet) > 0 {
			if _, ok := allowSet[t.Name()]; !ok {
				continue
			}
		}
		out.Register(t)
	}
	return out
}

func toSet(names []string) map[string]struct{} {
	s := make(map[string]struct{}, len(names))
	for _, n := range names {
		s[n] = struct{}{}
	}
	return s
}
