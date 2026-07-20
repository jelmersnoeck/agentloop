# agentloop Tool Framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `tool` package — a `Tool` interface, a concurrency-safe `Registry`, a generics + reflection `FromFunc` helper that generates JSON Schema from a typed args struct, and read-only concurrency support — plus a `tool/truncate` package with direction-aware output caps and temp-file spillover. All unit-testable with no network and no loop changes.

**Architecture:** `tool` depends only on `llm` (for `Result` blocks, `Context.Emit`, and `Registry.Schemas`) and the standard library. `FromFunc[T]` reflects the args struct `T` to build the input schema and to unmarshal model input before dispatching to a typed handler. `tool/truncate` is a standalone string/IO utility (no `llm` dependency) that tools will call at their own boundary in a later plan.

**Tech Stack:** Go 1.26, standard library only (`reflect`, `encoding/json`, `sync`, `sort`, `strings`, `os`, `context`, `testing`). No third-party dependencies.

## Global Constraints

- Module path: `github.com/jelmersnoeck/agentloop`; Go version floor `go 1.26`.
- `tool` may import only `github.com/jelmersnoeck/agentloop/llm` and the standard library. It MUST NOT import the root `agentloop` package or any provider package.
- `tool/truncate` imports only the standard library (no `llm`, no `tool`).
- A tool that does not implement the `ReadOnly` capability is treated as **mutating** (the safe default).
- `Registry.All()` and `Registry.Schemas()` MUST return results **sorted by tool name** (deterministic ordering keeps prompt-cache prefixes stable).
- Truncation defaults: **2000 lines**, **50000 bytes**, **500 chars per line**.
- Test-driven: write the failing test first, watch it fail, then implement.
- Commit after every task with `git -c commit.gpgsign=false commit ...` (this environment has no GPG secret key; a plain `git commit` fails signing).

---

## File Structure

- `tool/tool.go` — `Tool` interface, `Context`, `Result`, `ReadOnly`, `IsReadOnly`, result constructors.
- `tool/tool_test.go` — interface satisfaction + `IsReadOnly` + constructor tests.
- `tool/registry.go` — `Registry` (`Register`, `Get`, `All`, `Schemas`, `Execute`, `Filtered`).
- `tool/registry_test.go` — registry behavior tests.
- `tool/fromfunc.go` — `FromFunc[T]`, `funcTool[T]`, and the reflection schema generator.
- `tool/fromfunc_test.go` — schema-generation and dispatch tests.
- `tool/truncate/truncate.go` — `Head`, `Tail`, `Line`, `Spill`, `Apply`.
- `tool/truncate/truncate_test.go` — truncation + spill tests.

---

### Task 1: Tool interface, Context, Result

**Files:**
- Create: `tool/tool.go`
- Test: `tool/tool_test.go`

**Interfaces:**
- Consumes: `llm.Block`, `llm.TextBlock`, `llm.Event` from the `llm` package.
- Produces:
  - `type Result struct { Content []llm.Block; IsError bool }`.
  - `type Context struct { CWD string; SessionID string; Emit func(llm.Event) }`.
  - `type Tool interface { Name() string; Description() string; Schema() json.RawMessage; Execute(ctx context.Context, input json.RawMessage, tctx Context) (Result, error) }`.
  - `type ReadOnly interface { ReadOnly() bool }`.
  - `func IsReadOnly(t Tool) bool`.
  - `func TextResult(text string) Result`.
  - `func ErrorResult(text string) Result`.

- [ ] **Step 1: Write the failing test**

Create `tool/tool_test.go`:
```go
package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jelmersnoeck/agentloop/llm"
)

// staticTool is a minimal Tool used to check interface satisfaction.
type staticTool struct{ readOnly bool }

func (staticTool) Name() string           { return "static" }
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

// mutatingTool does not implement ReadOnly at all.
type mutatingTool struct{ staticTool }

func TestMissingReadOnlyIsMutating(t *testing.T) {
	// Embed but shadow so the type does NOT satisfy ReadOnly.
	var tool Tool = plainTool{}
	if IsReadOnly(tool) {
		t.Fatal("a tool without ReadOnly() must be treated as mutating")
	}
}

type plainTool struct{}

func (plainTool) Name() string           { return "plain" }
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tool/ -run 'TestTool|TestMissing|TestResult' -v`
Expected: FAIL — `undefined: Tool`, `Context`, `Result`, `IsReadOnly`, `TextResult`, `ErrorResult`.

