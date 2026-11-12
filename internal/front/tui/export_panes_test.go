package tui

import "strings"

// The main screen on a wide terminal is two panes, a rule between: these
// read a frame (its text, without colors) pane by pane, for the tests here
// and in tui_test.

// rule is the rule between the panes, with its spaces.
const rule = " │ "

// LeftPane is the left pane of each line of f, the table and the newest
// stops and starts; a line with no rule, as the header, is all left.
func LeftPane(f string) []string {
	var out []string
	for _, l := range strings.Split(f, "\n") {
		if i := strings.Index(l, rule); i >= 0 {
			l = l[:i]
		}
		out = append(out, l)
	}
	return out
}

// RightPane is the right pane of each line of f that has the rule: the
// selected distribution.
func RightPane(f string) []string {
	var out []string
	for _, l := range strings.Split(f, "\n") {
		if i := strings.Index(l, rule); i >= 0 {
			out = append(out, l[i+len(rule):])
		}
	}
	return out
}

// PaneText is the right pane's words in one line, one space between, so a
// sentence the pane wrapped reads whole.
func PaneText(f string) string {
	return strings.Join(strings.Fields(strings.Join(RightPane(f), " ")), " ")
}

// TableRow is the table's row of the distribution name in f, from the left
// pane: the row that starts with its square and its name.
func TableRow(f, name string) string {
	for _, l := range LeftPane(f) {
		t := strings.TrimLeft(l, " ")
		if strings.HasPrefix(t, "▪ "+name+" ") {
			return l
		}
	}
	return ""
}
