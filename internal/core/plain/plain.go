// Package plain makes what a distribution says safe to show: a terminal
// reads some characters as commands, and text read from a distribution (a
// hook's file name, a container's name) may hold them.
package plain

import (
	"strings"
	"unicode"
)

// Text is s with each character that a terminal obeys or that reorders what
// is shown turned into a question mark: control characters (C0, DEL and
// C1, ESC among them, which start every escape sequence), the line and
// paragraph separators, and the marks that set the direction of text. Bytes
// that are no UTF-8 are no character: each run of them is one question
// mark. Anything else, as it is. A line break and a tab are such characters
// too: Text is for one value on one line.
func Text(s string) string { return clean(s, false) }

// Lines is Text for text of several lines, an error's: line breaks and
// tabs stay, as a terminal only moves on for them.
func Lines(s string) string { return clean(s, true) }

// clean is Text, or Lines when lines is set.
func clean(s string, lines bool) string {
	// A lone byte that is no UTF-8 is passed over by the check below, which
	// reads it as the replacement character, yet a terminal that reads
	// 8-bit controls takes 0x9B for CSI: such bytes go first.
	s = strings.ToValidUTF8(s, "?")
	bad := func(r rune) bool { return unsafe(r) && !(lines && (r == '\n' || r == '\t')) }
	// Most text has none: nothing is copied for it.
	if !strings.ContainsFunc(s, bad) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if bad(r) {
			return '?'
		}
		return r
	}, s)
}

// unsafe reports whether r is one of those characters.
func unsafe(r rune) bool {
	switch {
	case unicode.IsControl(r):
		return true
	// The line and paragraph separators: no control to Go, but a line break
	// to a terminal or a viewer that honours them.
	case r == 0x2028, r == 0x2029:
		return true
	// The marks that set the direction of text: the Arabic letter mark,
	// the left-to-right and right-to-left marks, the embeddings and
	// overrides, and the isolates.
	case r == 0x061C, r == 0x200E, r == 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}
