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