- [ ] **Step 3: Write the implementation**

Create `tool/tool.go`:
```go
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
```

- [ ] **Step 4: Remove the unused mutatingTool helper from the test**

The test file above declares `mutatingTool` but uses `plainTool` instead. Delete the `mutatingTool` type declaration from `tool/tool_test.go` (it is unused and will fail to compile as dead code is allowed, but keep the file clean):
```go
// DELETE these lines from tool/tool_test.go:
// mutatingTool does not implement ReadOnly at all.
// type mutatingTool struct{ staticTool }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./tool/ -run 'TestTool|TestMissing|TestResult' -v`
Expected: PASS (all three tests).

- [ ] **Step 6: Commit**

```bash
git add tool/tool.go tool/tool_test.go
git -c commit.gpgsign=false commit -m "feat(tool): Tool interface, Context, Result, read-only capability"
```

---

### Task 2: Registry — Register, Get, All

**Files:**
- Create: `tool/registry.go`
- Test: `tool/registry_test.go`

**Interfaces:**
- Consumes: `Tool` from Task 1.
- Produces:
  - `type Registry struct { ... }` (concurrency-safe, `map[string]Tool` under a `sync.RWMutex`).
  - `func NewRegistry() *Registry`.
  - `func (r *Registry) Register(t Tool)`.
  - `func (r *Registry) Get(name string) (Tool, bool)`.
  - `func (r *Registry) All() []Tool` (sorted by name).

- [ ] **Step 1: Write the failing test**

Create `tool/registry_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tool/ -run TestRegistry -v`
Expected: FAIL — `undefined: NewRegistry`.

- [ ] **Step 3: Write the implementation**

Create `tool/registry.go` (Task 2 needs only `sort` and `sync`; Task 3 adds the
other imports when it introduces `Schemas`/`Execute`/`Filtered`):
```go
package tool

import (
	"sort"
	"sync"
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tool/ -run TestRegistry -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add tool/registry.go tool/registry_test.go
git -c commit.gpgsign=false commit -m "feat(tool): Registry with Register, Get, and sorted All"
```

---

### Task 3: Registry — Schemas, Execute, Filtered

**Files:**
- Modify: `tool/registry.go`
- Test: `tool/registry_test.go` (append)

**Interfaces:**
- Consumes: `Registry`, `Tool` from Task 2; `llm.ToolSchema`.
- Produces:
  - `func (r *Registry) Schemas() []llm.ToolSchema` (sorted by name).
  - `func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage, tctx Context) (Result, error)` (errors on unknown tool).
  - `func (r *Registry) Filtered(allow, deny []string) *Registry` (empty allow = all; deny wins).

- [ ] **Step 1: Write the failing test (append to `tool/registry_test.go`)**

Add to `tool/registry_test.go`:
```go
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
	if res.Content[0].(interface{ }) == nil {
		t.Fatal("expected content")
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tool/ -run 'TestRegistrySchemas|TestRegistryExecute|TestRegistryFiltered' -v`
Expected: FAIL — `r.Schemas undefined`, `r.Execute undefined`, `r.Filtered undefined`.

- [ ] **Step 3: Write the implementation (append to `tool/registry.go` and restore imports)**

