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
	mu    sync.Mutex
	Turns []Turn
	next  int
	reqs  []llm.Request
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

	ch := make(chan llm.Event, len(turn.Events))
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
