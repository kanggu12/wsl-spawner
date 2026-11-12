package tui

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/setup"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/ui"
	"github.com/kanggu12/wsl-spawner/internal/version"
)

// spinner is the frames of the mark on a step that runs.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// The widths the details' columns are laid out by. The containers stand
// beside the other sections where those keep minLeft cells and the
// containers' names minName; the newest stops and starts join them, under
// the containers, where the right column has room for them whole, and at
// least minRecent cells, beside the other sections at their own width.
const (
	minLeft   = 44
	minName   = 16
	minRecent = 64
	// maxLeft is the most the other sections take beside the containers:
	// a longer line wraps there, and the containers keep the room.
	maxLeft = 60
)

// View renders the whole screen.
func (m model) View() tea.View {
	var body string
	switch {
	// Below this there is no room: say so, in one line.
	case m.width < 40 || m.height < 12:
		body = m.frame([]string{ui.Fg(m.th.Muted).Render(i18n.T("small"))})
	case m.mode == modeHelp:
		body = m.helpView()
	case m.mode == modeHistory:
		body = m.historyView()
	case m.mode == modeCheck:
		body = m.checkView()
	case m.mode == modeRun:
		body = m.runView()
	default:
		body = m.mainView()
		if m.ask != nil {
			body = m.overlay(body, m.askBox(*m.ask))
		}
	}
	// The marks a console's font may not have, swapped once all is laid out.
	v := tea.NewView(ui.Marks(body))
	v.AltScreen = true
	v.WindowTitle = "wsl-spawner"
	return v
}

// frame pads or cuts every line to exactly the screen's width and the block
// to its height, so nothing wraps and nothing is left over.
func (m model) frame(lines []string) string {
	out := make([]string, m.height)
	for i := range out {
		if i < len(lines) {
			out[i] = ui.Fit(strings.ReplaceAll(lines[i], "\n", " "), m.width)
		} else {
			out[i] = strings.Repeat(" ", m.width)
		}
	}
	return strings.Join(out, "\n")
}

// page lays out a screen: the header, a blank row, the body, then the status
// line and the keys at the bottom.
func (m model) page(body []string, status, keys string) string {
	lines := append([]string{m.header(), ""}, body...)
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines[:m.height-2], status, keys)
	return m.frame(lines)
}

// bodyHeight is the rows between the header and the status line.
func (m model) bodyHeight() int { return m.height - 4 }

// turn is how far the logo's ring has turned: it turns while something
// runs or is read, and stands still otherwise, so motion always means work.
func (m model) turn() int {
	if m.loading || m.checking || (m.mode == modeRun && m.run != nil && !m.run.done) {
		return m.spin
	}
	return 0
}

// header is the top bar: the logo's chip, the name and this build's
// version, WSL's on the left; the sign-out hook and what is kept running on
// the right.
func (m model) header() string {
	t := m.th
	left := t.Chip("wsl-spawner", m.turn()) + " " + ui.Fg(t.Ink).Render(version.Short())
	wslv := ""
	if m.st.WSL != "" {
		// The dot between is quieter than the words it separates.
		wslv = ui.Fg(t.Faint).Render("  "+ui.Sep()+"  ") + ui.Fg(t.Muted).Render("WSL "+m.st.WSL)
	}
	var right []string
	if m.hook != nil {
		// The hook leads with its square in its own color: the tool's when it
		// is in and last, worth a look otherwise.
		cat, key := ui.CatWarn, "header.nohook"
		switch {
		case m.hook.Last:
			cat, key = ui.CatClean, "header.hook"
		case m.hook.Installed:
			key = "header.hooknotlast"
		}
		right = append(right, t.Dot(cat)+" "+ui.Fg(t.Cat[cat]).Render(i18n.T(key)))
	}
	if n := m.kept(); n > 0 {
		right = append(right, ui.Fg(t.Muted).Render(i18n.TN("header.kept", n, n)))
	}
	if n := m.atLogon(); n > 0 {
		right = append(right, ui.Fg(t.Muted).Render(i18n.TN("header.logon", n, n)))
	}
	// fit lays out a left side and right pieces, if they fit with a gap.
	fit := func(l string, r []string) (string, bool) {
		rs := strings.Join(r, ui.Fg(t.Faint).Render("  "+ui.Sep()+"  "))
		gap := m.width - lipgloss.Width(l) - lipgloss.Width(rs) - 2
		if gap < 2 {
			return "", false
		}
		return " " + l + strings.Repeat(" ", gap) + rs + " ", true
	}
	// What does not fit goes: what is kept running, then WSL's version;
	// the hook stays longest.
	for n := len(right); n > 0; n-- {
		if line, ok := fit(left+wslv, right[:n]); ok {
			return line
		}
	}
	if len(right) > 0 {
		if line, ok := fit(left, right[:1]); ok {
			return line
		}
	}
	return " " + left + wslv
}

// atLogon counts the distributions started at sign-in.
func (m model) atLogon() int {
	n := 0
	for _, d := range m.st.List {
		if d.AtLogon {
			n++
		}
	}
	return n
}

// kept counts the distributions kept running.
func (m model) kept() int {
	n := 0
	for _, d := range m.st.List {
		if d.Probe != nil && d.Probe.KeptRunning {
			n++
		}
	}
	return n
}

// mainView is the distributions: the table across the screen, the selected
// one under it, and the newest stops and starts in the rows left.
func (m model) mainView() string {
	t := m.th
	keys := m.footer()
	h := m.bodyHeight()
	switch {
	case !m.loaded:
		return m.page(m.centered(h, append(m.logoBlock(), "", ui.Fg(t.Muted).Render(i18n.T("loading")))...), "", keys)
	case m.err != nil:
		// The one line that matters, wrapped, not cut: its lines after
		// the first under its words, all of them a block in the middle.
		w := min(m.width-8, max(48, lipgloss.Width(m.logoBlock()[0])))
		lines := append(m.logoBlock(), "")
		lines = append(lines, block(m.wrapHangTo("✗ ", "  ", errWords(m.err), w, ui.Fg(t.Cat[ui.CatCut])))...)
		return m.page(m.centered(h, lines...), "", keys)
	case len(m.st.List) == 0:
		lines := append(m.logoBlock(), "", ui.Fg(t.Muted).Render(i18n.T("empty")))
		return m.page(m.centered(h, lines...), "", keys)
	}
	// Wide: two panes, the distributions and their newest stops and starts
	// on the left, the selected one on the right, a rule between, as
	// wsl-shrinker lays out its disks: the screen is full, and the eye goes
	// from the list to the one chosen.
	if m.width >= wideMin {
		return m.page(m.panes(h), m.statusLine(), keys)
	}
	// The table takes what it needs, up to half the rows when the
	// distributions are many: the selected one's row stays among those
	// shown, and its details in sight under it.
	body := m.table(m.width, max(4, h/2))
	// recentIn is set when the details hold the newest stops and starts in
	// their right column, on a wide screen.
	recentIn := false
	if rest := h - len(body) - 2; rest >= 3 {
		body = append(body, "")
		// Three cells in, under the table's names; a blank row kept above
		// the status line.
		var lines []string
		lines, recentIn = m.details(m.width-4, rest)
		for _, l := range lines {
			body = append(body, "   "+l)
		}
	}
	// The recent runs in what is left, a blank row kept above the status
	// line.
	if rest := h - len(body) - 2; rest >= 2 && !recentIn {
		for _, l := range append([]string{""}, m.recent(m.width-4, rest)...) {
			body = append(body, "   "+l)
		}
	}
	return m.page(body, m.statusLine(), keys)
}

// wideMin is the width from which the main screen is two panes: under it
// the selected distribution goes under the table, as on a narrow terminal.
const wideMin = 112

// leftMin and leftShare bound the left pane: the table at its own width, at
// least leftMin cells, at most leftShare of the screen, so the right pane
// keeps room for a step's bar.
const (
	leftMin   = 52
	leftShare = 0.58
)

// panes is the main screen on a wide terminal in h rows: on the left the
// table and, under it, the newest stops and starts; a rule; on the right
// the selected distribution, with its last stop step by step.
func (m model) panes(h int) []string {
	t := m.th
	// The table at its own width, within its bounds: one cell of margin on
	// the left is the page's, three cells for the rule and its spaces.
	// The table whole, the last stops with their times, where the left
	// pane has room for it; else each last stop as its word alone, its
	// time in the pane beside.
	wide := m
	room := int(float64(m.width) * leftShare)
	// First the tools' names in short, then the last stops' times, until
	// the table fits the room.
	if wide.tableWidth() > room {
		wide.shortOwner = true
	}
	if wide.tableWidth() > room {
		wide.wordOnly = true
	}
	leftW := min(max(wide.tableWidth(), leftMin), room)
	rightW := m.width - leftW - 3 - 2
	left := wide.table(leftW, max(4, h/2))
	// The recent runs fill the rest of the left pane, a blank row above:
	// three cells in, under the table's names, each run whole, on more
	// lines where it is long.
	if rest := h - len(left) - 1; rest >= 2 {
		left = append(left, "")
		for _, l := range m.recentWrapped(leftW-3, rest) {
			left = append(left, "   "+l)
		}
	}
	right := m.pane(rightW, h)
	// The rule runs the body's height, quieter than any text.
	rule := ui.Fg(t.Rule).Render("│")
	out := make([]string, h)
	for i := range out {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = ui.Fit(l, leftW) + " " + rule + " " + r
	}
	return out
}

// recentWrapped is the newest stops and starts in w cells and h rows,
// under a title, newest first, each whole: one too long goes on under its
// first line. A run that would not fit whole is left out, with those after.
func (m model) recentWrapped(w, h int) []string {
	t := m.th
	if h < 2 {
		return nil
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(t.Muted).Render(i18n.T("recent.title"))}
	if len(m.hist) == 0 {
		return append(lines, ui.Fit(ui.Fg(t.Muted).Render(i18n.T("history.none")), w))
	}
	for i := len(m.hist) - 1; i >= 0; i-- {
		e := m.entryLines(m.hist[i], "01-02 15:04", w)
		if len(lines)+len(e) > h {
			break
		}
		lines = append(lines, e...)
	}
	return lines
}

// tableWidth is how wide the table is with every column whole: the left
// pane's width when the screen leaves it.
func (m model) tableWidth() int {
	w := 0
	for _, l := range m.table(1<<12, len(m.st.List)+1) {
		// The selected row is lit across the width it is given: the others
		// say how wide the cells are.
		w = max(w, lipgloss.Width(strings.TrimRight(ansi.Strip(l), " ")))
	}
	return w + 1
}

// pane is the selected distribution in w cells and h rows, beside the
// table: what it is, a line that says what matters most, then its last
// stop step by step, what its next stop does, its containers and its
// choices. What does not fit goes from the end.
func (m model) pane(w, h int) []string {
	t := m.th
	d, ok := m.selected()
	if !ok || h < 1 {
		return nil
	}
	bold := lipgloss.NewStyle().Bold(true).Foreground(t.Ink)
	lines := []string{ui.Fit(bold.Render(d.Name)+ui.Fg(t.Muted).Render(m.facts(d)), w)}
	text, st := m.verdict(d)
	lines = append(lines, m.wrapTo(text, w, st)...)
	if !ours(d) {
		// Not wsl-spawner's: the verdict says whose it is, and that is all.
		return cut(lines, h)
	}
	p := d.Probe
	switch {
	case p == nil && d.Running && d.ProbeError != "":
		lines = append(lines, ui.Fg(t.Muted).Render(ui.Fit(d.ProbeError, w)))
	case p != nil && !p.Complete && d.ProbeError != "":
		lines = append(lines, ui.Fg(t.Cat[ui.CatWarn]).Render(ui.Fit(d.ProbeError, w)))
	}
	var secs []section
	if _, _, inHistory := m.lastStopIn(d.Name); p != nil || inHistory {
		secs = append(secs, section{title: i18n.T("detail.laststop"), rows: m.lastStopRows(d, w)})
	}
	if p != nil {
		rows, fill := m.nextStopLines(*p, w)
		secs = append(secs, section{title: i18n.T("detail.nextstop"), rows: rows, fill: fill, whole: true})
		if len(p.Containers) > 0 {
			secs = append(secs, section{title: i18n.T("detail.containers"), rows: m.containerRows(*p, w), list: true})
		}
	}
	if _, known := m.stateOf(d); known {
		secs = append(secs, section{title: i18n.T("detail.choices"), rows: m.choiceChips(d, w), whole: true})
	}
	// Its stops, where the screen is tall enough for them after the rest:
	// what fills a tall terminal is its own record.
	if rows := m.stopsRows(d.Name, w, 8); len(rows) > 1 {
		// The bars' scale in the title: the time a stop has.
		secs = append(secs, section{title: i18n.T("detail.stops") + " " + ui.Sep() + " " + i18n.T("detail.stops.of", m.st.StopLimit), rows: rows, list: true})
	}
	if len(secs) == 0 || h-len(lines) < 3 {
		return cut(lines, h)
	}
	lines = append(lines, "")
	return cut(append(lines, m.stack(secs, w, h-len(lines))...), h)
}