Set `tool/registry.go`'s import block to:
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/jelmersnoeck/agentloop/llm"
)
```

Append these methods to `tool/registry.go`:
```go
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
```

- [ ] **Step 4: Simplify the weak assertion in TestRegistryExecute**

The `res.Content[0].(interface{ }) == nil` check is not meaningful. Replace that assertion block in `tool/registry_test.go` with a real value check:
```go
	res, err := r.Execute(context.Background(), "read", nil, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Content[0].(llm.TextBlock).Text; got != "ran read" {
		t.Fatalf("Execute result = %q, want %q", got, "ran read")
	}
```
Add `"github.com/jelmersnoeck/agentloop/llm"` to `tool/registry_test.go`'s imports.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./tool/ -v`
Expected: PASS (all tool-package tests from Tasks 1–3).

- [ ] **Step 6: Commit**

```bash
git add tool/registry.go tool/registry_test.go
git -c commit.gpgsign=false commit -m "feat(tool): Registry Schemas, Execute, and Filtered (sub-agent scoping)"
```

---

### Task 4: FromFunc — JSON Schema generation from a struct

**Files:**
- Create: `tool/fromfunc.go`
- Test: `tool/fromfunc_test.go`

**Interfaces:**
- Consumes: standard library `reflect`, `encoding/json`.
- Produces (unexported, exercised via a test hook and Task 5):
  - `func schemaFor(t reflect.Type) json.RawMessage` — builds an object schema from a struct type.
  - Supports field types: string→`string`, int kinds→`integer`, float kinds→`number`, bool→`boolean`, slice/array→`array` (with `items.type`), struct/map→`object`.
  - Struct tags: `json:"name"` / `json:"name,omitempty"` / `json:"-"`; `jsonschema:"required"` and `jsonschema:"description=..."` (description must be the LAST directive and may contain commas).

- [ ] **Step 1: Write the failing test**

Create `tool/fromfunc_test.go`:
```go
package tool

import (
	"encoding/json"
	"reflect"
	"testing"
)

type schemaArgs struct {
	Path    string   `json:"path" jsonschema:"required,description=File to read"`
	Offset  int      `json:"offset" jsonschema:"description=Start line, 1-indexed"`
	Verbose bool     `json:"verbose"`
	Tags    []string `json:"tags"`
	Ignored string   `json:"-"`
}

func TestSchemaForBuildsObject(t *testing.T) {
	raw := schemaFor(reflect.TypeOf(schemaArgs{}))

	var got struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("schema is not valid JSON: %v\n%s", err, raw)
	}

	if got.Type != "object" {
		t.Fatalf("type = %q, want object", got.Type)
	}
	// Ignored field must be absent.
	if _, ok := got.Properties["Ignored"]; ok {
		t.Fatal("json:\"-\" field should be omitted")
	}
	if _, ok := got.Properties["ignored"]; ok {
		t.Fatal("ignored field leaked into schema")
	}
	// Required contains only path.
	if len(got.Required) != 1 || got.Required[0] != "path" {
		t.Fatalf("required = %v, want [path]", got.Required)
	}

	// Field types.
	assertProp(t, got.Properties["path"], "string", "File to read")
	// Description with a comma survives because description is the last directive.
	assertProp(t, got.Properties["offset"], "integer", "Start line, 1-indexed")
	assertProp(t, got.Properties["verbose"], "boolean", "")

	// Array carries an items type.
	var tagsProp struct {
		Type  string          `json:"type"`
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(got.Properties["tags"], &tagsProp); err != nil {
		t.Fatal(err)
	}
	if tagsProp.Type != "array" {
		t.Fatalf("tags type = %q, want array", tagsProp.Type)
	}
	var items struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(tagsProp.Items, &items); err != nil {
		t.Fatal(err)
	}
	if items.Type != "string" {
		t.Fatalf("tags items type = %q, want string", items.Type)
	}
}

func assertProp(t *testing.T, raw json.RawMessage, wantType, wantDesc string) {
	t.Helper()
	var p struct {
		Type        string `json:"type"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("bad property JSON %s: %v", raw, err)
	}
	if p.Type != wantType {
		t.Fatalf("type = %q, want %q", p.Type, wantType)
	}
	if p.Description != wantDesc {
		t.Fatalf("description = %q, want %q", p.Description, wantDesc)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tool/ -run TestSchemaFor -v`
Expected: FAIL — `undefined: schemaFor`.

- [ ] **Step 3: Write the implementation**

Create `tool/fromfunc.go`:
```go
package tool

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// schemaFor builds a JSON Schema object for a struct type by reflecting its
// exported fields and their `json` / `jsonschema` tags.
func schemaFor(t reflect.Type) json.RawMessage {
	if t == nil || t.Kind() != reflect.Struct {
		return json.RawMessage(`{"type":"object"}`)
	}
	props := make(map[string]json.RawMessage)
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, omit := jsonFieldName(f)
		if omit {
			continue
		}
		prop, req := propertyFor(f)
		props[name] = prop
		if req {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	schema := map[string]any{
		"type":       "object",
		"properties": props, // encoding/json marshals map keys sorted
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	b, _ := json.Marshal(schema)
	return b
}

// jsonFieldName resolves the JSON key for a field. It returns omit=true for
// json:"-".
func jsonFieldName(f reflect.StructField) (name string, omit bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	name = f.Name
	if tag != "" {
		if first := strings.Split(tag, ",")[0]; first != "" {
			name = first
		}
	}
	return name, false
}

// propertyFor builds the schema fragment for a single field and reports whether
// it is required. The `jsonschema` tag supports `required` and a trailing
// `description=...` (which may contain commas because it is parsed as the
// remainder of the tag).
func propertyFor(f reflect.StructField) (json.RawMessage, bool) {
	prop := map[string]any{"type": jsonType(f.Type)}
	if prop["type"] == "array" {
		prop["items"] = map[string]any{"type": jsonType(f.Type.Elem())}
	}

	required := false
	tag := f.Tag.Get("jsonschema")
	if tag != "" {
		if idx := strings.Index(tag, "description="); idx >= 0 {
			prop["description"] = tag[idx+len("description="):]
			tag = tag[:idx] // directives before description
		}
		for _, part := range strings.Split(tag, ",") {
			if strings.TrimSpace(part) == "required" {
				required = true
			}
		}
	}

	b, _ := json.Marshal(prop)
	return b, required
}

// jsonType maps a Go type to a JSON Schema type keyword.
func jsonType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return "string"
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tool/ -run TestSchemaFor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tool/fromfunc.go tool/fromfunc_test.go
git -c commit.gpgsign=false commit -m "feat(tool): JSON Schema generation from struct tags via reflection"
```

---

### Task 5: FromFunc — the generic tool wrapper

**Files:**
- Modify: `tool/fromfunc.go`
- Test: `tool/fromfunc_test.go` (append)

**Interfaces:**
- Consumes: `schemaFor` from Task 4; `Tool`, `Result`, `Context`, `ErrorResult` from Tasks 1.
- Produces:
  - `func FromFunc[T any](name, description string, fn func(ctx context.Context, args T, tctx Context) (Result, error)) Tool`.
  - Unmarshals input JSON into `T`; on unmarshal failure returns `ErrorResult(...)` with a nil error (so the model can react and retry).

- [ ] **Step 1: Write the failing test (append to `tool/fromfunc_test.go`)**

Add to `tool/fromfunc_test.go`:
```go
import additions: "context"
```
(Ensure `tool/fromfunc_test.go` imports `context` in its import block alongside the existing imports.)

Append:
```go
type echoArgs struct {
	Message string `json:"message" jsonschema:"required,description=text to echo"`
	Times   int    `json:"times"`
}

func TestFromFuncDispatch(t *testing.T) {
	var seen echoArgs
	tool := FromFunc("echo", "echoes a message",
		func(ctx context.Context, a echoArgs, tctx Context) (Result, error) {
			seen = a
			return TextResult(a.Message), nil
		})

	if tool.Name() != "echo" || tool.Description() != "echoes a message" {
		t.Fatalf("name/desc wrong: %q / %q", tool.Name(), tool.Description())
	}
	// Schema is generated from echoArgs.
	if !json.Valid(tool.Schema()) {
		t.Fatalf("schema not valid JSON: %s", tool.Schema())
	}

	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"message":"hi","times":3}`), Context{})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Message != "hi" || seen.Times != 3 {
		t.Fatalf("handler received %+v, want {hi 3}", seen)
	}
	if got := res.Content[0].(llm.TextBlock).Text; got != "hi" {
		t.Fatalf("result = %q, want hi", got)
	}
}

func TestFromFuncBadInputIsErrorResult(t *testing.T) {
	tool := FromFunc("echo", "",
		func(ctx context.Context, a echoArgs, tctx Context) (Result, error) {
			return TextResult("should not run"), nil
		})
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"times": "not a number"}`), Context{})
	if err != nil {
		t.Fatalf("bad input should not be a Go error, got %v", err)
	}
	if !res.IsError {
		t.Fatal("bad input should yield an error Result the model can react to")
	}
}
```
Add `"github.com/jelmersnoeck/agentloop/llm"` to `tool/fromfunc_test.go` imports (for `llm.TextBlock`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tool/ -run TestFromFunc -v`
Expected: FAIL — `undefined: FromFunc`.

