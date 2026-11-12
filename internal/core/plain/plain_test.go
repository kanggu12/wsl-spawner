package plain

import "testing"

func TestTextKeepsWhatATerminalDoesNotObeyAndTurnsTheRestIntoQuestionMarks(t *testing.T) {
	for in, want := range map[string]string{
		// Text, spaces, punctuation and other scripts stay as they are.
		"10-backup db (daily)": "10-backup db (daily)",
		"näme ✓ 名前":            "näme ✓ 名前",
		"":                     "",
		// ESC starts an escape sequence, BEL ends one, CR rewrites a line,
		// a tab and a line break break the fields and lines.
		"10\x1b]52;c;Zm9v\x07ok": "10?]52;c;Zm9v?ok",
		"a\rb\tc\nd":             "a?b?c?d",
		// DEL and a C1 control, which some terminals read as CSI.
		"x\x7fy\u009bz": "x?y?z",
		// The marks that reorder what is shown, a right-to-left override.
		"gpj.exe\u202ecod": "gpj.exe?cod",
		"a\u2066b\u2069c":  "a?b?c",
		// Bytes that are no UTF-8: a lone 0x9B is CSI to a terminal that
		// reads 8-bit controls, and a file name may hold any byte. Each run
		// of them is one question mark.
		"\x9b2J":         "?2J",
		"ok\xff\xfe end": "ok? end",
		// The line and paragraph separators, which break a line where a
		// terminal or a viewer honours them.
		"a\u2028b\u2029c": "a?b?c",
	} {
		if got := Text(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestLinesKeepsLineBreaksAndTabsOfAnError(t *testing.T) {
	// An error of several lines keeps its lines; what a terminal obeys goes.
	if got, want := Lines("sh: 1: boom\n\tnext\r\x1b[2Jline"), "sh: 1: boom\n\tnext??[2Jline"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	// Bytes that are no UTF-8 and the line separator go there too: only a
	// line break and a tab move a terminal on.
	if got, want := Lines("a\x9b[2J\nb c"), "a?[2J\nb?c"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}