// lastStopRows are how a distribution's last start ended, then, where
// wsl-spawner's stop ended it, that stop's steps each with a bar of its
// time, then what came back at its next start.
func (m model) lastStopRows(d api.Distro, w int) []string {
	rows := m.lastStopLines(d, w)
	how := ""
	if d.Probe != nil {
		how = d.Probe.LastEnd.How
	}
	// The steps belong to the stop the line above names: one that ended
	// the last start (clean, out of time, a step failed), or, said from the
	// history, the newest. A start cut short has no stop to show.
	_, r, ok := m.lastStopIn(d.Name)
	if !ok || (!fromHistory(how) && endCat(how) == ui.CatCut) {
		return rows
	}
	steps := m.stepRows(r, w)
	if len(steps) == 0 {
		return rows
	}
	// After the line that says how it ended, before what came back.
	n := len(m.endLines(d, w))
	out := append(append(append([]string{}, rows[:n]...), steps...), rows[n:]...)
	return out
}

// endLines are the lines lastStopLines gives to how the last start ended,
// without what came back after it.
func (m model) endLines(d api.Distro, w int) []string {
	if d.Probe == nil || len(d.Probe.Resumed) == 0 {
		return m.lastStopLines(d, w)
	}
	all := m.lastStopLines(d, w)
	back := wrapPieces("  ↻ ", "    ", w, m.backPieces(*d.Probe)...)
	return all[:max(0, len(all)-len(back))]
}

// stepRows are the steps of a stop, as the history keeps them, in w cells:
// a mark for how each ended, its name, a bar of its time against the
// longest step's, and its seconds. A pane too narrow for a bar has none.
func (m model) stepRows(r history.Distro, w int) []string {
	t := m.th
	if len(r.Steps) == 0 {
		return nil
	}
	names := make([]string, len(r.Steps))
	secs := make([]string, len(r.Steps))
	nameW, secW, longest := 0, 0, 0.0
	for i, s := range r.Steps {
		names[i] = historyStepWords(s)
		secs[i] = elapsed(time.Duration(s.Took * float64(time.Second)))
		nameW = max(nameW, lipgloss.Width(names[i]))
		secW = max(secW, lipgloss.Width(secs[i]))
		longest = max(longest, s.Took)
	}
	// Four cells in, under the result's words; the mark and a space; the
	// name; two spaces; the bar; two spaces; the seconds, right-aligned.
	// The names first, whole where they fit: the bar takes what is left.
	nameW = min(nameW, max(12, w-6-2-2-secW))
	barW := min(24, w-6-nameW-2-2-secW)
	var out []string
	for i, s := range r.Steps {
		color, mark := t.Cat[ui.CatClean], "✓"
		switch {
		case s.Code == 124:
			// Ran out of its time.
			color, mark = t.Cat[ui.CatWarn], "!"
		case s.Code != 0:
			color, mark = t.Cat[ui.CatCut], "✗"
		}
		row := "    " + ui.Fg(color).Render(mark) + " " + ui.Fit(ui.Fg(t.Ink).Render(names[i]), nameW)
		if barW >= 6 {
			// A step that took no measurable time keeps its track.
			row += "  " + t.Bar(barW, int64(longest*1000), ui.Part{Value: int64(s.Took * 1000), Color: color})
		}
		row += "  " + ui.Fg(t.Muted).Render(ui.FitRight(secs[i], secW))
		out = append(out, row)
	}
	return out
}

// historyStepWords names a stop's step from the history as the run screen
// named it while it ran: a hook by its file, the containers and units by
// their count; a failed one with its exit code.
func historyStepWords(s history.Step) string {
	words := ""
	switch {
	case s.Step == "units" && s.Count > 0:
		words = i18n.TN("step.units", s.Count, s.Count)
	case (s.Step == "docker" || s.Step == "podman") && s.Count > 0:
		words = i18n.TN("step.containers", s.Count, s.Step, s.Count)
	default:
		// The hook by its file, the others by name, the exit code with them.
		return stepName(s)
	}
	if s.Code != 124 && s.Code != 0 {
		words += i18n.T("stepname.exit", s.Code)
	}
	return words
}

// stopsRows are a distribution's newest stops in the history, newest
// first, at most n, each in w cells: when, a bar of how long it took
// against the stop limit, the seconds, and how it ended in its color, so
// stops that grow toward the limit show at a glance.
func (m model) stopsRows(name string, w, n int) []string {
	t := m.th
	limit := m.st.StopLimit
	if limit <= 0 {
		return nil
	}
	// row is one stop of it: when, how long, how it ended.
	type row struct {
		at   string
		took int64
		cat  ui.Category
		word string
	}
	var rows []row
	for i := len(m.hist) - 1; i >= 0 && len(rows) < n; i-- {
		e := m.hist[i]
		if e.What != "stop" {
			continue
		}
		for _, r := range e.Distros {
			if r.Name == name {
				rows = append(rows, row{e.At.Local().Format("01-02 15:04"), int64(math.Round(r.Took)), endCat(r.Result), i18n.T("end." + r.Result)})
			}
		}
	}
	// One width for every bar, from the widest word, so the bars share
	// their scale: stop_limit, the time each stop had.
	words := 0
	for _, r := range rows {
		words = max(words, lipgloss.Width(r.word))
	}
	barW := min(24, w-2-11-2-2-6-2-words)
	var out []string
	for _, r := range rows {
		line := "  " + ui.Fg(t.Muted).Render(r.at) + "  "
		if barW >= 8 {
			line += t.Bar(barW, int64(limit), ui.Part{Value: r.took, Color: t.Cat[r.cat]}) + "  "
		}
		out = append(out, line+ui.Fg(t.Muted).Render(ui.FitRight(i18n.T("secs", int(r.took)), 6))+"  "+ui.Fg(t.Cat[r.cat]).Render(r.word))
	}
	return out
}

// choiceChips are a distribution's three choices as chips: one on is lit,
// with its check; one off is quiet, with an empty circle. One line where
// they fit, else one under another.
func (m model) choiceChips(d api.Distro, w int) []string {
	t := m.th
	s, _ := m.stateOf(d)
	on := lipgloss.NewStyle().Background(t.Key)
	chip := func(set bool, key string) string {
		if set {
			return on.Foreground(t.Mark).Render(" ✓") + on.Foreground(t.Ink).Render(" "+i18n.T(key)+" ")
		}
		return ui.Fg(t.Faint).Render(" ○ ") + ui.Fg(t.Muted).Render(i18n.T(key)+" ")
	}
	chips := []string{chip(s.Managed, "choicebox.setup"), chip(s.Keep, "choicebox.keep"), chip(s.Logon, "choicebox.logon")}
	if line := "  " + strings.Join(chips, " "); lipgloss.Width(line) <= w {
		return []string{line}
	}
	// Short of room for the chips: the same as a checklist on one line, two
	// cells between.
	mark := func(on bool, key string) string {
		if on {
			return ui.Fg(t.Mark).Render("✓") + " " + ui.Fg(t.Ink).Render(i18n.T(key))
		}
		return ui.Fg(t.Faint).Render("○") + " " + ui.Fg(t.Muted).Render(i18n.T(key))
	}
	if line := "  " + mark(s.Managed, "choicebox.setup") + "  " + mark(s.Keep, "choicebox.keep") + "  " + mark(s.Logon, "choicebox.logon"); lipgloss.Width(line) <= w {
		return []string{line}
	}
	for i := range chips {
		chips[i] = "  " + chips[i]
	}
	return chips
}

// centered places lines in the middle of h rows.
func (m model) centered(h int, lines ...string) []string {
	out := make([]string, 0, h)
	for len(out) < (h-len(lines))/2 {
		out = append(out, "")
	}
	for _, l := range lines {
		out = append(out, lipgloss.PlaceHorizontal(m.width, lipgloss.Center, l))
	}
	return out
}

// logoBlock is the logo as the screen draws it: the mark, and beside it the
// name with this build's version over the line that says what it does,
// wrapped to two lines where one does not fit. Its lines are padded to one
// width, the block's own, so placed in the middle the mark stays where it
// is and the block stands in the middle of the screen.
func (m model) logoBlock() []string {
	t := m.th
	mark := t.Logo()
	name := lipgloss.NewStyle().Foreground(t.Ink).Bold(true).Render("wsl-spawner") + "  " + ui.Fg(t.Muted).Render(version.Short())
	room := max(10, m.width-lipgloss.Width(mark[0])-8)
	// The name and the line under it stand in the mark's middle rows; the
	// line's second half, where it needs one, in the row below.
	lines := make([]string, len(mark))
	copy(lines, mark)
	mid := (len(mark) - 1) / 2
	lines[mid] += "   " + name
	// One line where it fits; else two of about the same length, not a
	// long one over a word or two.
	words := i18n.T("logo.tagline")
	if w := lipgloss.Width(words); w > room {
		room = min(room, w/2+6)
	}
	tag := m.wrapTo(words, room, ui.Fg(t.Muted))
	for i := 0; i < len(tag) && mid+1+i < len(lines); i++ {
		lines[mid+1+i] += "   " + ui.Fit(tag[i], min(room, lipgloss.Width(tag[i])))
	}
	return block(lines)
}

// block pads lines to the widest's width, so placed in the middle they
// stay aligned on the left as one block.
func block(lines []string) []string {
	w := 0
	for _, l := range lines {
		w = max(w, lipgloss.Width(l))
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l + strings.Repeat(" ", w-lipgloss.Width(l))
	}
	return out
}

// column is one column of the table: its heading, each row's cell, and
// how early it goes when the table is too wide (lower first; 0 stays).
type column struct {
	head  string
	cells []string
	drop  int
}

// table is the distributions, one row each in w cells: a square in the
// state's color, the name, the state, how far wsl-spawner goes with it, its
// containers and how its last start ended. The selected row is lit. At most
// rows lines: past that, the rows around the selected one, and a line that
// says how many more there are above and below.
func (m model) table(w, rows int) []string {
	t := m.th
	cols := []column{
		{head: i18n.T("col.distro")}, {head: i18n.T("col.state")}, {head: i18n.T("col.kind"), drop: 3},
		{head: i18n.T("col.containers"), drop: 1}, {head: i18n.T("col.laststop"), drop: 2},
	}
	for _, d := range m.st.List {
		row := m.rowCells(d)
		for i := range cols {
			cols[i].cells = append(cols[i].cells, row[i])
		}
	}
	// lead is where a row's cells start: a space, the square and a space.
	const lead = 3
	// The widest cell sets each column's width.
	widths := func(cs []column) (int, []int) {
		total, ws := lead, make([]int, len(cs))
		for i, c := range cs {
			ws[i] = lipgloss.Width(c.head)
			for _, cell := range c.cells {
				ws[i] = max(ws[i], lipgloss.Width(cell))
			}
			total += ws[i]
			if i > 0 {
				total += 2
			}
		}
		return total, ws
	}
	// Columns go, the least needed first, until the table fits; the name
	// and the state never go.
	for {
		total, _ := widths(cols)
		if total <= w {
			break
		}
		worst := -1
		for i, c := range cols {
			if c.drop > 0 && (worst < 0 || c.drop < cols[worst].drop) {
				worst = i
			}
		}
		if worst < 0 {
			break
		}
		cols = append(cols[:worst:worst], cols[worst+1:]...)
	}
	_, ws := widths(cols)
	line := func(mark string, cells []string) string {
		var b strings.Builder
		b.WriteString(" " + mark + " ")
		for i, c := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(ui.Fit(c, ws[i]))
		}
		return b.String()
	}
	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = ui.Fg(t.Muted).Render(strings.ToUpper(c.head))
	}
	out := []string{line(" ", heads)}
	// The rows shown: all that fit under the heading, else as many as leave
	// a line for the rest, the selected one in their middle where it can be.
	n := len(m.st.List)
	first, shown := 0, n
	if n+1 > rows {
		shown = max(1, rows-2)
		first = min(max(0, m.sel-shown/2), n-shown)
	}
	for r := first; r < first+shown; r++ {
		d := m.st.List[r]
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = c.cells[r]
		}
		row := line(t.Dot(m.category(d)), cells)
		if r == m.sel {
			// The selected row: its own background across the table.
			row = ui.Highlight(row, t.Selected, w)
		}
		out = append(out, row)
	}
	if shown < n {
		out = append(out, "   "+ui.Fg(t.Muted).Render(moreRows(first, n-first-shown)))
	}
	return out
}

// moreRows says how many rows of the table are out of sight, above and
// below the ones shown.
func moreRows(above, below int) string {
	switch {
	case above == 0:
		return i18n.T("table.more.below", below)
	case below == 0:
		return i18n.T("table.more.above", above)
	}
	return i18n.T("table.more.both", above, below)
}

// category is what a distribution's square says: running, stopped, or
// left to WSL or another tool.
func (m model) category(d api.Distro) ui.Category {
	switch {
	case !ours(d):
		return ui.CatAside
	case d.Running:
		return ui.CatRunning
	}
	return ui.CatStopped
}

// rowCells are a distribution's cells: name, state, how far wsl-spawner
// goes with it, containers, last stop.
func (m model) rowCells(d api.Distro) []string {
	t := m.th
	state := ui.Fg(t.Cat[ui.CatStopped]).Render(i18n.T("state.stopped"))
	if d.Running {
		state = ui.Fg(t.Cat[ui.CatRunning]).Render(i18n.T("state.running"))
	}
	cells := []string{lipgloss.NewStyle().Foreground(t.Ink).Bold(d.Default).Render(d.Name), state, m.modeCell(d)}
	if !ours(d) {
		return append(cells, "", "")
	}
	containers := ui.Fg(t.Faint).Render("–")
	if d.Probe != nil {
		containers = m.containerWords(*d.Probe)
	}
	end := m.endWords(d, false)
	if m.wordOnly {
		// The word in its color; the time is in the pane beside.
		end = render(m.endPieces(d, false)[:1])
	}
	return append(cells, containers, end)
}