- [ ] **Step 3: Write the implementation (append to `tool/fromfunc.go`)**

Add `"context"` and `"fmt"` to `tool/fromfunc.go`'s import block, then append:
```go
// FromFunc builds a Tool from a typed handler. The args type T is reflected to
// generate the input JSON Schema and to unmarshal the model's input before
// calling fn. T should be a struct.
func FromFunc[T any](name, description string, fn func(ctx context.Context, args T, tctx Context) (Result, error)) Tool {
	var zero T
	return &funcTool[T]{
		name:        name,
		description: description,
		schema:      schemaFor(reflect.TypeOf(zero)),
		fn:          fn,
	}
}

type funcTool[T any] struct {
	name        string
	description string
	schema      json.RawMessage
	fn          func(ctx context.Context, args T, tctx Context) (Result, error)
}

func (t *funcTool[T]) Name() string           { return t.name }
func (t *funcTool[T]) Description() string     { return t.description }
func (t *funcTool[T]) Schema() json.RawMessage { return t.schema }

func (t *funcTool[T]) Execute(ctx context.Context, input json.RawMessage, tctx Context) (Result, error) {
	var args T
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			// Surface as an error Result (not a Go error) so the model can read
			// the message and retry with corrected arguments.
			return ErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
		}
	}
	return t.fn(ctx, args, tctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tool/ -v`
