package truncate

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHeadByLines(t *testing.T) {
	in := "a\nb\nc\nd\ne\n"
	out, trunc := Head(in, 3, 1_000_000)
	if !trunc {
		t.Fatal("expected truncated")
	}
	if out != "a\nb\nc\n" {
		t.Fatalf("Head = %q, want first 3 lines", out)
	}
}

func TestHeadByBytes(t *testing.T) {
	in := strings.Repeat("x", 100)
	out, trunc := Head(in, 1_000_000, 10)
	if !trunc || len(out) != 10 {
		t.Fatalf("Head byte-cut = %q (trunc=%v), want 10 bytes", out, trunc)
	}
}

func TestHeadNoTruncation(t *testing.T) {
	in := "short\n"
	out, trunc := Head(in, 2000, 50000)
	if trunc || out != in {
		t.Fatalf("Head should not truncate: %q trunc=%v", out, trunc)
	}
}

func TestTailByLines(t *testing.T) {
	in := "a\nb\nc\nd\ne\n"
	out, trunc := Tail(in, 2, 1_000_000)
	if !trunc {
		t.Fatal("expected truncated")
	}
	// Last two non-empty lines are d and e.
	if !strings.Contains(out, "d") || !strings.Contains(out, "e") || strings.Contains(out, "a") {
		t.Fatalf("Tail = %q, want last lines d,e", out)
	}
}

func TestTailByBytes(t *testing.T) {
	in := "0123456789abcdef"
	out, trunc := Tail(in, 1_000_000, 6)
	if !trunc || out != "abcdef" {
		t.Fatalf("Tail byte-cut = %q (trunc=%v), want last 6 bytes", out, trunc)
	}
}

func TestHeadByteCutIsRuneSafe(t *testing.T) {
	in := strings.Repeat("世", 10)        // 3 bytes each, 30 bytes total
	out, trunc := Head(in, 1_000_000, 8) // 8 bytes = 2 full runes + 2 partial bytes
	if !trunc {
		t.Fatal("expected truncated")
	}
	if !utf8.ValidString(out) {
		t.Fatalf("Head produced invalid UTF-8: %q", out)
	}
	if out != "世世" {
		t.Fatalf("Head = %q, want 世世 (partial rune dropped)", out)
	}
}

func TestTailByteCutIsRuneSafe(t *testing.T) {
	in := strings.Repeat("世", 10)
	out, trunc := Tail(in, 1_000_000, 8)
	if !trunc {
		t.Fatal("expected truncated")
	}
	if !utf8.ValidString(out) {
		t.Fatalf("Tail produced invalid UTF-8: %q", out)
	}
	if out != "世世" {
		t.Fatalf("Tail = %q, want 世世 (partial rune dropped)", out)
	}
}

func TestLineCap(t *testing.T) {
	long := strings.Repeat("y", 600)
	in := "ok\n" + long + "\n"
	out := Line(in, 500)
	firstKept := strings.Split(out, "\n")[0]
	if firstKept != "ok" {
		t.Fatalf("short line changed: %q", firstKept)
	}
	if !strings.Contains(out, "... [truncated]") {
		t.Fatal("expected per-line truncation marker")
	}
	// The long line must be capped near 500 chars plus the marker.
	longKept := strings.Split(out, "\n")[1]
	if len(longKept) > 500+len("... [truncated]") {
		t.Fatalf("long line not capped: len=%d", len(longKept))
	}
}

func TestSpillWritesFile(t *testing.T) {
	path, err := Spill("agentloop-test", "full content here")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "full content here" {
		t.Fatalf("spilled content = %q", string(data))
	}
}

func TestApplyTruncatesAndSpills(t *testing.T) {
	// 3000 lines exceeds DefaultMaxLines (2000) → truncated + spilled.
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString("line\n")
	}
	out, err := Apply(b.String(), false) // keepTail=false → Head
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Full output:") {
		t.Fatalf("expected spill marker, got tail: %q", out[len(out)-80:])
	}
	// Extract the path and confirm the full content was written.
	marker := out[strings.Index(out, "Full output:")+len("Full output:"):]
	path := strings.TrimSpace(strings.Trim(marker, "]"))
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable at %q: %v", path, err)
	}
	if strings.Count(string(data), "line") != 3000 {
		t.Fatalf("spill file missing lines: %d", strings.Count(string(data), "line"))
	}
}

func TestApplyNoTruncationNoSpill(t *testing.T) {
	out, err := Apply("small output\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Full output:") {
		t.Fatalf("small output should not spill: %q", out)
	}
	if out != "small output\n" {
		t.Fatalf("small output altered: %q", out)
	}
}
