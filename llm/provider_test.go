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
