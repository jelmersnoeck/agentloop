# agentloop Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the foundational `llm` contracts package, a scriptable `mock` provider, and a minimal command-in/event-out agent loop that runs a single LLM turn — proving the core architecture end-to-end with zero network.

**Architecture:** A dependency-free `llm` package defines the serializable `Context`, typed content `Block`s, the streaming `Provider` interface, and the normalized `Event` stream. A `mock` provider replays scripted event streams so the entire loop is testable offline. A minimal `Agent` exposes a command channel in and an event channel out, running one provider turn and stopping when the assistant returns no tool calls.

**Tech Stack:** Go 1.24, standard library only (`encoding/json`, `context`, `testing`). No third-party dependencies in this plan.

## Global Constraints

- Module path: `github.com/jelmersnoeck/agentloop` (exact).
- Go version floor: `go 1.24`.
- The `llm` package MUST NOT import any other package in this module. Every other package may import `llm`, never the reverse.
- Streaming is the only provider mode. Non-streaming = drain the channel.
- Content blocks are a typed interface union with a `type` discriminator in JSON; `ThinkingBlock` MUST round-trip its `Signature` field verbatim.
- Reasoning tier values are exactly: `minimal`, `low`, `medium`, `high`, `xhigh`.
- Test-driven: every task writes the failing test first, watches it fail, then implements.
- Commit after every task. Use `git ... -c commit.gpgsign=false` (this environment has no GPG secret key).

---

## File Structure

- `go.mod` — module definition.
- `llm/block.go` — `Block` interface, the four concrete block types, `BlockType`, JSON marshaling.
- `llm/block_test.go` — block JSON round-trip tests.
- `llm/context.go` — `Context`, `Message`, `Role`, `ToolSchema`.
- `llm/context_test.go` — Context serialization tests.
- `llm/request.go` — `Request`, `Reasoning`, `ModelRef`, `Event`, `EventType`, `ToolCallDelta`, `Usage`, `CacheUsage`.
- `llm/provider.go` — `Provider` interface + optional capability interfaces.
- `llm/mock/mock.go` — scriptable mock provider.
- `llm/mock/mock_test.go` — mock provider tests.
- `agent.go` — `Agent`, functional options, command/event types, the minimal loop.
- `agent_test.go` — end-to-end loop test driven by the mock provider.

---

### Task 1: Module initialization

**Files:**
- Create: `go.mod`
- Create: `llm/doc.go`

**Interfaces:**
- Consumes: nothing.
- Produces: a compilable module named `github.com/jelmersnoeck/agentloop`.

- [ ] **Step 1: Initialize the module**