// modeCell says how far wsl-spawner goes with a distribution: three squares
// as setup chooses them and a word for the farthest, or who it is left to.
// One stopped and never read is a dash: what is set up in it shows once it
// runs.
func (m model) modeCell(d api.Distro) string {
	t := m.th
	switch d.Kind {
	case "skipped":
		return ui.Fg(t.Cat[ui.CatAside]).Render(i18n.T("kind.skipped"))
	case "owned":
		if m.shortOwner {
			// The tool's name alone, where the table shares the screen.
			return ui.Fg(t.Cat[ui.CatAside]).Render(i18n.T("kind.owned.short", d.Owner))
		}
		return ui.Fg(t.Cat[ui.CatAside]).Render(i18n.T("kind.owned", d.Owner))
	case "wsl1":
		return ui.Fg(t.Cat[ui.CatAside]).Render(i18n.T("kind.wsl1"))
	}
	lv, known := m.level(d)
	if !known {
		return ui.Fg(t.Faint).Render("–")
	}
	word := ui.Fg(t.Ink).Render(i18n.T(fmt.Sprintf("level.%d", lv)))
	if lv == 0 {
		// Stopped with its services' time and nothing more: a quieter word.
		word = ui.Fg(t.Muted).Render(i18n.T("level.0"))
	}
	return t.Level(lv) + " " + word
}

// containerWords counts the running containers of all.
func (m model) containerWords(p api.Probe) string {
	t := m.th
	// An engine that does not answer is worth a look, before all. One
	// asleep behind its socket, or another's daemon, is not: said quietly
	// where no engine of the distribution's own runs.
	quiet, up := "", false
	for _, e := range []string{"docker", "podman"} {
		isUp, found := p.Engines[e]
		up = up || isUp
		switch p.EngineStates[e] {
		case "asleep":
			quiet = i18n.T("engine.asleep", e)
		case "foreign":
			if quiet == "" {
				quiet = i18n.T("engine.foreign", e)
			}
		default:
			if found && !isUp {
				return ui.Fg(t.Cat[ui.CatWarn]).Render(i18n.T("engine.down", e))
			}
		}
	}
	switch {
	case !up && quiet != "":
		return ui.Fg(t.Muted).Render(quiet)
	case !up:
		return ui.Fg(t.Faint).Render("–")
	}
	running := 0
	for _, c := range p.Containers {
		if c.State == "running" {
			running++
		}
	}
	return m.containerMeter(running, len(p.Containers)) + " " + fmt.Sprintf("%d/%d", running, len(p.Containers))
}

// containerMeter is a row's containers as squares: those running in their
// color, the rest as track, five squares at most for any count, so the
// column reads as a meter down the table.
func (m model) containerMeter(running, all int) string {
	t := m.th
	if all == 0 {
		return ""
	}
	n := min(all, 5)
	lit := int(math.Round(float64(running) / float64(all) * float64(n)))
	if running > 0 && lit == 0 {
		lit = 1
	}
	return ui.Fg(t.Cat[ui.CatRunning]).Render(strings.Repeat(ui.Block, lit)) + ui.Fg(t.Track).Render(strings.Repeat(ui.Block, n-lit))
}

// lastStopIn is a distribution's newest stop in the history, and how it went
// for it.
func (m model) lastStopIn(name string) (history.Entry, history.Distro, bool) {
	for i := len(m.hist) - 1; i >= 0; i-- {
		e := m.hist[i]
		if e.What != "stop" {
			continue
		}
		for _, d := range e.Distros {
			if d.Name == name {
				return e, d, true
			}
		}
	}
	return history.Entry{}, history.Distro{}, false
}

// endCat is the color of how a start ended.
func endCat(how string) ui.Category {
	switch how {
	case "clean":
		return ui.CatClean
	case "limit", "failed":
		return ui.CatWarn
	case "cut", "error":
		return ui.CatCut
	}
	return ui.CatStopped
}

// endWords says how a distribution's last start ended, in its color, with
// when; long says it in the details' words. What is noted inside it comes
// first; where nothing is, its newest stop in the history says how
// wsl-spawner's own stop of it went.
func (m model) endWords(d api.Distro, long bool) string { return render(m.endPieces(d, long)) }

// endPieces are endWords' words, in their colors, for a row that wraps.
func (m model) endPieces(d api.Distro, long bool) []piece {
	t := m.th
	how := ""
	if d.Probe != nil {
		how = d.Probe.LastEnd.How
	}
	// when are the pieces of when and how long, each kept on one line, and
	// who asked, which may break.
	when := func(at time.Time, took float64, by string) []piece {
		out := []piece{{" " + at.Local().Format("01-02 15:04"), ui.Fg(t.Muted), true}}
		if long {
			// Who asked stays whole: on the next line where the line is
			// full, not a word of it alone there.
			out = append(out, piece{" " + ui.Sep() + " " + i18n.T("secs", int(math.Round(took))), ui.Fg(t.Muted), true},
				piece{" " + ui.Sep() + " " + m.byFor(by), ui.Fg(t.Muted), true})
		}
		return out
	}
	if fromHistory(how) {
		if e, r, ok := m.lastStopIn(d.Name); ok {
			return append([]piece{{i18n.T("end." + r.Result), ui.Fg(t.Cat[endCat(r.Result)]), false}}, when(e.At, r.Took, e.By)...)
		}
	}
	if d.Probe == nil {
		return []piece{{"–", ui.Fg(t.Faint), false}}
	}
	p := *d.Probe
	key := map[string]string{"unknown": "end.unknown", "first": "end.first", "clean": "end.clean", "limit": "end.limit",
		"failed": "end.failed", "cut": "end.cut"}[p.LastEnd.How]
	if key == "" {
		key = "end.unknown"
	}
	// Not noted is quiet; how it ended is in the color of how it went.
	out := []piece{{i18n.T(key), ui.Fg(t.Cat[endCat(p.LastEnd.How)]), false}}
	if p.LastEnd.How == "unknown" || p.LastEnd.How == "first" || p.LastEnd.How == "" {
		out[0].style = ui.Fg(t.Muted)
	}
	switch p.LastEnd.How {
	case "clean", "limit", "failed":
		if p.LastStop != nil {
			out = append(out, when(p.LastStop.At, p.LastStop.Took, p.LastStop.By)...)
		}
	case "cut":
		// What ended it, and when, where its journal and Windows' logs
		// tell it; else when the start it ended began.
		if c := p.LastEnd; causeKinds[c.Cause] && c.CauseAt != nil {
			// Short in the table, whole in the details.
			words := "end.cause."
			if long {
				words = "end.because."
			}
			out = append(out, piece{": " + i18n.T(words+c.Cause), ui.Fg(t.Muted), false},
				piece{" " + c.CauseAt.Local().Format("01-02 15:04"), ui.Fg(t.Muted), true})
		} else if p.LastEnd.Start != nil {
			out = append(out, piece{" " + i18n.T("end.since", p.LastEnd.Start.Local().Format("01-02 15:04")), ui.Fg(t.Muted), false})
		}
	}
	return out
}

// causeKinds are the causes of a cut the screen has words for: one from a
// later program's JSON it does not know is not said.
var causeKinds = map[string]bool{"crash": true, "update": true, "shutdown": true, "signout": true, "outside": true}

// piece is a run of words in one style: what a row that wraps is made of.
// whole keeps it on one line, as a time is: a space before it is still
// where the line may break.
type piece struct {
	text  string
	style lipgloss.Style
	whole bool
}

// render is pieces on one line.
func render(ps []piece) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.style.Render(p.text))
	}
	return b.String()
}

// wrapPieces wraps pieces to w cells: the first line after lead, the rest
// after rest, so a row's second line stands under its words. Lines break
// only where a space is, never at a hyphen; a word longer than a line
// stands alone on it, for the caller to cut.
func wrapPieces(lead, rest string, w int, ps ...piece) []string {
	// word is a run of letters in one style, and whether a space comes
	// before it: only there may a line break.
	type word struct {
		text  string
		style lipgloss.Style
		space bool
	}
	var words []word
	space := false
	for _, p := range ps {
		if p.whole {
			// One word, but for the space it starts with.
			text := strings.TrimLeft(p.text, " ")
			words = append(words, word{text, p.style, space || text != p.text})
			space = false
			continue
		}
		for i, f := range strings.Split(p.text, " ") {
			if i > 0 {
				// A space was here, between two runs of letters.
				space = true
			}
			if f == "" {
				continue
			}
			words = append(words, word{f, p.style, space})
			space = false
		}
	}
	var lines []string
	line, used, empty := lead, lipgloss.Width(lead), true
	for _, wd := range words {
		ww, gap := lipgloss.Width(wd.text), 0
		if wd.space && !empty {
			gap = 1
		}
		// Past the width at a space: the word starts the next line.
		if wd.space && !empty && used+gap+ww > w {
			lines = append(lines, line)
			line, used, gap = rest, lipgloss.Width(rest), 0
		}
		line += strings.Repeat(" ", gap) + wd.style.Render(wd.text)
		used += gap + ww
		empty = false
	}
	return append(lines, line)
}

// fromHistory reports whether a distribution's last stop is the history's
// to say, by how what is noted inside it says its last start ended: not at
// all, or as the first start noted since wsl-spawner was set up in it,
// which knows nothing of the stop before it.
func fromHistory(how string) bool { return how == "" || how == "unknown" || how == "first" }

// byWords says who asked for a stop or start.
// byFor says who asked, in the words the line has room for.
func (m model) byFor(by string) string {
	if m.shortBy {
		return byShort(by)
	}
	return byWords(by)
}

func byWords(by string) string {
	switch by {
	case "signout":
		return i18n.T("by.signout")
	case "logon":
		return i18n.T("by.logon")
	}
	return i18n.T("by.hand")
}

// byShort is who asked, in a word, for a column.
func byShort(by string) string {
	switch by {
	case "signout":
		return i18n.T("by.short.signout")
	case "logon":
		return i18n.T("by.short.logon")
	}
	return i18n.T("by.short.hand")
}

// section is one titled block of the details: its rows.
type section struct {
	title string
	rows  []string
	// fill is a last row drawn to the section's width, as a bar is; nil
	// for none.
	fill func(w int) string
	// list is set for a list that can lose rows to an "and N more".
	list bool
	// newest is set for rows newest first, as the recent stops and starts
	// are, which can lose their oldest as they are: nothing is said of them.
	newest bool
	// whole is set for a section that says one thing with all its rows, as
	// the choices and the next stop's steps with their bar do: it shows
	// whole, or not at all. Another shows as many rows as fit, one at
	// least: its first says the most, as how the last stop went.
	whole bool
}

// details is the selected distribution in w cells and h rows: what it is,
// a line that says what matters most about it, then its choices, how its
// last stop went, what its next stop does and its containers. Where there
// is room the containers stand in a column of their own beside the rest,
// and on a wide screen the newest stops and starts under them, which
// recentIn says. What does not fit goes from the end.
func (m model) details(w, h int) (lines []string, recentIn bool) {
	t := m.th
	d, ok := m.selected()
	if !ok || h < 1 {
		return nil, false
	}
	bold := lipgloss.NewStyle().Bold(true).Foreground(t.Ink)
	lines = []string{ui.Fit(bold.Render(d.Name)+ui.Fg(t.Muted).Render(m.facts(d)), w)}
	// The verdict wraps rather than lose its end.
	text, st := m.verdict(d)
	lines = append(lines, m.wrapTo(text, w, st)...)
	if !ours(d) {
		// Not wsl-spawner's: the verdict says whose it is, and that is all.
		return cut(lines, h), false
	}
	p := d.Probe
	switch {
	case p == nil && d.Running && d.ProbeError != "":
		lines = append(lines, ui.Fg(t.Muted).Render(ui.Fit(d.ProbeError, w)))
	case p != nil && !p.Complete && d.ProbeError != "":
		// Read in part: said in the color of what is worth a look, as
		// what follows may leave something out.
		lines = append(lines, ui.Fg(t.Cat[ui.CatWarn]).Render(ui.Fit(d.ProbeError, w)))
	}
	// side are the sections beside the containers, laid out in cw cells.
	side := func(cw int) []section {
		var out []section
		// The choices, where they are known: for one never read, the
		// verdict says they show once it runs, and a section would say it
		// again.
		if _, known := m.stateOf(d); known {
			out = append(out, section{title: i18n.T("detail.choices"), rows: m.choicesRows(d, cw), whole: true})
		}
		// Its last stop, where something says how it went: what runs in
		// it, or the history; for one stopped with neither, a dash says
		// nothing.
		if _, _, inHistory := m.lastStopIn(d.Name); p != nil || inHistory {
			out = append(out, section{title: i18n.T("detail.laststop"), rows: m.lastStopLines(d, cw)})
		}
		if p != nil {
			rows, fill := m.nextStopLines(*p, cw)
			out = append(out, section{title: i18n.T("detail.nextstop"), rows: rows, fill: fill, whole: true})
		}
		return out
	}
	// natural is how wide those sections are with nothing wrapped, up to
	// maxLeft; a bar takes the width it is given.
	natural := 0
	for _, sc := range side(1 << 12) {
		natural = max(natural, lipgloss.Width(sc.title))
		for _, r := range sc.rows {
			natural = max(natural, lipgloss.Width(r))
		}
	}
	natural = min(natural, maxLeft)
	var list *section
	if p != nil && len(p.Containers) > 0 {
		list = &section{title: i18n.T("detail.containers"), list: true}
	}
	room := h - len(lines) - 1
	// No room, or no section to show: the name and the verdict are all.
	if room < 2 || natural == 0 && list == nil {
		return cut(lines, h), false
	}
	lines = append(lines, "")
	// need is the width of the containers' rows with their state and words
	// on coming back whole, and their names at minName cells or less.
	need := 0
	if list != nil {
		need = m.containersWidth(*p, minName)
	}
	// recentNeed is as wide as the newest stops and starts that would show
	// there are, at least minRecent: they are not cut to stand beside.
	recentNeed := minRecent
	for i := len(m.hist) - 1; i >= 0 && i >= len(m.hist)-room; i-- {
		recentNeed = max(recentNeed, lipgloss.Width(m.entryLine(m.hist[i], "01-02 15:04")))
	}
	// Wide: the sections at their own width, and beside them the containers,
	// then the newest stops and starts, which a blank row then keeps from
	// the status line, as below the details.
	if rw := w - 3 - natural; rw >= max(need, recentNeed) && len(m.hist) > 0 {
		var right []section
		if list != nil {
			list.rows = m.containerRows(*p, rw)
			right = append(right, *list)
		}
		right = append(right, section{title: i18n.T("recent.title"), rows: m.recentRows(rw), newest: true})
		cols := beside(m.stack(side(natural), natural, room-1), natural, m.stack(right, rw, room-1))
		return append(lines, cols...), true
	}
	// Two columns: the containers beside the other sections, which wrap to
	// leave the containers their room, down to minLeft cells, or their own
	// width where that is less.
	if lw := min(natural, w-3-need); list != nil && lw >= min(minLeft, natural) {
		rw := w - 3 - lw
		list.rows = m.containerRows(*p, rw)
		return cut(append(lines, beside(m.stack(side(lw), lw, room), lw, m.stack([]section{*list}, rw, room))...), h), false
	}
	// One column: each section under the one before.
	all := side(w)
	if list != nil {
		list.rows = m.containerRows(*p, w)
		all = append(all, *list)
	}
	return cut(append(lines, m.stack(all, w, room)...), h), false
}

