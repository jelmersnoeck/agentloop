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