Expected: PASS (all `tool` package tests).

- [ ] **Step 5: Commit**

```bash
git add tool/fromfunc.go tool/fromfunc_test.go
git -c commit.gpgsign=false commit -m "feat(tool): FromFunc generic tool wrapper with typed args"
```

---

### Task 6: truncate — Head and Tail

**Files:**
- Create: `tool/truncate/truncate.go`
- Test: `tool/truncate/truncate_test.go`

**Interfaces:**
- Consumes: standard library only.
- Produces:
  - `const DefaultMaxLines = 2000`, `DefaultMaxBytes = 50000`, `DefaultMaxLineChars = 500`.
  - `func Head(s string, maxLines, maxBytes int) (out string, truncated bool)` — keeps the first lines/bytes.
  - `func Tail(s string, maxLines, maxBytes int) (out string, truncated bool)` — keeps the last lines/bytes.

- [ ] **Step 1: Write the failing test**

Create `tool/truncate/truncate_test.go`:
```go
package truncate

import (
	"strings"
	"testing"
)

func TestHeadByLines(t *testing.T) {
	in := "a\nb\nc\nd\ne\n"
	out, trunc := Head(in, 3, 1_000_000)
	if !trunc {
		t.Fatal("expected truncated")
	}
	if out != "a\nb\nc\n" {
		t.Fatalf("Head = %q, want first 3 lines", out)
	}
}

func TestHeadByBytes(t *testing.T) {
	in := strings.Repeat("x", 100)
	out, trunc := Head(in, 1_000_000, 10)
	if !trunc || len(out) != 10 {
		t.Fatalf("Head byte-cut = %q (trunc=%v), want 10 bytes", out, trunc)
	}
}

func TestHeadNoTruncation(t *testing.T) {
	in := "short\n"
	out, trunc := Head(in, 2000, 50000)
	if trunc || out != in {
		t.Fatalf("Head should not truncate: %q trunc=%v", out, trunc)
	}
}

func TestTailByLines(t *testing.T) {
	in := "a\nb\nc\nd\ne\n"
	out, trunc := Tail(in, 2, 1_000_000)
	if !trunc {
		t.Fatal("expected truncated")
	}
	// Last two non-empty lines are d and e.
	if !strings.Contains(out, "d") || !strings.Contains(out, "e") || strings.Contains(out, "a") {
		t.Fatalf("Tail = %q, want last lines d,e", out)
	}
}

func TestTailByBytes(t *testing.T) {
	in := "0123456789abcdef"
	out, trunc := Tail(in, 1_000_000, 6)
	if !trunc || out != "abcdef" {
		t.Fatalf("Tail byte-cut = %q (trunc=%v), want last 6 bytes", out, trunc)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tool/truncate/ -run 'TestHead|TestTail' -v`
Expected: FAIL — `undefined: Head`, `Tail`.

- [ ] **Step 3: Write the implementation**