// beside sets two columns side by side, the left lw cells wide and three
// cells of space between.
func beside(left []string, lw int, right []string) []string {
	var out []string
	for i := 0; i < max(len(left), len(right)); i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, ui.Fit(l, lw)+"   "+r)
	}
	return out
}

// stack lays sections one under another in w cells and h rows, a blank row
// between: a list that does not fit says how many more it has, and what
// still does not fit goes from the end.
func (m model) stack(secs []section, w, h int) []string {
	t := m.th
	var out []string
	for i, sc := range secs {
		gap := 0
		if i > 0 {
			gap = 1
		}
		// What the section needs under its heading: one row, or, for one
		// that shows whole, all its rows and its bar. Short of that it
		// goes, and those after it: no heading over nothing, no bar cut
		// off. A list says the rows it has no room for as how many more;
		// the others' later rows go without a word.
		need := 1
		if sc.whole {
			need = len(sc.rows)
			if sc.fill != nil {
				need++
			}
		}
		if h-len(out)-gap-1 < max(1, need) {
			break
		}
		if gap > 0 {
			out = append(out, "")
		}
		out = append(out, ui.Fit(lipgloss.NewStyle().Bold(true).Foreground(t.Muted).Render(sc.title), w))
		rows := sc.rows
		if room := h - len(out); sc.list && room < len(rows) && room >= 1 {
			// The list gets the rows left, its last saying how many more.
			more := len(rows) - room + 1
			rows = append(rows[:room-1:room-1], ui.Fg(t.Muted).Render("  "+i18n.TN("detail.more", more, more)))
		}
		for _, r := range rows {
			out = append(out, ui.Fit(r, w))
		}
		// A bar, drawn to the section's width.
		if sc.fill != nil {
			out = append(out, ui.Fit(sc.fill(w), w))
		}
	}
	return cut(out, h)
}

// cut keeps the first h lines.
func cut(lines []string, h int) []string {
	if len(lines) > h {
		return lines[:max(0, h)]
	}
	return lines
}

