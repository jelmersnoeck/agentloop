package agentloop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jelmersnoeck/agentloop/llm"
	"github.com/jelmersnoeck/agentloop/llm/mock"
)

func TestRunStreamsAndReturnsResult(t *testing.T) {
	p := mock.New(mock.TextTurn("hello world"))
	a, err := New(WithProvider(p), WithModel("test-model"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var streamed string
	res, err := a.Run(ctx, "hi", func(e llm.Event) {
		if e.Type == llm.EventText {
			streamed += e.Text
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if streamed != "hello world" {
		t.Fatalf("streamed = %q, want %q", streamed, "hello world")
	}
	if res.FinalText != "hello world" {
		t.Fatalf("FinalText = %q, want %q", res.FinalText, "hello world")
	}

	// The provider must have received the user task as the sole message.
	reqs := p.Requests()
	if len(reqs) != 1 {
		t.Fatalf("want 1 request, got %d", len(reqs))
	}
	msgs := reqs[0].Context.Messages
	if len(msgs) != 1 || msgs[0].Role != llm.RoleUser {
		t.Fatalf("expected one user message, got %+v", msgs)
	}
}

func TestRunNilHandler(t *testing.T) {
	p := mock.New(mock.TextTurn("ok"))
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Run(context.Background(), "go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalText != "ok" {
		t.Fatalf("FinalText = %q, want %q", res.FinalText, "ok")
	}
}

// TestFollowContinuesLoop verifies a queued follow-up drives a second turn and
// that Result reflects the final assistant message.
func TestFollowContinuesLoop(t *testing.T) {
	p := mock.New(mock.TextTurn("first"), mock.TextTurn("second"))
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}
	a.Follow("again")

	res, err := a.Run(context.Background(), "start", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalText != "second" {
		t.Fatalf("FinalText = %q, want %q", res.FinalText, "second")
	}
	if n := len(p.Requests()); n != 2 {
		t.Fatalf("want 2 provider calls, got %d", n)
	}
}

// TestSteerPrecedesFollow verifies steering messages drain ahead of follow-ups.
func TestSteerPrecedesFollow(t *testing.T) {
	p := mock.New(
		mock.TextTurn("t1"), // seeded task
		mock.TextTurn("t2"), // steer
		mock.TextTurn("t3"), // follow
	)
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}
	a.Follow("f")
	a.Steer("s")

	_, err = a.Run(context.Background(), "start", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Requests after the seeded task: the steer message must arrive before the
	// follow message.
	reqs := p.Requests()
	if len(reqs) != 3 {
		t.Fatalf("want 3 provider calls, got %d", len(reqs))
	}
	lastUser := func(r llm.Request) string {
		msgs := r.Context.Messages
		return msgs[len(msgs)-1].Content[0].(llm.TextBlock).Text
	}
	if got := lastUser(reqs[1]); got != "s" {
		t.Fatalf("second turn last user = %q, want steer %q", got, "s")
	}
	if got := lastUser(reqs[2]); got != "f" {
		t.Fatalf("third turn last user = %q, want follow %q", got, "f")
	}
}

// TestRunSurfacesProviderErrorOnce guards against a mid-stream provider error
// being streamed to the callback more than once, and confirms Run returns it.
func TestRunSurfacesProviderErrorOnce(t *testing.T) {
	sentinel := errors.New("provider boom")
	p := mock.New(mock.Turn{Events: []llm.Event{
		{Type: llm.EventError, Err: sentinel},
	}})
	a, err := New(WithProvider(p))
	if err != nil {
		t.Fatal(err)
	}

	var errCount int
	_, err = a.Run(context.Background(), "go", func(e llm.Event) {
		if e.Type == llm.EventError && e.Err != nil {
			errCount++
		}
	})
	if errCount != 1 {
		t.Fatalf("error streamed %d times, want exactly 1", errCount)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Run returned err = %v, want sentinel", err)
	}
}

// TestPackageRun exercises the one-shot package-level Run with WithOnEvent.
func TestPackageRun(t *testing.T) {
	p := mock.New(mock.TextTurn("via package Run"))
	var streamed string
	res, err := Run(context.Background(), "hi",
		WithProvider(p),
		WithOnEvent(func(e llm.Event) {
			if e.Type == llm.EventText {
				streamed += e.Text
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalText != "via package Run" {
		t.Fatalf("FinalText = %q", res.FinalText)
	}
	if streamed != "via package Run" {
		t.Fatalf("streamed = %q", streamed)
	}
}

func TestNewRequiresProvider(t *testing.T) {
	if _, err := New(); err == nil {
		t.Fatal("expected error when no provider configured")
	}
}