Create `tool/truncate/truncate.go`:
```go
// Package truncate provides direction-aware output caps and temp-file spillover
// for tool results, so a single oversized result can never blow the context
// window. Tools call these at their own boundary (bash keeps the tail, file
// reads keep the head, greps cap each line).
package truncate

import "strings"

// Default caps: whichever limit is hit first wins.
const (
	DefaultMaxLines     = 2000
	DefaultMaxBytes     = 50000
	DefaultMaxLineChars = 500
)

// Head keeps the first maxLines lines and at most maxBytes bytes, whichever is
// more restrictive, reporting whether anything was dropped.
func Head(s string, maxLines, maxBytes int) (string, bool) {
	truncated := false

	lines := strings.SplitAfter(s, "\n")
	// SplitAfter on a trailing "\n" yields a final empty element; drop it.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	out := strings.Join(lines, "")

	if len(out) > maxBytes {
		out = out[:maxBytes]
		truncated = true
	}
	return out, truncated
}

// Tail keeps the last maxLines lines and at most maxBytes bytes, whichever is
// more restrictive.
func Tail(s string, maxLines, maxBytes int) (string, bool) {
	truncated := false

	lines := strings.SplitAfter(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
		truncated = true
	}
	out := strings.Join(lines, "")

	if len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
		truncated = true
	}
	return out, truncated
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tool/truncate/ -run 'TestHead|TestTail' -v`
Expected: PASS (all five tests).

- [ ] **Step 5: Commit**

```bash
git add tool/truncate/truncate.go tool/truncate/truncate_test.go
git -c commit.gpgsign=false commit -m "feat(tool/truncate): direction-aware Head and Tail caps"
```

---

### Task 7: truncate — Line cap, Spill, and Apply

**Files:**
- Modify: `tool/truncate/truncate.go`
- Test: `tool/truncate/truncate_test.go` (append)

**Interfaces:**
- Consumes: `Head`, `Tail` from Task 6; standard library `os`, `fmt`.
- Produces:
  - `func Line(s string, maxChars int) string` — caps each line at maxChars, appending `... [truncated]`.
  - `func Spill(prefix, content string) (path string, err error)` — writes content to a temp file, returns its path.
  - `func Apply(content string, keepTail bool) (string, error)` — applies the default Head/Tail cap; if truncated, spills the full content and appends a `[Showing … Full output: <path>]` marker.

- [ ] **Step 1: Write the failing test (append)**

Add to `tool/truncate/truncate_test.go`:
```go
import additions: "os"
```
(Ensure the import block includes `"os"` alongside `"strings"` and `"testing"`.)

Append:
```go
func TestLineCap(t *testing.T) {
	long := strings.Repeat("y", 600)
	in := "ok\n" + long + "\n"
	out := Line(in, 500)
	firstKept := strings.Split(out, "\n")[0]
	if firstKept != "ok" {
		t.Fatalf("short line changed: %q", firstKept)
	}
	if !strings.Contains(out, "... [truncated]") {
		t.Fatal("expected per-line truncation marker")
	}
	// The long line must be capped near 500 chars plus the marker.
	longKept := strings.Split(out, "\n")[1]
	if len(longKept) > 500+len("... [truncated]") {
		t.Fatalf("long line not capped: len=%d", len(longKept))
	}
}

func TestSpillWritesFile(t *testing.T) {
	path, err := Spill("agentloop-test", "full content here")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "full content here" {
		t.Fatalf("spilled content = %q", string(data))
	}
}

func TestApplyTruncatesAndSpills(t *testing.T) {
	// 3000 lines exceeds DefaultMaxLines (2000) → truncated + spilled.
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString("line\n")
	}
	out, err := Apply(b.String(), false) // keepTail=false → Head
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Full output:") {
		t.Fatalf("expected spill marker, got tail: %q", out[len(out)-80:])
	}
	// Extract the path and confirm the full content was written.
	marker := out[strings.Index(out, "Full output:")+len("Full output:"):]
	path := strings.TrimSpace(strings.Trim(marker, "]"))
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable at %q: %v", path, err)
	}
	if strings.Count(string(data), "line") != 3000 {
		t.Fatalf("spill file missing lines: %d", strings.Count(string(data), "line"))
	}
}

func TestApplyNoTruncationNoSpill(t *testing.T) {
	out, err := Apply("small output\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Full output:") {
		t.Fatalf("small output should not spill: %q", out)
	}
	if out != "small output\n" {
		t.Fatalf("small output altered: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tool/truncate/ -run 'TestLine|TestSpill|TestApply' -v`