// facts are the selected distribution's few facts after its name: the
// default one, and whether systemd runs in it.
func (m model) facts(d api.Distro) string {
	var parts []string
	if d.Default {
		parts = append(parts, i18n.T("fact.default"))
	}
	if p := d.Probe; p != nil && ours(d) {
		switch p.Systemd {
		case "running", "degraded":
			parts = append(parts, "systemd")
		case "":
			parts = append(parts, i18n.T("fact.nosystemd"))
		case "noanswer":
			parts = append(parts, i18n.T("fact.systemd.noanswer"))
		default:
			// Starting, stopping and the like: said as systemd says it.
			parts = append(parts, "systemd "+p.Systemd)
		}
		if len(p.Failed) > 0 {
			parts = append(parts, i18n.TN("fact.failed", len(p.Failed), len(p.Failed)))
		}
		// Its first start noted, where the last stop shows the history's
		// stop before it: said here, as the last stop no longer says it.
		if _, _, ok := m.lastStopIn(d.Name); ok && p.LastEnd.How == "first" {
			parts = append(parts, i18n.T("end.first"))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, " "+ui.Sep()+" ")
}

// verdict is the line that says what matters most about a distribution,
// and its color: a start that ended without its time first, then what a
// stop and a start do with it.
func (m model) verdict(d api.Distro) (string, lipgloss.Style) {
	t := m.th
	color := func(c ui.Category) lipgloss.Style { return ui.Fg(t.Cat[c]).Bold(true) }
	ink := lipgloss.NewStyle().Foreground(t.Ink).Bold(true)
	switch d.Kind {
	case "owned":
		return i18n.T("detail.owned", d.Owner), color(ui.CatAside)
	case "skipped":
		return i18n.T("detail.skipped"), color(ui.CatAside)
	case "wsl1":
		return i18n.T("detail.wsl1"), color(ui.CatAside)
	}
	if !d.Running {
		if d.AtLogon {
			return i18n.T("verdict.stopped.logon"), color(ui.CatStopped)
		}
		// Read before it stopped: its choices are known, and shown.
		if _, known := m.level(d); known {
			return i18n.T("verdict.stopped.known"), color(ui.CatStopped)
		}
		return i18n.T("verdict.stopped"), color(ui.CatStopped)
	}
	p := d.Probe
	if p == nil {
		return i18n.T("verdict.unread"), color(ui.CatWarn)
	}
	switch p.LastEnd.How {
	case "cut":
		return i18n.T("verdict.cut"), color(ui.CatCut)
	case "limit":
		return i18n.T("verdict.limit"), color(ui.CatWarn)
	case "failed":
		return i18n.T("verdict.failed"), color(ui.CatWarn)
	}
	// No systemd: its services end with WSL, and nothing can be set up.
	if p.Systemd == "" {
		return i18n.T("verdict.nosystemd"), color(ui.CatWarn)
	}
	if !p.SetUp {
		return i18n.T("verdict.stoponly"), ink
	}
	if n := m.comingBack(*p); n > 0 {
		return i18n.TN("verdict.back", n, n), color(ui.CatClean)
	}
	return i18n.T("verdict.setup"), color(ui.CatClean)
}

// comingBack counts the running containers that come back after a stop: by
// their restart policy, or by wsl-spawner where the settings bring back
// every running one.
func (m model) comingBack(p api.Probe) int {
	n := 0
	for _, c := range p.Containers {
		if m.comesBack(c, p) {
			n++
		}
	}
	return n
}

// comesBack reports whether a running container comes back after a stop:
// by its restart policy, or by wsl-spawner where it is set up and the
// settings bring back every running one. One its engine removes as it
// stops (--rm) comes back by nothing.
func (m model) comesBack(c api.Container, p api.Probe) bool {
	return c.State == "running" && !c.AutoRemove && (backsItself(c) || p.SetUp && m.st.Resume == "running")
}

// backsItself reports whether a container's restart policy brings it back.
func backsItself(c api.Container) bool { return c.Policy == "always" || c.Policy == "unless-stopped" }

// choicesRows are a distribution's three choices, each marked on or off, in
// a row of cw cells, or a row each where they do not fit in one.
func (m model) choicesRows(d api.Distro, cw int) []string {
	t := m.th
	s, _ := m.stateOf(d)
	// A checklist: ✓ for a choice on, an empty circle for one off, as the
	// wizard marks a choice not taken; the words name the choice, not a
	// state, so an off one does not read as done.
	mark := func(on bool, key string) string {
		if on {
			return ui.Fg(t.Mark).Render("✓") + " " + ui.Fg(t.Ink).Render(i18n.T(key))
		}
		return ui.Fg(t.Faint).Render("○") + " " + ui.Fg(t.Muted).Render(i18n.T(key))
	}
	marks := []string{mark(s.Managed, "choicebox.setup"), mark(s.Keep, "choicebox.keep"), mark(s.Logon, "choicebox.logon")}
	if line := "  " + strings.Join(marks, "   "); lipgloss.Width(line) <= cw {
		return []string{line}
	}
	// Narrow: one under another, each whole.
	for i := range marks {
		marks[i] = "  " + marks[i]
	}
	return marks
}

// lastStopLines are how a distribution's last start ended and what came
// back at its last start, wrapped to cw cells under their words.
func (m model) lastStopLines(d api.Distro, cw int) []string {
	t := m.th
	how := ""
	if d.Probe != nil {
		how = d.Probe.LastEnd.How
	}
	var mark string
	switch endCat(how) {
	case ui.CatClean:
		mark = ui.Fg(t.Cat[ui.CatClean]).Render("✓")
	case ui.CatWarn:
		mark = ui.Fg(t.Cat[ui.CatWarn]).Render("!")
	case ui.CatCut:
		mark = ui.Fg(t.Cat[ui.CatCut]).Render("✗")
	default:
		mark = ui.Fg(t.Faint).Render("·")
		if _, r, ok := m.lastStopIn(d.Name); ok && fromHistory(how) {
			// Said from the history: the mark of how that stop went.
			mark = map[string]string{"clean": ui.Fg(t.Cat[ui.CatClean]).Render("✓"), "limit": ui.Fg(t.Cat[ui.CatWarn]).Render("!"),
				"failed": ui.Fg(t.Cat[ui.CatWarn]).Render("!"), "error": ui.Fg(t.Cat[ui.CatCut]).Render("✗")}[r.Result]
		}
	}
	ps := m.endPieces(d, true)
	if lipgloss.Width(render(ps)) > cw-4 {
		// The line in fewer words where it does not fit whole: who asked
		// as the table of runs says it.
		short := m
		short.shortBy = true
		ps = short.endPieces(d, true)
	}
	out := wrapPieces("  "+mark+" ", "    ", cw, ps...)
	if p := d.Probe; p != nil && len(p.Resumed) > 0 {
		out = append(out, wrapPieces("  "+ui.Fg(t.Cat[ui.CatStarting]).Render("↻")+" ", "    ", cw, m.backPieces(*p)...)...)
	}
	return out
}

// longestStop is the longest stop timeout among a distribution's running
// containers: what its containers' step waits for at most.
func longestStop(p api.Probe) int {
	n := 0
	for _, c := range p.Containers {
		if c.State == "running" {
			n = max(n, c.StopSeconds)
		}
	}
	return n
}

// nextStopLines are the steps of the next stop of a distribution, each on
// its own line, wrapped to cw cells under its words, and the bar of its
// longest wait against the stop limit, drawn to the width it is given and
// ending with what it says, as a run's bar does: nil where no container
// waits.
func (m model) nextStopLines(p api.Probe, cw int) ([]string, func(int) string) {
	t := m.th
	var out []string
	for _, item := range stepItems(p) {
		out = append(out, wrapPieces("  "+ui.Fg(t.Faint).Render(ui.Block)+" ", "    ", cw, piece{item, ui.Fg(t.Ink), false})...)
	}
	longest, limit := longestStop(p), m.st.StopLimit
	if longest <= 0 || limit <= 0 {
		return out, nil
	}
	// A wait as long as the limit or longer runs out of time: worth a look.
	color := t.Mark
	if longest >= limit {
		color = t.Cat[ui.CatWarn]
	}
	label := ui.Fg(t.Muted).Render(i18n.T("run.of", longest, limit))
	return out, func(w int) string {
		// As wide as the section leaves beside the label, as a run's bar.
		bw := max(8, min(48, w-4-lipgloss.Width(label)))
		return "  " + t.Bar(bw, int64(limit), ui.Part{Value: int64(min(longest, limit)), Color: color}) + "  " + label
	}
}

// stepItems are the steps of a stop of a distribution as the screen lists
// them: up to three hooks by name, in the order they run, as the run then
// shows them; more as their count.
func stepItems(p api.Probe) []string {
	items := planItems(p)
	if n := len(p.Hooks); n > 0 && n <= 3 {
		// The hooks by name, for their count, the first item.
		named := make([]string, n)
		for i, h := range p.Hooks {
			named[i] = i18n.T("step.hook", h)
		}
		items = append(named, items[1:]...)
	}
	return items
}

// planItems are the steps of a stop of a distribution, from its probe.
func planItems(p api.Probe) []string {
	var parts []string
	if n := len(p.Hooks); n > 0 {
		parts = append(parts, i18n.TN("plan.hooks", n, n))
	}
	if n := len(p.StopUnits); n > 0 {
		parts = append(parts, i18n.TN("plan.units", n, n))
	}
	for _, e := range []string{"docker", "podman"} {
		if up, ok := p.Engines[e]; !ok || !up {
			continue
		}
		running, longest := 0, 0
		for _, c := range p.Containers {
			if c.Engine == e && c.State == "running" {
				running++
				longest = max(longest, c.StopSeconds)
			}
		}
		if running > 0 {
			parts = append(parts, i18n.TN("plan.containers", running, e, running, longest))
		}
	}
	return append(parts, i18n.T("plan.rest"))
}

// backPieces say what came back at the distribution's last start, and
// when, for a row that wraps.
func (m model) backPieces(p api.Probe) []piece {
	var parts []string
	for _, r := range p.Resumed {
		parts = append(parts, r.Name+" "+i18n.T("back."+r.What))
	}
	out := []piece{{strings.Join(parts, ", "), ui.Fg(m.th.Ink), false}}
	if p.ResumedAt != nil {
		// The time on one line, the dot before it with it.
		out = append(out, piece{" " + ui.Sep() + " " + p.ResumedAt.Local().Format("01-02 15:04"), ui.Fg(m.th.Muted), true})
	}
	return out
}

// imageW is the least room an image is shown in: a longer one keeps its
// start and its tag, docker.io/…/redis:7.
const imageW = 16

// containerCols are the widths of a distribution's containers' columns:
// the widest name, image, state and words on coming back.
func (m model) containerCols(p api.Probe) (nameW, imgW, stateW, backW int) {
	for _, c := range p.Containers {
		nameW = max(nameW, lipgloss.Width(c.Name))
		imgW = max(imgW, lipgloss.Width(c.Image))
		stateW = max(stateW, lipgloss.Width(containerState(c.State)))
		backW = max(backW, lipgloss.Width(m.backLine(c, p)))
	}
	return nameW, imgW, stateW, backW
}

// containersWidth is how wide a distribution's containers' rows are with
// their names at most names cells, without their images.
func (m model) containersWidth(p api.Probe, names int) int {
	nameW, _, stateW, backW := m.containerCols(p)
	// The square and its margin, the name, then the state.
	w := 4 + min(nameW, names) + 2 + stateW
	if backW > 0 {
		w += 2 + backW
	}
	return w
}

// containerRows are a distribution's containers, one row each in w cells:
// a square in its state's color, its name, image and state, and whether it
// comes back after a stop. The state and those words stay whole: the
// image goes first, then names are shortened in the middle, which keeps
// their start and the end that tells web-1 from web-2.
func (m model) containerRows(p api.Probe, w int) []string {
	t := m.th
	nameMax, imgMax, stateW, backW := m.containerCols(p)
	// rest is all but the name and the image.
	rest := 4 + 2 + stateW
	if backW > 0 {
		rest += 2 + backW
	}
	// The image, where every name stands whole beside it in imageW cells or
	// its own width; it takes the room left, up to its own width.
	image := imgMax > 0 && w-rest-2-min(imageW, imgMax) >= nameMax
	imgW := min(imgMax, w-rest-2-nameMax)
	nameW := w - rest
	if image {
		nameW -= 2 + imgW
	}
	nameW = max(4, min(nameW, nameMax))
	var out []string
	for _, c := range p.Containers {
		cat := ui.CatStopped
		if c.State == "running" {
			cat = ui.CatRunning
		}
		row := "  " + t.Dot(cat) + " " + ui.Middle(c.Name, nameW)
		if image {
			row += "  " + ui.Fg(t.Muted).Render(ui.Middle(c.Image, imgW))
		}
		row += "  " + ui.Fit(containerState(c.State), stateW)
		if back := m.backLine(c, p); back != "" {
			row += "  " + back
		}
		out = append(out, row)
	}
	return out
}

// backLine says whether a running container comes back after a stop: by
// its restart policy, by wsl-spawner where it is set up and the settings
// bring back every running one, or not at all, which the verdict above or
// the check says why.
func (m model) backLine(c api.Container, p api.Probe) string {
	t := m.th
	if c.State != "running" {
		return ""
	}
	switch {
	case c.AutoRemove:
		// Removed as it stops: nothing can start it again.
	case backsItself(c):
		return ui.Fg(t.Muted).Render(i18n.T("container.policy"))
	case p.SetUp && m.st.Resume == "running":
		return ui.Fg(t.Cat[ui.CatClean]).Render(i18n.T("container.wslspawner"))
	}
	return ui.Fg(t.Cat[ui.CatWarn]).Render(i18n.T("container.stays"))
}

// containerState is a container's state in the screen's language; one
// the catalog does not name is the engine's own word.
func containerState(s string) string {
	if i18n.Has(i18n.English, "cstate."+s) {
		return i18n.T("cstate." + s)
	}
	return s
}

// choiceWords says a distribution's choices, in a line.
func choiceWords(s setup.State) string {
	if s.Skip {
		return i18n.T("kind.skipped")
	}
	var parts []string
	if s.Managed {
		parts = append(parts, i18n.T("choice.setup"))
	} else {
		parts = append(parts, i18n.T("choice.notsetup"))
	}
	if s.Keep {
		parts = append(parts, i18n.T("choice.keep"))
	}
	if s.Logon {
		parts = append(parts, i18n.T("choice.logon"))
	}
	return strings.Join(parts, " "+ui.Sep()+" ")
}

// recent is the newest stops and starts in w cells and h rows, under a
// title: when, what, each distribution and how it went, and who asked.
func (m model) recent(w, h int) []string {
	t := m.th
	if h < 2 {
		return nil
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(t.Muted).Render(i18n.T("recent.title"))}
	// None yet, as on a new install: said, so the rows below are not a
	// blank that looks like something missing.
	if len(m.hist) == 0 {
		return append(lines, ui.Fit(lipgloss.NewStyle().Foreground(t.Muted).Render(i18n.T("history.none")), w))
	}
	return cut(append(lines, m.recentRows(w)...), h)
}

// recentRows are every stop and start of the history, newest first, each in
// w cells.
func (m model) recentRows(w int) []string {
	var out []string
	for i := len(m.hist) - 1; i >= 0; i-- {
		out = append(out, ui.Fit(m.entryLine(m.hist[i], "01-02 15:04"), w))
	}
	return out
}

// entryLine is one stop or start of the history: when, in layout, what, who
// asked, then each distribution and how it went, in its color.
func (m model) entryLine(e history.Entry, layout string) string {
	lead, results := m.entryParts(e, layout)
	return lead + render(results)
}

// entryLines is entryLine wrapped to w cells, the results that do not fit
// on lines of their own under the first's.
func (m model) entryLines(e history.Entry, layout string, w int) []string {
	lead, results := m.entryParts(e, layout)
	return wrapPieces(lead, strings.Repeat(" ", lipgloss.Width(lead)), w, results...)
}

// entryParts are a history entry's when, what and who, in columns, and
// each distribution with how it went, a piece each that stays on one line.
func (m model) entryParts(e history.Entry, layout string) (string, []piece) {
	t := m.th
	what := i18n.T("history." + e.What)
	if e.Shutdown {
		what = i18n.T("history.stopwsl")
	}
	var parts []piece
	for i, d := range e.Distros {
		word, cat := i18n.T("end."+d.Result), endCat(d.Result)
		// A stop out of time, or with a step failed: which step, but in a
		// list beside the run's own pane, which names it.
		if s, ok := d.Late(); ok && !m.noSteps {
			word += ": " + stepName(s)
		}
		if e.What == "start" {
			word, cat = i18n.T("result."+d.Result), ui.CatRunning
			if d.Result == "error" {
				word, cat = i18n.T("result.starterror"), ui.CatCut
			}
		}
		// A comma after each but the last, and a space before each but
		// the first: where a line may break.
		sep := ""
		if i > 0 {
			sep = " "
		}
		end := ""
		if i < len(e.Distros)-1 || e.Err != "" {
			end = ","
		}
		// The name whole; the result in its color, a long one broken at its
		// spaces where the line ends; the comma after it, with it.
		parts = append(parts, piece{sep + d.Name, lipgloss.NewStyle(), true}, piece{" " + word, ui.Fg(t.Cat[cat]), lipgloss.Width(word) <= 16})
		if end != "" {
			parts = append(parts, piece{end, lipgloss.NewStyle(), true})
		}
	}
	if e.Err != "" {
		parts = append(parts, piece{" " + e.Err, ui.Fg(t.Cat[ui.CatCut]), false})
	}
	// The words for what and who line up, whatever the language.
	whatW, byW := 0, 0
	for _, k := range []string{"history.stop", "history.start", "history.stopwsl"} {
		whatW = max(whatW, lipgloss.Width(i18n.T(k)))
	}
	for _, k := range []string{"by.short.signout", "by.short.logon", "by.short.hand"} {
		byW = max(byW, lipgloss.Width(i18n.T(k)))
	}
	lead := ui.Fg(t.Muted).Render(e.At.Local().Format(layout)) + "  " + ui.Fit(what, whatW) + "  " +
		ui.Fg(t.Muted).Render(ui.Fit(byShort(e.By), byW)) + "  "
	return lead, parts
}

// stepName names a stop's step for the history: a hook by its file, the
// others as the run screen does, with a failed one's exit code.
func stepName(s history.Step) string {
	name := i18n.T("stepname." + s.Step)
	if s.Step == "hook" {
		name = i18n.T("step.hook", s.Hook)
	}
	if s.Code != 124 && s.Code != 0 {
		name += i18n.T("stepname.exit", s.Code)
	}
	return name
}

// statusLine is the line above the keys: what just happened, or how many.
func (m model) statusLine() string {
	t := m.th
	if m.note != "" {
		return " " + ui.Fg(t.Cat[m.noteCat]).Render(m.note)
	}
	running := 0
	for _, d := range m.st.List {
		if d.Running {
			running++
		}
	}
	return " " + ui.Fg(t.Muted).Render(i18n.TN("status.distros", len(m.st.List), len(m.st.List))+" "+ui.Sep()+" "+i18n.TN("status.running", running, running))
}

// footer is the main screen's keys: those that do something to the
// selected distribution.
func (m model) footer() string {
	if m.ask != nil {
		return m.keyBar([][2]string{{"⏎", i18n.T("key.do")}, {"esc", i18n.T("key.cancel")}})
	}
	// Nothing to select yet, or nothing at all: reading again, the check,
	// help and quitting are what there is to do.
	switch {
	case !m.loaded:
		return m.keyBar([][2]string{{"q", i18n.T("key.quit")}})
	case m.err != nil || len(m.st.List) == 0:
		return m.keyBar([][2]string{{"r", i18n.T("key.reload")}, {"c", i18n.T("key.check")}, {"?", i18n.T("key.help")}, {"q", i18n.T("key.quit")}})
	}
	keys := [][2]string{{"↑↓", i18n.T("key.select")}}
	d, _ := m.selected()
	switch {
	case ours(d):
		// Stop for one running, start for one stopped; the choices for one
		// whose systemd is not known to be missing.
		if d.Running {
			keys = append(keys, [2]string{"s", i18n.T("key.stop")})
		} else {
			keys = append(keys, [2]string{"t", i18n.T("key.start")})
		}
		if p := d.Probe; p == nil || p.Systemd != "" {
			keys = append(keys, [2]string{"m", i18n.T("key.setup")}, [2]string{"k", i18n.T("key.keep")}, [2]string{"l", i18n.T("key.logon")})
		}
	case d.Kind == "skipped":
		// Left to WSL: making it wsl-spawner's again is all there is to do.
		keys = append(keys, [2]string{"x", i18n.T("key.noskip")})
	}
	return m.keyBar(append(keys, [2]string{"c", i18n.T("key.check")}, [2]string{"h", i18n.T("key.history")},
		[2]string{"?", i18n.T("key.help")}, [2]string{"q", i18n.T("key.quit")}))
}

// dropOrder is which keys leave the bar first when it does not fit; the help
// screen lists them all.
var dropOrder = []string{"↑↓", "l", "k", "m", "h", "t", "c", "esc"}

// keyBar joins keycaps to fit the width, dropping the least needed first.
func (m model) keyBar(keys [][2]string) string {
	render := func(keys [][2]string) string {
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = m.th.KeyCap(k[0], k[1])
		}
		return " " + strings.Join(parts, "  ")
	}
	for _, drop := range dropOrder {
		if lipgloss.Width(render(keys)) <= m.width {
			break
		}
		for i, k := range keys {
			if k[0] == drop {
				keys = append(keys[:i:i], keys[i+1:]...)
				break
			}
		}
	}
	return render(keys)
}