Run:
```bash
cd /Users/jelmersnoeck/Projects/agentloop
go mod init github.com/jelmersnoeck/agentloop
```
Expected: creates `go.mod` containing `module github.com/jelmersnoeck/agentloop` and `go 1.24` (or the local toolchain's line).

- [ ] **Step 2: Add a package doc file so `llm` compiles as a package**

Create `llm/doc.go`:
```go
// Package llm defines the provider-agnostic contracts every agentloop
// component depends on: the serializable Context, typed content Blocks,
// the streaming Provider interface, and the normalized Event stream.
//
// This package MUST NOT import any other package in this module.
package llm
```

- [ ] **Step 3: Verify it builds**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add go.mod llm/doc.go
git -c commit.gpgsign=false commit -m "chore: initialize agentloop Go module"
```

---

### Task 2: Content block types with JSON round-tripping

**Files:**
- Create: `llm/block.go`
- Test: `llm/block_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type BlockType string` with constants `BlockText`, `BlockThinking`, `BlockToolUse`, `BlockToolResult`.
  - `type Block interface { Type() BlockType }`.
  - `type TextBlock struct { Text string }`.
  - `type ThinkingBlock struct { Text string; Signature string }`.
  - `type ToolUseBlock struct { ID string; Name string; Input json.RawMessage }`.
  - `type ToolResultBlock struct { ToolUseID string; Content []Block; IsError bool }`.
  - `func MarshalBlock(b Block) ([]byte, error)` and `func UnmarshalBlock(data []byte) (Block, error)`.

- [ ] **Step 1: Write the failing test**

Create `llm/block_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./llm/ -run TestBlock -v`
Expected: FAIL — compilation error, `undefined: Block`, `MarshalBlock`, etc.

- [ ] **Step 3: Write the implementation**

Create `llm/block.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./llm/ -run 'TestBlock|TestThinking|TestToolResult' -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add llm/block.go llm/block_test.go
git -c commit.gpgsign=false commit -m "feat(llm): typed content blocks with JSON round-tripping"
```

---

### Task 3: Context, Message, and ToolSchema

**Files:**
- Create: `llm/context.go`
- Test: `llm/context_test.go`

**Interfaces:**
- Consumes: `Block` from Task 2.
- Produces:
  - `type Role string` with `RoleUser`, `RoleAssistant`.
  - `type Message struct { Role Role; Content []Block }` with JSON round-trip via `MarshalBlock`/`UnmarshalBlock`.
  - `type ToolSchema struct { Name string; Description string; InputSchema json.RawMessage }`.
  - `type Context struct { System []Block; Messages []Message; Tools []ToolSchema }`.
  - `func (m Message) MarshalJSON() ([]byte, error)` / `func (m *Message) UnmarshalJSON([]byte) error`.

- [ ] **Step 1: Write the failing test**

Create `llm/context_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./llm/ -run 'TestMessage|TestContext' -v`
Expected: FAIL — `undefined: Message`, `Context`, `ToolSchema`, `RoleAssistant`.

- [ ] **Step 3: Write the implementation**

Create `llm/context.go`:
```go
package llm

import "encoding/json"

// Role identifies the author of a Message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one conversational turn: a role plus ordered content blocks.
type Message struct {
	Role    Role
	Content []Block
}

type messageWire struct {
	Role    Role              `json:"role"`
	Content []json.RawMessage `json:"content"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	w := messageWire{Role: m.Role}
	for _, b := range m.Content {
		raw, err := MarshalBlock(b)
		if err != nil {
			return nil, err
		}
		w.Content = append(w.Content, raw)
	}
	return json.Marshal(w)
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var w messageWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	m.Role = w.Role
	m.Content = nil
	for _, raw := range w.Content {
		b, err := UnmarshalBlock(raw)
		if err != nil {
			return err
		}
		m.Content = append(m.Content, b)
	}
	return nil
}

// ToolSchema is the provider-neutral description of a callable tool.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Context is the serializable source of truth for a conversation. Copying or
// serializing it is how state is persisted and handed to sub-agents.
type Context struct {
	System   []systemBlocks `json:"-"`
	Messages []Message      `json:"messages"`
	Tools    []ToolSchema   `json:"tools"`
}
```

> NOTE: `System` must serialize the same way `Message.Content` does. Replace the
> `Context` struct above with the version below, which uses a small named slice
> type carrying the block marshaling. (Written as its own step to keep the
> change explicit.)

- [ ] **Step 4: Replace the Context definition with a serializable System field**

In `llm/context.go`, replace the `Context` struct and add the `blockSlice` helper:
```go
// blockSlice marshals a []Block using the block type discriminator.
type blockSlice []Block

func (bs blockSlice) MarshalJSON() ([]byte, error) {
	raws := make([]json.RawMessage, 0, len(bs))
	for _, b := range bs {
		raw, err := MarshalBlock(b)
		if err != nil {
			return nil, err
		}
		raws = append(raws, raw)
	}
	return json.Marshal(raws)
}

func (bs *blockSlice) UnmarshalJSON(data []byte) error {
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return err
	}
	out := make(blockSlice, 0, len(raws))
	for _, raw := range raws {
		b, err := UnmarshalBlock(raw)
		if err != nil {
			return err
		}
		out = append(out, b)
	}
	*bs = out
	return nil
}

// Context is the serializable source of truth for a conversation.
type Context struct {
	System   blockSlice   `json:"system"`
	Messages []Message    `json:"messages"`
	Tools    []ToolSchema `json:"tools"`
}
```
Then delete the earlier broken `Context` struct and the unused `systemBlocks` reference.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./llm/ -run 'TestMessage|TestContext' -v`
Expected: PASS. (`Context.System` is a `blockSlice`; the test's `[]Block{...}` literal assigns to it directly.)

- [ ] **Step 6: Commit**

```bash
git add llm/context.go llm/context_test.go
git -c commit.gpgsign=false commit -m "feat(llm): serializable Context, Message, and ToolSchema"
```

---

### Task 4: Request, Event, and Usage types

**Files:**
- Create: `llm/request.go`
- Test: `llm/request_test.go`

**Interfaces:**
- Consumes: `Context` from Task 3.
- Produces:
  - `type Reasoning string` with `ReasoningMinimal`, `ReasoningLow`, `ReasoningMedium`, `ReasoningHigh`, `ReasoningXHigh`.
  - `type ModelRef struct { Model string; Reasoning Reasoning }`.
  - `type Request struct { Model string; Reasoning Reasoning; Context Context; MaxTokens int }`.
  - `type EventType string` with `EventText`, `EventThinking`, `EventToolUseStart`, `EventToolUseDelta`, `EventToolUseStop`, `EventUsage`, `EventMessageStop`, `EventError`.
  - `type ToolCallDelta struct { ID string; Name string; PartialJSON string }`.
  - `type CacheUsage struct { ReadTokens int; WriteTokens int; WriteTokens1h int }`.
  - `type Usage struct { InputTokens int; OutputTokens int; Cache CacheUsage }`.
  - `type Event struct { Type EventType; Text string; ToolCall *ToolCallDelta; Usage *Usage; Err error }`.

- [ ] **Step 1: Write the failing test**

Create `llm/request_test.go`:
```go
package llm

import "testing"

func TestReasoningValues(t *testing.T) {
	got := []Reasoning{ReasoningMinimal, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh}
	want := []string{"minimal", "low", "medium", "high", "xhigh"}
	for i, r := range got {
		if string(r) != want[i] {
			t.Fatalf("reasoning[%d]: got %q want %q", i, r, want[i])
		}
	}
}

func TestEventConstruction(t *testing.T) {
	e := Event{Type: EventText, Text: "hi"}
	if e.Type != EventText || e.Text != "hi" {
		t.Fatalf("unexpected event: %+v", e)
	}
	u := Event{Type: EventUsage, Usage: &Usage{InputTokens: 10, Cache: CacheUsage{ReadTokens: 4}}}
	if u.Usage.Cache.ReadTokens != 4 {
		t.Fatalf("cache read tokens not set: %+v", u.Usage)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./llm/ -run 'TestReasoning|TestEvent' -v`
Expected: FAIL — `undefined: Reasoning`, `Event`, etc.

- [ ] **Step 3: Write the implementation**

Create `llm/request.go`:
```go
package llm

// Reasoning is a provider-neutral reasoning tier. Adapters map it to each
// provider's native effort/thinking controls.
type Reasoning string

const (
	ReasoningMinimal Reasoning = "minimal"
	ReasoningLow     Reasoning = "low"
	ReasoningMedium  Reasoning = "medium"
	ReasoningHigh    Reasoning = "high"
	ReasoningXHigh   Reasoning = "xhigh"
)

// ModelRef binds a concrete model id to a reasoning tier (used by logical tiers).
type ModelRef struct {
	Model     string
	Reasoning Reasoning
}

// Request is one provider call. The provider is resolved from Model elsewhere.
type Request struct {
	Model     string
	Reasoning Reasoning
	Context   Context
	MaxTokens int
}

// EventType enumerates the normalized streaming events.
type EventType string

const (
	EventText         EventType = "text"
	EventThinking     EventType = "thinking"
	EventToolUseStart EventType = "tool_use_start"
	EventToolUseDelta EventType = "tool_use_delta"
	EventToolUseStop  EventType = "tool_use_stop"
	EventUsage        EventType = "usage"
	EventMessageStop  EventType = "message_stop"
	EventError        EventType = "error"
)

// ToolCallDelta carries an incrementally-streamed tool call.
type ToolCallDelta struct {
	ID          string
	Name        string
	PartialJSON string
}

// CacheUsage normalizes cache token accounting across providers.
type CacheUsage struct {
	ReadTokens    int
	WriteTokens   int
	WriteTokens1h int
}

// Usage is the token/cost accounting for a turn.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Cache        CacheUsage
}

// Event is a single normalized streaming delta from a Provider.
type Event struct {
	Type     EventType
	Text     string
	ToolCall *ToolCallDelta
	Usage    *Usage
	Err      error
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./llm/ -run 'TestReasoning|TestEvent' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add llm/request.go llm/request_test.go
git -c commit.gpgsign=false commit -m "feat(llm): Request, Event stream, and Usage types"
```

---

### Task 5: Provider interface and capability interfaces

**Files:**
- Create: `llm/provider.go`
- Test: `llm/provider_test.go`

**Interfaces:**
- Consumes: `Request`, `Event` from Task 4.
- Produces:
  - `type Provider interface { Stream(ctx context.Context, req Request) (<-chan Event, error) }`.
  - `type ModelDefaulter interface { DefaultModel() string }`.
  - `type ModelMatcher interface { Serves(model string) bool }`.
  - `type ModelInfo struct { ID string; DisplayName string }`.
  - `type ModelLister interface { Models(ctx context.Context) ([]ModelInfo, error) }`.

- [ ] **Step 1: Write the failing test**

Create `llm/provider_test.go`:
```go
package llm

import (
	"context"
	"testing"
)

// staticProvider is a compile-time check that a minimal type satisfies Provider.
type staticProvider struct{}

func (staticProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

func TestProviderInterfaceSatisfied(t *testing.T) {
	var p Provider = staticProvider{}
	ch, err := p.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("expected closed channel")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./llm/ -run TestProvider -v`
Expected: FAIL — `undefined: Provider`.

- [ ] **Step 3: Write the implementation**

Create `llm/provider.go`:
```go
package llm

import "context"

// Provider is the single seam every LLM backend implements. Streaming is the
// only mode; non-streaming callers drain the channel to completion.
type Provider interface {
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// ModelDefaulter is an optional capability: a provider that has a default model.
type ModelDefaulter interface {
	DefaultModel() string
}

// ModelMatcher is an optional capability used by the Router to resolve a model
// id to the provider that serves it.
type ModelMatcher interface {
	Serves(model string) bool
}

// ModelInfo describes a single model a provider can serve.
type ModelInfo struct {
	ID          string
	DisplayName string
}

// ModelLister is an optional capability: a provider that can enumerate models.
type ModelLister interface {
	Models(ctx context.Context) ([]ModelInfo, error)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./llm/ -run TestProvider -v`
Expected: PASS.

- [ ] **Step 5: Run the whole llm package to confirm nothing regressed**

Run: `go test ./llm/ -v`
Expected: PASS (all tests from Tasks 2–5).

- [ ] **Step 6: Commit**

```bash
git add llm/provider.go llm/provider_test.go
git -c commit.gpgsign=false commit -m "feat(llm): Provider interface and optional capability interfaces"
```

---

### Task 6: Scriptable mock provider

**Files:**
- Create: `llm/mock/mock.go`
- Test: `llm/mock/mock_test.go`

**Interfaces:**
- Consumes: `llm.Provider`, `llm.Event`, `llm.Request`.
- Produces:
  - `type Turn struct { Events []llm.Event }` — one scripted provider response.
  - `type Provider struct { Turns []Turn; ... }` — replays one `Turn` per `Stream` call, in order.
  - `func New(turns ...Turn) *Provider`.
  - `func TextTurn(text string) Turn` — helper: a turn that streams one text delta + message_stop.
  - `func (p *Provider) Stream(ctx, req) (<-chan llm.Event, error)` satisfying `llm.Provider`.
  - `func (p *Provider) Requests() []llm.Request` — captured requests, for assertions.

- [ ] **Step 1: Write the failing test**

Create `llm/mock/mock_test.go`:
```go
package mock

import (
	"context"
	"testing"

	"github.com/jelmersnoeck/agentloop/llm"
)

func drain(ch <-chan llm.Event) []llm.Event {
	var out []llm.Event
	for e := range ch {
		out = append(out, e)
	}
	return out
}

func TestMockReplaysTurnsInOrder(t *testing.T) {
	p := New(TextTurn("first"), TextTurn("second"))

	ch1, err := p.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	got1 := drain(ch1)
	if got1[0].Type != llm.EventText || got1[0].Text != "first" {
		t.Fatalf("turn 1 unexpected: %+v", got1)
	}

	ch2, _ := p.Stream(context.Background(), llm.Request{Model: "m"})
	got2 := drain(ch2)
	if got2[0].Text != "second" {
		t.Fatalf("turn 2 unexpected: %+v", got2)
	}
}

func TestMockCapturesRequests(t *testing.T) {
	p := New(TextTurn("x"))
	_, _ = p.Stream(context.Background(), llm.Request{Model: "opus"})
	reqs := p.Requests()
	if len(reqs) != 1 || reqs[0].Model != "opus" {
		t.Fatalf("captured requests wrong: %+v", reqs)
	}
}

func TestMockErrorsWhenExhausted(t *testing.T) {
	p := New(TextTurn("only"))
	_, _ = p.Stream(context.Background(), llm.Request{})
	if _, err := p.Stream(context.Background(), llm.Request{}); err == nil {
		t.Fatal("expected error when turns exhausted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./llm/mock/ -v`
Expected: FAIL — `undefined: New`, `TextTurn`.

- [ ] **Step 3: Write the implementation**

Create `llm/mock/mock.go`:
```go
// Package mock provides a scriptable llm.Provider that replays predefined
// event streams, so the loop and higher layers are testable without a network.
package mock

import (
	"context"
	"fmt"
	"sync"

	"github.com/jelmersnoeck/agentloop/llm"
)

// Turn is one scripted provider response: the events to stream, in order.
type Turn struct {
	Events []llm.Event
}

// TextTurn is a convenience Turn that streams a single text delta followed by
// a message_stop event.
func TextTurn(text string) Turn {
	return Turn{Events: []llm.Event{
		{Type: llm.EventText, Text: text},
		{Type: llm.EventMessageStop},
	}}
}

// Provider replays one Turn per Stream call, in order, and records requests.
type Provider struct {
	mu     sync.Mutex
	Turns  []Turn
	next   int
	reqs   []llm.Request
}

// New builds a mock Provider from an ordered list of Turns.
func New(turns ...Turn) *Provider {
	return &Provider{Turns: turns}
}

// Stream satisfies llm.Provider. It returns the next scripted Turn's events on
// a channel, or an error if the script is exhausted.
func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.reqs = append(p.reqs, req)
	if p.next >= len(p.Turns) {
		return nil, fmt.Errorf("mock: no scripted turn %d (only %d defined)", p.next, len(p.Turns))
	}
	turn := p.Turns[p.next]
	p.next++

	ch := make(chan llm.Event)
	go func() {
		defer close(ch)
		for _, e := range turn.Events {
			select {
			case <-ctx.Done():
				return
			case ch <- e:
			}
		}
	}()
	return ch, nil
}

// Requests returns a copy of the requests captured so far.
func (p *Provider) Requests() []llm.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]llm.Request, len(p.reqs))
	copy(out, p.reqs)
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./llm/mock/ -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add llm/mock/mock.go llm/mock/mock_test.go
git -c commit.gpgsign=false commit -m "feat(llm/mock): scriptable provider for offline loop testing"
```

---

### Task 7: Minimal command-in/event-out agent loop

**Files:**
- Create: `agent.go`
- Test: `agent_test.go`

**Interfaces:**
- Consumes: `llm.Provider`, `llm.Event`, `llm.Request`, `llm.Context`, `llm.Message`, `llm.Block`, `llm.TextBlock`, `llm.RoleUser`, `llm.RoleAssistant`, `llm.EventText`, `llm.EventMessageStop`, `llm.EventError`.
- Produces:
  - `type Command interface{ isCommand() }` with `Send struct{ Text string }` and `Stop struct{}` (both implement `isCommand()`).
  - `type Option func(*Agent)` with `WithProvider(p llm.Provider) Option` and `WithModel(model string) Option`.
  - `func New(opts ...Option) (*Agent, error)` — errors if no provider is configured.
  - `type Agent struct { ... }` with `func (a *Agent) Commands() chan<- Command` and `func (a *Agent) Events() <-chan llm.Event`.
  - `func (a *Agent) Run(ctx context.Context) error` — drives the loop until `Stop`, ctx cancel, or the command channel closes.

**Loop semantics for this task:** on `Send{Text}`, append a user message, call `provider.Stream`, forward every event to `Events()`, accumulate text into an assistant message, and — because there are no tools yet — stop the turn at `EventMessageStop` (assistant produced no tool calls = turn done). `Stop{}` and ctx cancellation end `Run`.

- [ ] **Step 1: Write the failing test**

Create `agent_test.go`:
```go
package agentloop

import (
	"context"
	"testing"
	"time"

	"github.com/jelmersnoeck/agentloop/llm"
	"github.com/jelmersnoeck/agentloop/llm/mock"
)

func TestAgentRunsOneTurn(t *testing.T) {
	p := mock.New(mock.TextTurn("hello world"))
	a, err := New(WithProvider(p), WithModel("test-model"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = a.Run(ctx) }()

	a.Commands() <- Send{Text: "hi"}

	var text string
	for e := range a.Events() {
		if e.Type == llm.EventText {
			text += e.Text
		}
		if e.Type == llm.EventMessageStop {
			break
		}
	}
	if text != "hello world" {
		t.Fatalf("got %q want %q", text, "hello world")
	}

	// The provider must have received the user message.
	reqs := p.Requests()
	if len(reqs) != 1 {
		t.Fatalf("want 1 request, got %d", len(reqs))
	}
	last := reqs[0].Context.Messages
	if len(last) != 1 || last[0].Role != llm.RoleUser {
		t.Fatalf("expected one user message, got %+v", last)
	}

	a.Commands() <- Stop{}
}

func TestNewRequiresProvider(t *testing.T) {
	if _, err := New(); err == nil {
		t.Fatal("expected error when no provider configured")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run 'TestAgent|TestNew' -v`
Expected: FAIL — `undefined: New`, `Send`, `Stop`, `WithProvider`.

- [ ] **Step 3: Write the implementation**

Create `agent.go`:
```go
// Package agentloop is the public API for building agents: a command-in,
// event-out loop over a pluggable llm.Provider.
package agentloop

import (
	"context"
	"errors"

	"github.com/jelmersnoeck/agentloop/llm"
)

// Command drives the agent. Concrete commands are Send and Stop.
type Command interface{ isCommand() }

// Send starts or continues a turn with a user message.
type Send struct{ Text string }

func (Send) isCommand() {}

// Stop ends the run gracefully.
type Stop struct{}

func (Stop) isCommand() {}

// Option configures an Agent.
type Option func(*Agent)

// WithProvider sets the LLM provider the loop calls.
func WithProvider(p llm.Provider) Option {
	return func(a *Agent) { a.provider = p }
}

// WithModel sets the model id sent on each request.
func WithModel(model string) Option {
	return func(a *Agent) { a.model = model }
}

// Agent runs the loop. Construct with New; drive via Commands(); observe via Events().
type Agent struct {
	provider llm.Provider
	model    string

	commands chan Command
	events   chan llm.Event
	history  []llm.Message
}

// New builds an Agent. It errors if no provider was configured.
func New(opts ...Option) (*Agent, error) {
	a := &Agent{
		commands: make(chan Command),
		events:   make(chan llm.Event),
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.provider == nil {
		return nil, errors.New("agentloop: no provider configured (use WithProvider or WithDefaultConfig)")
	}
	return a, nil
}

// Commands returns the channel used to drive the agent.
func (a *Agent) Commands() chan<- Command { return a.commands }

// Events returns the channel of streamed events.
func (a *Agent) Events() <-chan llm.Event { return a.events }

// Run drives the loop until Stop, ctx cancellation, or the command channel closes.
func (a *Agent) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case cmd, ok := <-a.commands:
			if !ok {
				return nil
			}
			switch c := cmd.(type) {
			case Stop:
				return nil
			case Send:
				if err := a.runTurn(ctx, c.Text); err != nil {
					a.emit(llm.Event{Type: llm.EventError, Err: err})
				}
			}
		}
	}
}

func (a *Agent) emit(e llm.Event) {
	select {
	case a.events <- e:
	default:
		// Non-blocking so a slow consumer cannot deadlock the loop; a buffered
		// or ranged consumer receives events normally.
		a.events <- e
	}
}

// runTurn appends the user message, streams one provider response, forwards
// events, and records the assistant message. With no tools, the turn ends at
// EventMessageStop.
func (a *Agent) runTurn(ctx context.Context, text string) error {
	a.history = append(a.history, llm.Message{
		Role:    llm.RoleUser,
		Content: []llm.Block{llm.TextBlock{Text: text}},
	})

	req := llm.Request{
		Model:   a.model,
		Context: llm.Context{Messages: a.history},
	}
	ch, err := a.provider.Stream(ctx, req)
	if err != nil {
		return err
	}

	var assistantText string
	for e := range ch {
		a.emit(e)
		switch e.Type {
		case llm.EventText:
			assistantText += e.Text
		case llm.EventError:
			if e.Err != nil {
				return e.Err
			}
		}
	}

	a.history = append(a.history, llm.Message{
		Role:    llm.RoleAssistant,
		Content: []llm.Block{llm.TextBlock{Text: assistantText}},
	})
	return nil
}
```

> NOTE: the `emit` method above has a redundant branch. Simplify it in the next step.

- [ ] **Step 4: Simplify emit to a context-free blocking send**

In `agent.go`, replace the `emit` method with:
```go
func (a *Agent) emit(e llm.Event) {
	a.events <- e
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test . -run 'TestAgent|TestNew' -v`
Expected: PASS (both tests).

- [ ] **Step 6: Run the full module test suite**

Run: `go test ./... -v`
Expected: PASS across `llm`, `llm/mock`, and the root package.

- [ ] **Step 7: Vet and tidy**

Run:
```bash
go vet ./...
go mod tidy
```
Expected: no output from vet; `go.mod`/`go.sum` unchanged (no external deps yet).

- [ ] **Step 8: Commit**

```bash
git add agent.go agent_test.go go.mod
git -c commit.gpgsign=false commit -m "feat: minimal command-in/event-out agent loop over a provider"
```

---

## Self-Review

**1. Spec coverage (of the Foundation slice):**
- `llm` contracts package — Tasks 2–5. ✓
- Serializable `Context` with thinking-signature preservation — Tasks 2, 3 (`TestThinkingSignaturePreserved`, `TestMessageRoundTrip`). ✓
- `Provider` streaming interface + capability interfaces — Task 5. ✓
- Reasoning tiers (exact five values) — Task 4 (`TestReasoningValues`). ✓
- Unified `Usage`/`CacheUsage` present from day one — Task 4. ✓
- `mock` provider for offline testing — Task 6. ✓
- Command-in/event-out loop, single turn, stop-on-no-tool-calls — Task 7. ✓
- Deferred to later plans (correctly out of scope here): real providers, tools, steering/follow-up queues, hooks, convergence, sub-agents, groups, routing/tiers, AGENTS.md, caching adapters. These are Plans 2–5.

**2. Placeholder scan:** No TBD/TODO. Two tasks (3 and 7) intentionally include a "write, then correct in the next explicit step" refinement — each shows the exact replacement code, so there is no hidden work. ✓

**3. Type consistency:** `llm.Event`, `EventType` constants, `Context.System` as `blockSlice` (assignable from `[]Block` literals), `Message` custom JSON, `Provider.Stream` signature, and mock `Turn`/`New`/`TextTurn` names are used identically across tasks and tests. `Agent` uses `WithProvider`/`WithModel` consistently between the Interfaces block, implementation, and tests. ✓

---

## Execution Handoff

This is Plan 1 of 5. On completion it yields a working, offline-testable minimal agent. Plans 2–5 (tools+loop, real providers, sub-agents+groups, config+caching) each build on this foundation and will be written as their own documents.
