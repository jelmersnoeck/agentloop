// Package tool defines the agent tool interface, a concurrency-safe registry,
// and a reflection helper (FromFunc) for authoring tools from typed functions.
package tool

import (
	"context"
	"encoding/json"

	"github.com/jelmersnoeck/agentloop/llm"
)

// Result is what a Tool returns to the model.
type Result struct {
	Content []llm.Block
	IsError bool
}

// Context threads shared services into every tool invocation.
type Context struct {
	CWD       string
	SessionID string
	Emit      func(llm.Event) // stream tool progress onto the event bus (may be nil)
}

// Tool is a self-describing, self-executing capability.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON Schema for the input object
	Execute(ctx context.Context, input json.RawMessage, tctx Context) (Result, error)
}

// ReadOnly is an optional capability: a Tool that declares it does not mutate
// state, so the executor may run it concurrently with other read-only tools.
type ReadOnly interface {
	ReadOnly() bool
}

// IsReadOnly reports whether t opts into read-only execution. A tool that does
// not implement ReadOnly (or returns false) is treated as mutating — the safe
// default, since a wrongly-parallelized mutating tool could corrupt state.
func IsReadOnly(t Tool) bool {
	ro, ok := t.(ReadOnly)
	return ok && ro.ReadOnly()
}

// TextResult is a convenience constructor for a plain-text tool result.
func TextResult(text string) Result {
	return Result{Content: []llm.Block{llm.TextBlock{Text: text}}}
}

// ErrorResult is a convenience constructor for an error tool result the model
// can read and react to.
func ErrorResult(text string) Result {
	return Result{Content: []llm.Block{llm.TextBlock{Text: text}}, IsError: true}
}