// overlay floats a box over the screen, which is dimmed behind it. The rows
// the box stands on are cleared from edge to edge, so no cut word of the
// screen is left beside it.
func (m model) overlay(screen, box string) string {
	rows := strings.Split(screen, "\n")
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	// Never over the header, the first row: the box is fitted to the rows
	// between it and the keys.
	x, y := max(0, (m.width-bw)/2), max(1, (m.height-bh)/2)
	// A row of space above and below the box, so nothing behind it touches
	// its border; the header stays, dimmed, where the box needs its row.
	top, end := max(1, y-1), min(len(rows)-1, y+bh+1)
	for i, r := range rows {
		switch {
		// The keys stay as they are: they are the box's own.
		case i == len(rows)-1:
		case i >= top && i < end:
			rows[i] = strings.Repeat(" ", m.width)
		default:
			rows[i] = ui.Fg(m.th.Rule).Render(ansi.Strip(r))
		}
	}
	out := lipgloss.NewCompositor(lipgloss.NewLayer(strings.Join(rows, "\n")), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
	return m.frame(strings.Split(out, "\n"))
}

// boxWidth is a dialog's width on this screen, its border and margins in.
func (m model) boxWidth() int { return min(72, m.width-4) }

// box draws a dialog: an opaque panel with a border in border's color
// around lines already wrapped to its width.
func (m model) box(border ui.Category, lines ...string) string {
	w := m.boxWidth()
	// Wrapped already: each line fits the panel, which only pads it.
	body := lipgloss.NewStyle().Width(w - 6).Foreground(m.th.Ink).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().BorderForeground(m.th.Cat[border]).Border(lipgloss.NormalBorder()).Padding(0, 2).Width(w).Render(body)
}

// boxPart is one part of a dialog's text, each item wrapped to the box's
// width. When the box has no room, a list gives up its last items first,
// down to its first, saying how many more; then the parts of rank 3 and 2
// go; last the text of rank 1 is cut short. Rank 0 always stays.
type boxPart struct {
	// items are the part's sentences or items, each as its wrapped lines;
	// heads marks those a list does not count, a name over its items.
	items [][]string
	heads map[int]bool
	rank  int
	list  bool
	// more is the line that says how many more a list has, in its style;
	// gone is how many it gave up.
	more func(n int) string
	gone int
	// short is the part in fewer rows, which stands in for it first.
	short *boxPart
}

// lines are a part's lines: its items, then how many more it has.
func (p boxPart) lines() []string {
	var out []string
	for _, it := range p.items {
		out = append(out, it...)
	}
	if p.gone > 0 {
		out = append(out, p.more(p.gone))
	}
	return out
}

// counted reports whether a list counts its item i: a name over items
// and a blank between groups are not counted.
func (p boxPart) counted(i int) bool { return !p.heads[i] }

// fitBox lays parts out, a blank row between each two, in at most h rows
// of w cells.
func fitBox(parts []boxPart, h, w int) []string {
	// layout is the parts with a blank row between each two.
	layout := func() []string {
		var out []string
		for i, p := range parts {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, p.lines()...)
		}
		return out
	}
	// A part said in fewer rows stands in for it first.
	for i := len(parts) - 1; i >= 0 && len(layout()) > h; i-- {
		if parts[i].short != nil {
			parts[i] = *parts[i].short
		}
	}
	// A list gives up its last items, one by one, to its first; a heading
	// or a blank left last goes with the item after it.
	for i := len(parts) - 1; i >= 0; i-- {
		p := &parts[i]
		for p.list && len(layout()) > h && p.firstItem() < len(p.items)-1 {
			if p.counted(len(p.items) - 1) {
				p.gone++
			}
			p.items = p.items[:len(p.items)-1]
			for len(p.items) > 1 && !p.counted(len(p.items)-1) {
				p.items = p.items[:len(p.items)-1]
			}
		}
	}
	// Then whole parts go, rank 3 before rank 2.
	for _, rank := range []int{3, 2} {
		for i := len(parts) - 1; i >= 0 && len(layout()) > h; i-- {
			if parts[i].rank == rank {
				parts = append(parts[:i:i], parts[i+1:]...)
			}
		}
	}
	// Last, the text of rank 1 is cut short from its end, its last line
	// kept ending in "…", a list's "and N more" kept after it.
	for i := len(parts) - 1; i >= 0 && len(layout()) > h; i-- {
		p := &parts[i]
		if p.rank != 1 {
			continue
		}
		var text []string
		for _, it := range p.items {
			text = append(text, it...)
		}
		keep := max(1, len(text)-(len(layout())-h))
		cut := append([]string{}, text[:keep]...)
		// The last line's words in their colors, without the stop or the
		// comma they ended on, then "…", within the width.
		last := cut[keep-1]
		n := min(lipgloss.Width(strings.TrimRight(ansi.Strip(last), " .:,")), w-1)
		cut[keep-1] = ansi.Truncate(last, n, "") + "…"
		p.items, p.heads = [][]string{cut}, nil
	}
	return layout()
}

// firstItem is the index of a list's first counted item, past the heads
// before it.
func (p boxPart) firstItem() int {
	for i := range p.items {
		if p.counted(i) {
			return i
		}
	}
	return len(p.items)
}

// askBox says what an action will do, before it is done, fitted to the
// rows between the header and the keys.
func (m model) askBox(a action) string {
	t := m.th
	bold := lipgloss.NewStyle().Bold(true).Foreground(t.Ink)
	muted := ui.Fg(t.Muted)
	// iw is the width the text has: the box's, less its border and margins.
	iw := m.boxWidth() - 6
	// text is a sentence wrapped to the box, in st.
	text := func(s string, st lipgloss.Style) []string { return m.wrapHangTo("", "", s, iw, st) }
	// one is a part of one sentence, of a rank.
	one := func(s string, st lipgloss.Style, rank int) boxPart {
		return boxPart{items: [][]string{text(s, st)}, rank: rank}
	}
	// list is a list whose rows say how many more it has, in their indent.
	list := func(rank int) boxPart {
		return boxPart{rank: rank, list: true, heads: map[int]bool{},
			more: func(n int) string { return muted.Render("  " + i18n.TN("detail.more", n, n)) }}
	}
	keys := boxPart{items: [][]string{{t.KeyCap("⏎", i18n.T("key.do")) + "  " + t.KeyCap("esc", i18n.T("key.cancel"))}}}
	var parts []boxPart
	switch a.kind {
	case "stop":
		if len(a.names) == 0 {
			// Nothing of wsl-spawner's runs: WSL's own stop is all there is,
			// said once.
			parts = append(parts, one(i18n.T("ask.stopwsl"), bold, 0), one(i18n.T("ask.stopwsl.none"), ui.Fg(t.Ink), 1))
			break
		}
		parts = append(parts, one(i18n.T("ask.stop", strings.Join(a.names, ", ")), bold, 0),
			one(i18n.T("ask.stop.limit", m.st.StopLimit), ui.Fg(t.Ink), 2))
		// The plan: each distribution's name, then its steps under it, their
		// second lines under their words. In fewer rows, a line for each, as
		// the stop command says it: the name, then its steps but WSL's end.
		plan, short := list(3), list(3)
		nameW := 0
		for _, n := range a.names {
			nameW = max(nameW, lipgloss.Width(n))
		}
		for _, n := range a.names {
			for _, d := range m.st.List {
				if d.Name != n || d.Probe == nil {
					continue
				}
				if len(plan.items) > 0 {
					// A blank row between two distributions.
					plan.heads[len(plan.items)] = true
					plan.items = append(plan.items, []string{""})
				}
				plan.heads[len(plan.items)] = true
				plan.items = append(plan.items, text(n, bold))
				steps := stepItems(*d.Probe)
				for _, s := range steps {
					plan.items = append(plan.items, m.wrapHangTo("  "+ui.Block+" ", "    ", s, iw, muted))
				}
				// The last step is WSL ending the rest, the same for each: said
				// only for one with no step of its own.
				own := steps[:len(steps)-1]
				if len(own) == 0 {
					own = steps
				}
				line := bold.Render(ui.Fit(n, nameW)) + "  " + muted.Render(strings.Join(own, " "+ui.Sep()+" "))
				short.items = append(short.items, []string{ui.Fit(line, iw)})
			}
		}
		if len(plan.items) > 0 {
			plan.short = &short
			parts = append(parts, plan)
		}
		if a.shutdown {
			// WSL's own stop after: said last, and kept longest.
			parts = append(parts, one(i18n.T("ask.shutdown"), ui.Fg(t.Ink), 1))
		}
	case "start":
		parts = append(parts, one(i18n.T("ask.start", a.names[0]), bold, 0), one(i18n.T("ask.start.back"), ui.Fg(t.Ink), 1))
	case "set":
		parts = append(parts, one(i18n.T("ask.set", a.names[0]), bold, 0))
		changes := list(1)
		for _, l := range changeLines(a.cur, a.want) {
			// The square in the tool's color, each change's second line
			// under its first word.
			changes.items = append(changes.items, m.wrapHangTo(ui.Fg(t.Mark).Render(ui.Block)+" ", "  ", l, iw, ui.Fg(t.Ink)))
		}
		parts = append(parts, changes)
		if setup.Inside(a.cur, a.want) {
			for _, d := range m.st.List {
				if d.Name == a.names[0] && !d.Running {
					parts = append(parts, one(i18n.T("ask.set.starts"), muted, 2))
				}
			}
		}
	}
	// The box stands between the header and the keys: its border takes two
	// of those rows.
	return m.box(ui.CatClean, fitBox(append(parts, keys), m.height-4, iw)...)
}

// changeLines says each change going from cur to w does.
func changeLines(cur, w setup.State) []string {
	var out []string
	switch {
	case w.Managed && !cur.Managed:
		out = append(out, i18n.T("change.setup"))
	case !w.Managed && cur.Managed:
		out = append(out, i18n.T("change.takeout"))
	}
	// Keeping it running ends with taking wsl-spawner out too: said either
	// way, as the row's choice turns off.
	switch {
	case w.Keep && !cur.Keep:
		out = append(out, i18n.T("change.keep"))
	case !w.Keep && cur.Keep:
		out = append(out, i18n.T("change.nokeep"))
	}
	switch {
	case w.Logon && !cur.Logon:
		out = append(out, i18n.T("change.logon"))
	case !w.Logon && cur.Logon:
		out = append(out, i18n.T("change.nologon"))
	}
	switch {
	case w.Skip && !cur.Skip:
		out = append(out, i18n.T("change.skip"))
	case !w.Skip && cur.Skip:
		out = append(out, i18n.T("change.noskip"))
	}
	return out
}

