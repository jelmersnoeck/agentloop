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
	a.events <- e
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
