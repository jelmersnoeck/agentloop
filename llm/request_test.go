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