// runView is a stop or a start: each distribution's steps as they go, with
// a bar of its time against the stop limit, then how it went, what comes
// back at its next start, and WSL's own stop after them when asked.
func (m model) runView() string {
	t := m.th
	r := m.run
	bold := lipgloss.NewStyle().Bold(true).Foreground(t.Ink)
	title := runTitle(r)
	if len(r.act.names) > 0 && r.act.shutdown {
		// WSL's own stop after the distributions, quieter than their names.
		title += ui.Fg(t.Muted).Render(" " + ui.Sep() + " " + i18n.T("run.thenwsl"))
	}
	// The time it took, against the right edge: as each distribution said
	// at its end once done, as the clock runs until then.
	took := m.now.Sub(r.started)
	if r.done {
		took = r.ended.Sub(r.started)
		for _, name := range r.act.names {
			took = max(took, m.runUsed(name, r))
		}
	}
	clock := ui.Fg(t.Muted).Render(elapsed(took))
	w := m.width - 4
	// The title gives way to the clock, which stays at the right edge.
	shown := bold.Render(ui.Fit(title, max(1, w-lipgloss.Width(clock)-2)))
	head := []string{"  " + strings.TrimRight(shown, " ") + strings.Repeat(" ", max(2, w-lipgloss.Width(strings.TrimRight(shown, " "))-lipgloss.Width(clock))) + clock,
		// A rule under the title, quieter than any text.
		"  " + ui.Fg(t.Rule).Render(strings.Repeat("─", w)), ""}
	// cols is how many distributions stand side by side: two where there
	// are two or more and room for two blocks with their bars.
	cols := 1
	if len(r.act.names) >= 2 && w >= 2*minRunCol+3 {
		cols = 2
	}
	bw := w
	if cols == 2 {
		bw = (w - 3) / 2
	}
	// inner is a block's width after its edge and a space.
	inner := bw - 2
	// Each distribution's block: its name with how it went, its steps, its
	// bar and what comes back. Folded once, its steps go; folded twice, all
	// but its first line.
	type block struct {
		top, steps, rest []string
		fold             int
	}
	var blocks []block
	for _, name := range r.act.names {
		// The block's edge, in the color of how it goes: a card down the
		// screen, which its lines hang from.
		edge := ui.Fg(t.Cat[m.runColor(name, r)]).Render("▌") + " "
		top := bold.Render(ui.Fit(name, inner))
		res, ok := r.results[name]
		if ok {
			words := m.resultWords(name, res, r)
			top = bold.Render(name) + strings.Repeat(" ", max(2, inner-lipgloss.Width(name)-lipgloss.Width(words))) + words
		}
		b := block{top: []string{edge + top}}
		if r.act.kind == "stop" {
			// The steps' words, their bars and their times: the words up to
			// half the block, the bar what is left, the time eight cells.
			sw := min(stepWidth(r), max(8, inner/2))
			barW := min(24, inner-2-2-sw-2-2-8)
			scale := m.stepScale(r.steps[name], m.now)
			for _, s := range r.steps[name] {
				b.steps = append(b.steps, edge+"  "+m.stepLine(s, m.now, sw, barW, scale))
			}
			b.rest = append(b.rest, edge+"  "+m.runBar(name, r, inner-2))
			// While a step runs, what it does, under the bar: why a stop of
			// wsl-spawner's takes its time where WSL's takes seconds.
			if about := stepAbout(r.steps[name]); !ok && about != "" {
				for _, l := range wrapPieces("", "", inner-2, piece{text: about, style: ui.Fg(t.Muted)}) {
					b.rest = append(b.rest, edge+"  "+l)
				}
			}
			if next := m.nextStart(name); ok && next != "" {
				b.rest = append(b.rest, edge+"  "+ui.Fg(t.Muted).Render(ui.Fit(next, inner-2)))
			}
		} else if !ok {
			b.steps = append(b.steps, edge+"  "+ui.Fg(t.Cat[ui.CatStarting]).Render(spinner[m.spin%len(spinner)]+" "+i18n.T("run.waiting")))
		}
		blocks = append(blocks, b)
	}
	// tail is what comes after the distributions: WSL's own stop, an error.
	var tail []string
	// WSL's own stop, after every distribution.
	if r.act.shutdown {
		var mark, words string
		switch r.wsl {
		case "run":
			mark, words = ui.Fg(t.Cat[ui.CatStarting]).Render(spinner[m.spin%len(spinner)]), i18n.T("run.wsl.going")
		case "done":
			mark, words = ui.Fg(t.Cat[ui.CatClean]).Render("✓"), i18n.T("run.wsl.done")
		case "failed":
			mark, words = ui.Fg(t.Cat[ui.CatCut]).Render("✗"), i18n.T("run.wsl.failed")
		default:
			mark, words = ui.Fg(t.Faint).Render("·"), i18n.T("run.wsl.wait")
		}
		// Its edge as a block's: blue while it goes, then how it went.
		edgeCat := map[string]ui.Category{"run": ui.CatStarting, "done": ui.CatClean, "failed": ui.CatCut}[r.wsl]
		edge := ui.Fg(t.Faint).Render("▌") + " "
		if r.wsl != "" {
			edge = ui.Fg(t.Cat[edgeCat]).Render("▌") + " "
		}
		tail = append(tail, "  "+edge+bold.Render("WSL"), "  "+edge+"  "+mark+" "+words, "")
	}
	if r.done && r.err != nil {
		tail = append(tail, "  "+ui.Fg(t.Cat[ui.CatCut]).Render("✗ "+errWords(r.err)))
	}
	// layout is the title, the blocks as folded, a blank after each, and
	// the tail, without the blank rows at its end.
	lines := func(b block) []string {
		out := append([]string{}, b.top...)
		if b.fold < 1 {
			out = append(out, b.steps...)
		}
		if b.fold < 2 {
			out = append(out, b.rest...)
		}
		return out
	}
	layout := func() []string {
		body := append([]string{}, head...)
		for i := 0; i < len(blocks); i += cols {
			left := lines(blocks[i])
			if cols == 2 && i+1 < len(blocks) {
				// Two blocks side by side, three cells between.
				right := lines(blocks[i+1])
				for j := 0; j < max(len(left), len(right)); j++ {
					l, rr := "", ""
					if j < len(left) {
						l = left[j]
					}
					if j < len(right) {
						rr = right[j]
					}
					body = append(body, "  "+ui.Fit(l, bw)+"   "+rr)
				}
			} else {
				for _, l := range left {
					body = append(body, "  "+l)
				}
			}
			body = append(body, "")
		}
		body = append(body, tail...)
		for len(body) > 0 && body[len(body)-1] == "" {
			body = body[:len(body)-1]
		}
		return body
	}
	// Too tall: the distributions that have ended fold, the first first,
	// their steps first, then all but the line that says how they went, so
	// no step stands without its distribution's name.
	for fold := 1; fold <= 2; fold++ {
		for i, name := range r.act.names {
			if _, ended := r.results[name]; ended && len(layout()) > m.bodyHeight() {
				blocks[i].fold = fold
			}
		}
	}
	body := layout()
	if h := m.bodyHeight(); len(body) > h {
		// Still too tall: the title stays, and the newest lines under it.
		body = append(append([]string{}, head[0]), body[len(body)-(h-1):]...)
	}
	keys := m.keyBar([][2]string{{"ctrl+c", i18n.T("key.quit")}})
	status := " " + ui.Fg(t.Muted).Render(i18n.T("run.going"))
	if r.done {
		keys = m.keyBar([][2]string{{"⏎", i18n.T("key.back")}})
		status = ""
	}
	return m.page(body, status, keys)
}

// historyPanes is the history on a wide terminal: on the left the stops
// and starts, newest first, the one chosen lit, as many as fit around it;
// a rule; on the right what the chosen one did, each distribution with how
// it went and the steps of its stop, each with a bar of its time.
func (m model) historyPanes() string {
	t := m.th
	h := m.bodyHeight()
	n := len(m.hist)
	// scroll is the chosen run, the newest first: the keys that scroll the
	// list on a narrow screen choose a run here.
	sel := min(m.scroll, n-1)
	leftW := max(leftMin, int(float64(m.width)*leftShare))
	rightW := m.width - leftW - 3 - 2
	// The list says how each went in its word: which step, in the pane.
	list := m
	list.noSteps = true
	first := 0
	if n > h {
		first = min(max(0, sel-h/2), n-h)
	}
	var left []string
	for i := first; i < min(n, first+h); i++ {
		e := m.hist[n-1-i]
		row := " " + ui.Fit(list.entryLine(e, "01-02 15:04"), leftW-2)
		if i == sel {
			// The chosen run: the screen's selection, as the table's row.
			row = ui.Highlight(ui.Fg(t.Mark).Render("▌")+ui.Fit(list.entryLine(e, "01-02 15:04"), leftW-2), t.Selected, leftW)
		}
		left = append(left, row)
	}
	right := m.entryPane(m.hist[n-1-sel], rightW, h)
	rule := ui.Fg(t.Rule).Render("│")
	body := make([]string, h)
	for i := range body {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		body[i] = ui.Fit(l, leftW) + " " + rule + " " + r
	}
	status := " " + ui.Fg(t.Muted).Render(i18n.TN("history.count", n, n))
	keys := m.keyBar([][2]string{{"↑↓", i18n.T("key.select")}, {"esc", i18n.T("key.back")}})
	return m.page(body, status, keys)
}

// entryPane is a run of the history in w cells and h rows: what it was,
// when, who asked and how long it took; then each distribution with how it
// went, the steps of its stop with their bars, and what went wrong.
func (m model) entryPane(e history.Entry, w, h int) []string {
	t := m.th
	bold := lipgloss.NewStyle().Bold(true).Foreground(t.Ink)
	what := i18n.T("history." + e.What)
	if e.Shutdown {
		what = i18n.T("history.stopwsl")
	}
	sep := " " + ui.Sep() + " "
	lines := wrapPieces("", "", w, piece{what, bold, true},
		piece{sep + e.At.Local().Format("2006-01-02 15:04"), ui.Fg(t.Muted), true},
		piece{sep + byWords(e.By), ui.Fg(t.Muted), true},
		piece{sep + i18n.T("secs", int(math.Round(e.Took))), ui.Fg(t.Muted), true})
	if e.Version != "" {
		lines = append(lines, ui.Fg(t.Faint).Render(ui.Fit("wsl-spawner "+e.Version, w)))
	}
	for _, d := range e.Distros {
		lines = append(lines, "")
		word, cat := i18n.T("end."+d.Result), endCat(d.Result)
		if e.What == "start" {
			word, cat = i18n.T("result."+d.Result), ui.CatRunning
			if d.Result == "error" {
				word, cat = i18n.T("result.starterror"), ui.CatCut
			}
		}
		head := bold.Render(d.Name) + "  " + ui.Fg(t.Cat[cat]).Render(word)
		if d.Took > 0 {
			head += ui.Fg(t.Muted).Render(sep + i18n.T("secs", int(math.Round(d.Took))))
		}
		lines = append(lines, ui.Fit(head, w))
		lines = append(lines, m.stepRows(d, w)...)
		if d.Err != "" {
			lines = append(lines, m.wrapHangTo("    ", "    ", d.Err, w, ui.Fg(t.Cat[ui.CatCut]))...)
		}
	}
	if e.Err != "" {
		lines = append(lines, "")
		lines = append(lines, m.wrapHangTo("✗ ", "  ", e.Err, w, ui.Fg(t.Cat[ui.CatCut]))...)
	}
	return cut(lines, h)
}

// minRunCol is the least width of a run's block standing beside another.
const minRunCol = 56

// runTitle names a run: what it does while it goes; once it has ended, what
// it did, or, where something went wrong, what it was, as the lines under
// it say what.
func runTitle(r *runState) string {
	names := strings.Join(r.act.names, ", ")
	// wsl is WSL's stop alone, when nothing of wsl-spawner's ran.
	wsl, start := len(r.act.names) == 0, r.act.kind == "start"
	switch {
	case !r.done:
		// Under way.
		switch {
		case wsl:
			return i18n.T("run.stopwsl")
		case start:
			return i18n.T("run.starting", names)
		}
		return i18n.T("run.stopping", names)
	case r.err == nil && !slices.Contains(slices.Collect(maps.Values(r.results)), "error"):
		// Ended, and each distribution stopped or started.
		switch {
		case wsl:
			return i18n.T("run.stoppedwsl")
		case start:
			return i18n.T("run.started", names)
		}
		return i18n.T("run.stopped", names)
	}
	// Ended with an error, or with a distribution that did not stop or
	// start: what it was, and the lines under it say what went wrong.
	switch {
	case wsl:
		return i18n.T("run.stopofwsl")
	case start:
		return i18n.T("run.startof", names)
	}
	return i18n.T("run.stopof", names)
}

// runBar is a distribution's time in a stop against the stop limit: the
// steps done so far, and the one under way up to now.
func (m model) runBar(name string, r *runState, w int) string {
	t := m.th
	limit := time.Duration(m.st.StopLimit) * time.Second
	if limit <= 0 {
		return ""
	}
	used := m.runUsed(name, r)
	cat := ui.CatClean
	if res := r.results[name]; res == "limit" || res == "failed" {
		cat = ui.CatWarn
	}
	bw := max(8, min(48, w-24))
	label := i18n.T("run.of", int(used.Round(time.Second)/time.Second), m.st.StopLimit)
	return t.Bar(bw, int64(limit), ui.Part{Value: int64(min(used, limit)), Color: t.Cat[cat]}) + "  " + ui.Fg(t.Muted).Render(label)
}

// runUsed is the time a distribution's stop has taken: its steps done, the
// one under way up to now, or what it said at its end, if more.
func (m model) runUsed(name string, r *runState) time.Duration {
	var used time.Duration
	for _, s := range r.steps[name] {
		switch s.state {
		case "run":
			used += m.now.Sub(s.since)
		case "wait":
		default:
			used += s.took
		}
	}
	if d, ok := r.took[name]; ok && r.results[name] != "" {
		used = max(used, d)
	}
	return used
}

