// Package agentloop is the public API for building agents: a blocking Run loop
// over a pluggable llm.Provider that streams events to a callback. Asynchronous
// input is injected with the Steer and Follow methods.
package agentloop

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/jelmersnoeck/agentloop/llm"
)

// EventFunc receives streamed events during a Run. It is called synchronously
// for every event, in order. It may be nil.
type EventFunc func(llm.Event)

// Result is the outcome of a completed Run.
type Result struct {
	// FinalText is the concatenated text of the final assistant message.
	FinalText string
	// Messages is the full conversation, including the seeded task.
	Messages []llm.Message
}

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

// WithOnEvent sets a default event handler, used by the package-level Run.
func WithOnEvent(fn EventFunc) Option {
	return func(a *Agent) { a.onEvent = fn }
}

// Agent runs the loop. Construct with New, drive with Run, and inject messages
// asynchronously with Steer and Follow.
type Agent struct {
	provider llm.Provider
	model    string
	onEvent  EventFunc

	mu      sync.Mutex
	history []llm.Message
	steerQ  []string
	followQ []string
}

// New builds an Agent. It errors if no provider was configured.
func New(opts ...Option) (*Agent, error) {
	a := &Agent{}
	for _, opt := range opts {
		opt(a)
	}
	if a.provider == nil {
		return nil, errors.New("agentloop: no provider configured (use WithProvider)")
	}
	return a, nil
}

// Steer enqueues a high-priority steering message, injected at the next turn
// boundary ahead of any follow-ups. Safe to call from any goroutine.
func (a *Agent) Steer(text string) {
	a.mu.Lock()
	a.steerQ = append(a.steerQ, text)
	a.mu.Unlock()
}

// Follow enqueues a sequential follow-up message, processed after the current
// work and any steering drains. Safe to call from any goroutine.
func (a *Agent) Follow(text string) {
	a.mu.Lock()
	a.followQ = append(a.followQ, text)
	a.mu.Unlock()
}

// Run drives the loop to completion, blocking. It seeds the conversation with
// task, streams every event to onEvent (which may be nil), and returns when the
// assistant produces no tool calls and no messages remain queued, or when ctx
// is cancelled. Queued Steer/Follow messages continue the loop.
func (a *Agent) Run(ctx context.Context, task string, onEvent EventFunc) (Result, error) {
	a.appendUser(task)

	for {
		if err := ctx.Err(); err != nil {
			return a.result(), err
		}

		msg, err := a.runTurn(ctx, onEvent)
		if err != nil {
			return a.result(), err
		}
		a.appendMessage(msg)

		// With no tools yet, a turn always ends without tool calls. Drain the
		// next queued message to continue; otherwise the run is complete.
		if next, ok := a.nextQueued(); ok {
			a.appendUser(next)
			continue
		}
		return a.result(), nil
	}
}

// runTurn streams one provider response, forwarding every event to onEvent and
// accumulating the assistant message.
func (a *Agent) runTurn(ctx context.Context, onEvent EventFunc) (llm.Message, error) {
	req := llm.Request{
		Model:   a.model,
		Context: llm.Context{Messages: a.snapshotHistory()},
	}
	ch, err := a.provider.Stream(ctx, req)
	if err != nil {
		return llm.Message{}, err
	}

	var text strings.Builder
	for e := range ch {
		if onEvent != nil {
			onEvent(e)
		}
		switch e.Type {
		case llm.EventText:
			text.WriteString(e.Text)
		case llm.EventError:
			if e.Err != nil {
				// The error was already streamed to onEvent above; return it as
				// the terminal error so Run reports it to the caller. Partial
				// assistant text is intentionally dropped (a deferred concern).
				return llm.Message{}, e.Err
			}
		}
	}

	return llm.Message{
		Role:    llm.RoleAssistant,
		Content: []llm.Block{llm.TextBlock{Text: text.String()}},
	}, nil
}

// nextQueued returns the next queued message, steering ahead of follow-ups.
func (a *Agent) nextQueued() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.steerQ) > 0 {
		text := a.steerQ[0]
		a.steerQ = a.steerQ[1:]
		return text, true
	}
	if len(a.followQ) > 0 {
		text := a.followQ[0]
		a.followQ = a.followQ[1:]
		return text, true
	}
	return "", false
}

func (a *Agent) appendUser(text string) {
	a.appendMessage(llm.Message{
		Role:    llm.RoleUser,
		Content: []llm.Block{llm.TextBlock{Text: text}},
	})
}

func (a *Agent) appendMessage(m llm.Message) {
	a.mu.Lock()
	a.history = append(a.history, m)
	a.mu.Unlock()
}

func (a *Agent) snapshotHistory() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]llm.Message, len(a.history))
	copy(out, a.history)
	return out
}

func (a *Agent) result() Result {
	msgs := a.snapshotHistory()
	final := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != llm.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, blk := range msgs[i].Content {
			if tb, ok := blk.(llm.TextBlock); ok {
				b.WriteString(tb.Text)
			}
		}
		final = b.String()
		break
	}
	return Result{FinalText: final, Messages: msgs}
}

// Run constructs an Agent from opts and runs task to completion, streaming to
// the handler set via WithOnEvent. A one-shot convenience for the common case.
func Run(ctx context.Context, task string, opts ...Option) (Result, error) {
	a, err := New(opts...)
	if err != nil {
		return Result{}, err
	}
	return a.Run(ctx, task, a.onEvent)
}