Expected: FAIL — `undefined: Line`, `Spill`, `Apply`.

- [ ] **Step 3: Write the implementation (append to `tool/truncate/truncate.go`)**

Change the import to a block and add `os` and `fmt`:
```go
import (
	"fmt"
	"os"
	"strings"
)
```

Append:
```go
// Line caps each line of s at maxChars runes, appending a marker to any line it
// shortens. Neither end of the output is dropped — only over-long lines shrink.
func Line(s string, maxChars int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if len(ln) > maxChars {
			lines[i] = ln[:maxChars] + "... [truncated]"
		}
	}
	return strings.Join(lines, "\n")
}

// Spill writes content to a new temp file and returns its path. The caller is
// responsible for the file's lifetime.
func Spill(prefix, content string) (string, error) {
	f, err := os.CreateTemp("", prefix+"-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// Apply caps content with the default line/byte limits — Tail when keepTail is
// true (shell output), Head otherwise (file reads). If content was truncated it
// spills the full content to a temp file and appends a marker pointing at it, so
// the model can re-read or grep the complete output.
func Apply(content string, keepTail bool) (string, error) {
	var out string
	var truncated bool
	if keepTail {
		out, truncated = Tail(content, DefaultMaxLines, DefaultMaxBytes)
	} else {
		out, truncated = Head(content, DefaultMaxLines, DefaultMaxBytes)
	}
	if !truncated {
		return out, nil
	}
	path, err := Spill("agentloop", content)
	if err != nil {
		return out, err
	}
	return fmt.Sprintf("%s\n[Output truncated. Full output: %s]", out, path), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tool/truncate/ -v`
Expected: PASS (all truncate tests).

- [ ] **Step 5: Full module verification**

Run:
```bash
go test ./...
go test -race ./tool/...
go vet ./...
gofmt -l .
```
Expected: all packages pass; race-clean; vet silent; `gofmt -l` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add tool/truncate/truncate.go tool/truncate/truncate_test.go
git -c commit.gpgsign=false commit -m "feat(tool/truncate): per-line cap, temp-file spill, and Apply helper"
```

---

## Self-Review

**1. Spec coverage (of the tool-framework slice of design §6):**
- `Tool` interface (`Name`/`Description`/`Schema`/`Execute`) + `ToolContext` + `Result` — Task 1. ✓
- `ReadOnly` as an optional interface, mutating default — Task 1 (`TestMissingReadOnlyIsMutating`). ✓
- `Registry` with `Register`/`Schemas` (sorted)/`Execute`/`Filtered` — Tasks 2–3. ✓
- `tool.FromFunc` reflection over a typed args struct — Tasks 4–5. ✓
- Direction-aware truncation (`Head`/`Tail`/`Line`) + temp-file spillover — Tasks 6–7. ✓
- Correctly deferred to later plans: the six built-in tools (Plan 3), and wiring tools + concurrency + hooks + convergence into `Run` (Plan 4). `tool/truncate` ships now because the builtins depend on it.

**2. Placeholder scan:** No TBD/TODO. Tasks 2 and 3 use an explicit "trim imports now, restore in Task 3" sequence with exact import blocks shown — no hidden work. Task 1 Step 4 removes a named-but-unused test helper with the exact lines to delete.

**3. Type consistency:** `Tool`, `Context`, `Result`, `Registry`, `FromFunc`, `schemaFor`, `Head`/`Tail`/`Line`/`Spill`/`Apply` names and signatures are used identically across every task and its tests. `llm.ToolSchema{Name, Description, InputSchema}` matches the field names defined in the foundation plan's Task 3. `Registry.Filtered(allow, deny)` and `Execute(ctx, name, input, tctx)` signatures match between the Interfaces blocks, implementations, and tests.

---

## Execution Handoff

This is the tool-framework plan (revised roadmap: Plan 2 of 7). On completion it yields a working, tested tool library — register custom tools, generate schemas from typed structs, execute with sub-agent-scoping, and cap oversized output with spillover — all with no network and no changes to the loop. Plan 3 (built-in tools) and Plan 4 (wiring tools into `Run` with hooks and convergence) build on it.
