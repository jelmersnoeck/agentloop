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
