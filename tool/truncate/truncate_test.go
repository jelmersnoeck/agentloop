package truncate

import (
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
