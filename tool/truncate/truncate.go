// Package truncate provides direction-aware output caps and temp-file spillover
// for tool results, so a single oversized result can never blow the context
// window. Tools call these at their own boundary (bash keeps the tail, file
// reads keep the head, greps cap each line).
package truncate

import "strings"

// Default caps: whichever limit is hit first wins.
const (
	DefaultMaxLines     = 2000
	DefaultMaxBytes     = 50000
	DefaultMaxLineChars = 500
)

// Head keeps the first maxLines lines and at most maxBytes bytes, whichever is
// more restrictive, reporting whether anything was dropped.
func Head(s string, maxLines, maxBytes int) (string, bool) {
	truncated := false

	lines := strings.SplitAfter(s, "\n")
	// SplitAfter on a trailing "\n" yields a final empty element; drop it.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	out := strings.Join(lines, "")

	if len(out) > maxBytes {
		out = out[:maxBytes]
		truncated = true
	}
	return out, truncated
}

// Tail keeps the last maxLines lines and at most maxBytes bytes, whichever is
// more restrictive.
func Tail(s string, maxLines, maxBytes int) (string, bool) {
	truncated := false

	lines := strings.SplitAfter(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
		truncated = true
	}
	out := strings.Join(lines, "")

	if len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
		truncated = true
	}
	return out, truncated
}
