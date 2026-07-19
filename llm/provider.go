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