// nextStart says which containers of a distribution come back at its next
// start, from what ran in it when it was last read.
func (m model) nextStart(name string) string {
	p, ok := m.seen[name]
	if !ok {
		return ""
	}
	var names []string
	for _, c := range p.Containers {
		if m.comesBack(c, p) {
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return i18n.T("run.next", strings.Join(names, ", "))
}

// stepWidth is the width of the widest step's name in a run, so the
// times line up in one column.
func stepWidth(r *runState) int {
	w := 0
	for _, steps := range r.steps {
		for _, s := range steps {
			w = max(w, lipgloss.Width(stepWords(s)))
		}
	}
	return w
}

// stepLine is one step of a stop: its mark, what it is, and its time, the
// names width wide.
func (m model) stepLine(s step, now time.Time, width, barW int, scale time.Duration) string {
	t := m.th
	var mark string
	when := ""
	color := t.Cat[ui.CatClean]
	switch s.state {
	case "wait":
		mark = ui.Fg(t.Faint).Render("·")
	case "run":
		mark = ui.Fg(t.Cat[ui.CatStarting]).Render(spinner[m.spin%len(spinner)])
		when = elapsed(now.Sub(s.since))
		color = t.Cat[ui.CatStarting]
	case "done":
		mark = ui.Fg(t.Cat[ui.CatClean]).Render("✓")
		when = i18n.T("secs", int(s.took.Round(time.Second)/time.Second))
	case "late":
		mark = ui.Fg(t.Cat[ui.CatWarn]).Render("!")
		when = i18n.T("run.late", int(s.took.Round(time.Second)/time.Second))
		color = t.Cat[ui.CatWarn]
	default:
		mark = ui.Fg(t.Cat[ui.CatCut]).Render("✗")
		when = i18n.T("run.failed")
		color = t.Cat[ui.CatCut]
	}
	line := mark + " " + ui.Fit(stepWords(s), width)
	// The bar: the step's time against the block's longest, growing while
	// it runs; a step still to come has its track only.
	if barW >= 8 && scale > 0 {
		used := s.took
		if s.state == "run" {
			used = now.Sub(s.since)
		}
		line += "  " + t.Bar(barW, int64(scale), ui.Part{Value: int64(min(used, scale)), Color: color})
	}
	return line + "  " + ui.Fg(t.Muted).Render(when)
}

// stepScale is the longest of a distribution's steps so far, the one its
// steps' bars are drawn against: at least a second, so a quick stop's bars
// still say which step took the time.
func (m model) stepScale(steps []step, now time.Time) time.Duration {
	longest := time.Second
	for _, s := range steps {
		d := s.took
		if s.state == "run" {
			d = now.Sub(s.since)
		}
		longest = max(longest, d)
	}
	return longest
}

// runColor is a run's block's edge: blue while it goes, then the color of
// how it ended.
func (m model) runColor(name string, r *runState) ui.Category {
	res, ok := r.results[name]
	switch {
	case !ok:
		return ui.CatStarting
	case r.errs[name] != "" || res == "error":
		return ui.CatCut
	case res == "limit" || res == "failed":
		return ui.CatWarn
	case res == "busy":
		return ui.CatAside
	case res == "started" || res == "running":
		return ui.CatRunning
	}
	return ui.CatClean
}

// stepWords names a step.
func stepWords(s step) string {
	switch {
	case strings.HasPrefix(s.key, "hook:"):
		return i18n.T("step.hook", strings.TrimPrefix(s.key, "hook:"))
	case s.key == "units":
		return i18n.TN("step.units", s.count, s.count)
	case s.key == "docker" || s.key == "podman":
		return i18n.TN("step.containers", s.count, s.key, s.count)
	case s.key == "sync":
		return i18n.T("step.sync")
	case s.key == "record":
		// Said only when a file of the stop's own could not be written.
		return i18n.T("step.record")
	case s.key == "terminate":
		return i18n.T("step.terminate")
	}
	return s.key
}

// stepAbout says what the step running among steps does, or "" when none
// runs.
func stepAbout(steps []step) string {
	for _, s := range steps {
		if s.state != "run" {
			continue
		}
		// A hook's own name is on its row: what every hook is, here.
		key := s.key
		if strings.HasPrefix(key, "hook:") {
			key = "hook"
		}
		switch key {
		case "hook", "units", "docker", "podman", "sync", "terminate":
			return i18n.T("step.about." + key)
		}
	}
	return ""
}

// resultWords says how one distribution's stop or start went.
func (m model) resultWords(name, res string, r *runState) string {
	t := m.th
	// Another stop is stopping it: nothing went wrong, and that stop ends
	// it. Said quietly, before the error its result carries for scripts.
	if res == "busy" {
		return ui.Fg(t.Muted).Render(i18n.T("result.busy"))
	}
	if e := r.errs[name]; e != "" {
		return ui.Fg(t.Cat[ui.CatCut]).Render("✗ " + e)
	}
	switch res {
	case "clean":
		return ui.Fg(t.Cat[ui.CatClean]).Render("✓ " + i18n.T("result.clean"))
	case "limit":
		return ui.Fg(t.Cat[ui.CatWarn]).Render("! " + i18n.T("result.limit"))
	case "failed":
		return ui.Fg(t.Cat[ui.CatWarn]).Render("! " + i18n.T("result.failed"))
	case "started", "running":
		s := ui.Fg(t.Cat[ui.CatRunning]).Render("✓ " + i18n.T("result."+res))
		var back []string
		for _, b := range r.back[name] {
			back = append(back, b.Name+" "+i18n.T("back."+b.What))
		}
		if len(back) > 0 {
			s += ui.Fg(t.Muted).Render(" " + ui.Sep() + " " + strings.Join(back, ", "))
		}
		return s
	}
	return ui.Fg(t.Cat[ui.CatCut]).Render("✗ " + res)
}

// elapsed is a duration in seconds, as a step's time and the stop's limit
// are said beside it.
func elapsed(d time.Duration) string {
	return i18n.T("secs", int(d.Round(time.Second)/time.Second))
}

// scrolled is the lines from the scroll on, as many as the body holds,
// with the scroll kept within them.
func (m model) scrolled(lines []string) ([]string, int) {
	h := m.bodyHeight()
	top := min(m.scroll, max(0, len(lines)-h))
	end := min(len(lines), top+h)
	return lines[top:end], top
}

// listKeys are the keys of the check, history and help screens.
func (m model) listKeys(extra ...[2]string) string {
	keys := append([][2]string{{"↑↓", i18n.T("key.scroll")}}, extra...)
	return m.keyBar(append(keys, [2]string{"esc", i18n.T("key.back")}))
}

// checkView is the check's findings, group by group.
func (m model) checkView() string {
	t := m.th
	var lines []string
	switch {
	case m.checking && m.check == nil:
		lines = append(lines, "  "+ui.Fg(t.Muted).Render(spinner[m.spin%len(spinner)]+" "+i18n.T("check.reading")))
	case m.checkErr != nil:
		lines = append(lines, "  "+ui.Fg(t.Cat[ui.CatCut]).Render("✗ "+errWords(m.checkErr)))
	case m.check != nil:
		// The verdict first, in one line: what to look at, and how many
		// findings are fine.
		lines = append(lines, "  "+m.checkSummary(), "")
		if m.width >= wideMin {
			// Wide: the groups in two columns, a rule between, so the whole
			// check fits where a list would scroll.
			lines = append(lines, m.checkColumns()...)
			break
		}
		for i, g := range m.check.Groups {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, m.groupLines(g, m.width-1)...)
		}
	}
	shown, _ := m.scrolled(lines)
	status := " " + ui.Fg(t.Muted).Render(i18n.T("check.title"))
	return m.page(shown, status, m.listKeys([2]string{"r", i18n.T("key.reload")}))
}

// checkSummary is the check's verdict in a line: how many findings to look
// at, in the color of the worst, and how many are fine.
func (m model) checkSummary() string {
	t := m.th
	var look, fine int
	for _, g := range m.check.Groups {
		for _, f := range g.Items {
			switch f.Mark {
			case "warn", "bad":
				look++
			case "ok":
				fine++
			}
		}
	}
	verdict := findingMark(t, "ok") + " " + lipgloss.NewStyle().Bold(true).Foreground(t.Ink).Render(i18n.T("check.sum.ok"))
	if look > 0 {
		cat := ui.CatWarn
		if m.check.Worst == "bad" {
			cat = ui.CatCut
		}
		verdict = findingMark(t, m.check.Worst) + " " + lipgloss.NewStyle().Bold(true).Foreground(t.Cat[cat]).Render(i18n.TN("check.sum.look", look, look))
	}
	return verdict + ui.Fg(t.Muted).Render("  "+ui.Sep()+"  "+i18n.TN("check.sum.fine", fine, fine))
}

// groupLines are a group of the check in w cells: its name after a square
// in the color of its worst finding, then each finding, wrapped under its
// words, with what to do about it.
func (m model) groupLines(g api.CheckGroup, w int) []string {
	t := m.th
	name := g.Name
	if g.Key != "distro" {
		name = i18n.T("check.g." + g.Key)
	}
	// The group's worst finding colors its square: a glance finds the one
	// to look at.
	worst := ""
	for _, f := range g.Items {
		if rank(f.Mark) > rank(worst) {
			worst = f.Mark
		}
	}
	square := map[string]ui.Category{"ok": ui.CatClean, "warn": ui.CatWarn, "bad": ui.CatCut}
	sq := ui.Fg(t.Faint).Render(ui.Block)
	if c, ok := square[worst]; ok {
		sq = ui.Fg(t.Cat[c]).Render(ui.Block)
	}
	lines := []string{"  " + sq + " " + lipgloss.NewStyle().Bold(true).Foreground(t.Ink).Render(ui.Fit(name, max(1, w-4)))}
	for _, f := range g.Items {
		text := i18n.T("check."+f.Key, jsonArgs(f.Args)...)
		lines = append(lines, m.wrapHangTo("    "+findingMark(t, f.Mark)+" ", "      ", text, w, ui.Fg(t.Ink))...)
		if f.DoKey != "" {
			lines = append(lines, m.wrapHangTo("      → ", "        ", i18n.T("check."+f.DoKey, jsonArgs(f.DoArgs)...), w, ui.Fg(t.Muted))...)
		}
	}
	return lines
}

// rank orders a finding's marks, the worst highest.
func rank(mark string) int {
	return map[string]int{"info": 1, "ok": 2, "warn": 3, "bad": 4}[mark]
}

// checkColumns are the check's groups in two columns, in their order: the
// first ones on the left, the rest on the right, split where the taller
// column is shortest. A rule stands between them.
func (m model) checkColumns() []string {
	t := m.th
	colW := (m.width - 3) / 2
	var groups [][]string
	for _, g := range m.check.Groups {
		groups = append(groups, m.groupLines(g, colW-1))
	}
	// height is the lines of groups[a:b], a blank between each two.
	height := func(a, b int) int {
		n := 0
		for i := a; i < b; i++ {
			n += len(groups[i])
			if i > a {
				n++
			}
		}
		return n
	}
	split, best := len(groups), height(0, len(groups))
	for k := 1; k < len(groups); k++ {
		if h := max(height(0, k), height(k, len(groups))); h < best {
			split, best = k, h
		}
	}
	column := func(gs [][]string) []string {
		var out []string
		for i, g := range gs {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, g...)
		}
		return out
	}
	left, right := column(groups[:split]), column(groups[split:])
	sep := ui.Fg(t.Rule).Render(" │ ")
	var out []string
	for i := 0; i < max(len(left), len(right)); i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, ui.Fit(l, colW)+sep+r)
	}
	return out
}

// findingMark is a finding's mark in its color.
func findingMark(t ui.Theme, mark string) string {
	switch mark {
	case "ok":
		return ui.Fg(t.Cat[ui.CatClean]).Render("✓")
	case "warn":
		return ui.Fg(t.Cat[ui.CatWarn]).Render("!")
	case "bad":
		return ui.Fg(t.Cat[ui.CatCut]).Render("✗")
	}
	return ui.Fg(t.Faint).Render("·")
}

// jsonArgs are a finding's values as JSON gave them, with whole numbers as
// whole numbers again: JSON reads every number as a float.
func jsonArgs(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		if f, ok := a.(float64); ok && f == math.Trunc(f) {
			out[i] = int(f)
			continue
		}
		out[i] = a
	}
	return out
}

// hyphen stands in for "-" while text wraps: wrapping breaks a word at a
// hyphen, and a path or a command would lose its end to the next line.
const hyphen = "‑"

// wrapTo wraps s to w cells, each line in st, words whole.
func (m model) wrapTo(s string, w int, st lipgloss.Style) []string {
	wrapped := strings.Split(ansi.Wordwrap(strings.ReplaceAll(s, "-", hyphen), max(10, w), ""), "\n")
	for i, l := range wrapped {
		wrapped[i] = st.Render(strings.ReplaceAll(l, hyphen, "-"))
	}
	return wrapped
}

// wrapHang wraps s to the screen's width: the first line after first, the
// rest after rest.
func (m model) wrapHang(first, rest, s string, st lipgloss.Style) []string {
	// A cell short of the edge, as a terminal's last column wraps in some.
	return m.wrapHangTo(first, rest, s, m.width-1, st)
}

// wrapHangTo wraps s to w cells, the leads in: the first line after first,
// the rest after rest, so a list item's second line stands under its words.
func (m model) wrapHangTo(first, rest, s string, w int, st lipgloss.Style) []string {
	wrapped := m.wrapTo(s, w-lipgloss.Width(first), st)
	for i, l := range wrapped {
		lead := rest
		if i == 0 {
			lead = first
		}
		wrapped[i] = lead + l
	}
	return wrapped
}

// historyView is the stops and starts, newest first.
func (m model) historyView() string {
	t := m.th
	// Wide: the runs on the left, one chosen, what it did on the right.
	if m.width >= wideMin && len(m.hist) > 0 {
		return m.historyPanes()
	}
	var lines []string
	if len(m.hist) == 0 {
		lines = append(lines, "  "+ui.Fg(t.Muted).Render(i18n.T("history.none")))
	}
	// The year, where there is room for it: a narrow screen keeps the
	// results instead, as Recent does.
	layout := "2006-01-02 15:04"
	if m.width < 80 {
		layout = "01-02 15:04"
	}
	for i := len(m.hist) - 1; i >= 0; i-- {
		// Results that do not fit go on under the first's, not cut.
		for _, l := range m.entryLines(m.hist[i], layout, m.width-3) {
			lines = append(lines, "  "+l)
		}
	}
	shown, _ := m.scrolled(lines)
	status := " " + ui.Fg(t.Muted).Render(i18n.TN("history.count", len(m.hist), len(m.hist)))
	return m.page(shown, status, m.listKeys())
}

// helpView is the logo, the keys and how wsl-spawner works.
func (m model) helpView() string {
	t := m.th
	keys := [][2]string{
		{"↑ ↓", "help.select"}, {"s", "help.stop"}, {"S", "help.stopall"}, {"t", "help.start"}, {"m", "help.setup"},
		{"k", "help.keep"}, {"l", "help.logon"}, {"x", "help.skip"}, {"c", "help.check"}, {"h", "help.history"},
		{"r", "help.reload"}, {"q", "help.quit"},
	}
	section := func(s string) string { return "  " + lipgloss.NewStyle().Bold(true).Foreground(t.Muted).Render(s) }
	var lines []string
	// The logo, where it leaves the keys room: on a short screen its six
	// rows would be most of what shows.
	if m.height >= 20 {
		for _, l := range m.logoBlock() {
			lines = append(lines, "  "+l)
		}
		lines = append(lines, "")
	}
	lines = append(lines, section(i18n.T("help.keys")))
	for _, k := range keys {
		// The key's cap, then its words; their next lines under the words.
		lead := "  " + t.KeyCap(k[0], "") + ui.Fit("", max(0, 6-lipgloss.Width(k[0])))
		lines = append(lines, m.wrapHang(lead, strings.Repeat(" ", lipgloss.Width(lead)), i18n.T(k[1]), ui.Fg(t.Ink))...)
	}
	lines = append(lines, "", section(i18n.T("help.how")))
	for i, k := range []string{"help.how.stop", "help.how.hook", "help.how.back", "help.how.keep", "help.how.cli"} {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.wrapHang("  ", "  ", i18n.T(k), ui.Fg(t.Ink))...)
	}
	shown, _ := m.scrolled(lines)
	return m.page(shown, "", m.listKeys())
}
