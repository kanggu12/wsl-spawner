// Package gpo reads and changes this PC's local Group Policy where a sign-out
// script lives: scripts.ini, which lists the user scripts Windows runs as a
// user signs out, and gpt.ini, whose version tells Windows the policy
// changed. Signing out comes before every shutdown and restart, an update's
// restart included, and Windows waits for each such script in turn.
//
// Other programs keep their sign-out scripts in the same file. A change here
// keeps every one of them as Windows runs it, and a file it cannot read for
// certain as Windows does stays as it is: the change is refused, with why.
// Windows reads these files as its profile functions read any .ini file:
// sections and keys without regard to case, the blanks around a key and its
// value dropped, a value's enclosing quotes dropped, the first of two keys
// of one name taken, and the scripts from index 0 up to the first one
// missing.
//
// The text work is here, the same on every system; reading and writing the
// files, which needs an administrator, is in the Windows file.
package gpo

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Unsure is a policy file wsl-spawner leaves as it is, and why: it cannot
// read it, or the change, for certain as Windows does.
type Unsure struct {
	// File is the file, as named to the person; Why is what is uncertain.
	File, Why string
}

// Error says the file and why.
func (e *Unsure) Error() string { return e.File + ": " + e.Why }

// unsure is an Unsure for scripts.ini, the file named as the person knows it.
func unsure(why string, a ...any) *Unsure {
	return &Unsure{File: "scripts.ini", Why: fmt.Sprintf(why, a...)}
}

// scriptsCSE and scriptsTool are the GUIDs gpt.ini lists for the Scripts
// extension on the user's side, so Windows reads scripts.ini as it applies
// policy: the extension first in its brackets, then the editor's tool for it.
const (
	scriptsCSE  = "{42B5FAAE-6536-11D2-AE5A-0000F87571E3}"
	scriptsTool = "{40B6664F-4972-11D1-A7CA-0000F87571E3}"
)

// RefreshArgs are what Refresh gives gpupdate, and what the person types to
// have Windows read the policy themselves: the user's side only, and only
// what changed in it. No /force: it would apply every policy again, a
// domain's too, which can take long, ask to sign out, and have Windows
// apply some again at the next sign-in. A change here needs none of that:
// it raises gpt.ini's version (Bumped), which is how Windows tells a policy
// that changed from one it has read already.
var RefreshArgs = []string{"/target:user"}

// Script is one sign-out script as Windows runs it.
type Script struct {
	// Index is its place: Windows runs them from 0 up, one after another,
	// and stops at the first index that is missing.
	Index int
	// CmdLine is the program; Parameters what it is given: as Windows reads
	// them, without the blanks around them or the quotes they are in.
	CmdLine, Parameters string
}

// lines splits a file's text into lines, without their line breaks.
func lines(text string) []string {
	// A byte order mark left in the text is no part of the first line.
	text = strings.TrimPrefix(text, "\ufeff")
	ls := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	// The break after the last line ends it; it starts no line of its own.
	if n := len(ls); n > 0 && ls[n-1] == "" {
		ls = ls[:n-1]
	}
	return ls
}

// section is where one section of an .ini file is, read as Windows reads
// headings: a line in brackets names a section, without regard to case.
type section struct {
	// heading is the heading's line, -1 when there is none; start and end
	// are the lines of its body, up to the next heading or the file's end.
	heading, start, end int
	// doubt is why the file cannot be read for certain around the section:
	// two headings for it, or a heading that may or may not be one.
	doubt string
}

// findSection finds the section called name in ls.
func findSection(ls []string, name string) section {
	s := section{heading: -1}
	for i, l := range ls {
		t := strings.TrimSpace(l)
		// Only a line that starts with a bracket is a heading.
		if !strings.HasPrefix(t, "[") {
			continue
		}
		// Any heading ends the body of the section found before it.
		if s.heading >= 0 && s.end < 0 {
			s.end = i
		}
		closing := strings.LastIndex(t, "]")
		if closing < 0 {
			// Windows may take it as a heading or as a line of the section
			// above it: what follows it is uncertain.
			if s.doubt == "" {
				s.doubt = fmt.Sprintf("line %d, %q, opens a section and does not close it", i+1, t)
			}
			continue
		}
		inner := t[1:closing]
		if !strings.EqualFold(strings.TrimSpace(inner), name) {
			continue
		}
		// Blanks inside the brackets, or text after them: Windows may or
		// may not read it as this section.
		if inner != strings.TrimSpace(inner) || closing != len(t)-1 {
			if s.doubt == "" {
				s.doubt = fmt.Sprintf("line %d, %q, may or may not begin the [%s] section", i+1, t, name)
			}
			continue
		}
		// A second heading of the same name: which one Windows reads is
		// not certain.
		if s.heading >= 0 {
			if s.doubt == "" {
				s.doubt = fmt.Sprintf("it has [%s] twice", name)
			}
			continue
		}
		s.heading, s.start, s.end = i, i+1, -1
	}
	// The section, if any, runs to the end of the file.
	if s.heading >= 0 && s.end < 0 {
		s.end = len(ls)
	}
	return s
}

// scriptKey matches the name of a sign-out script's key, as Windows reads
// it: an index written as Windows writes one, then CmdLine or Parameters,
// without regard to case. "00CmdLine" is no key Windows ever reads.
var scriptKey = regexp.MustCompile(`(?i)^(0|[1-9][0-9]*)(cmdline|parameters)$`)

// key is a sign-out script's key: its index, and Parameters or CmdLine.
type key struct {
	index int
	param bool
}

// name is a key as Windows writes it, for the words.
func (k key) name() string {
	if k.param {
		return strconv.Itoa(k.index) + "Parameters"
	}
	return strconv.Itoa(k.index) + "CmdLine"
}

// logoff is scripts.ini's [Logoff] section as Windows reads it.
type logoff struct {
	// text is the file's text as read; ls its lines, and sec where the
	// section is in them.
	text string
	ls   []string
	sec  section
	// raw is each script key's value as written after its "=", and line
	// the line it is on: the first of two, as Windows takes it.
	raw  map[key]string
	line map[key]int
	// runs are the scripts Windows runs, in their order.
	runs []Script
	// doubt is why the file cannot be changed for certain, "" when it can.
	doubt string
}

// unquote is a value as Windows reads it: without the blanks around it, and
// without the quotes, double or single, that enclose all of it.
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if n := len(v); n >= 2 && (v[0] == '"' && v[n-1] == '"' || v[0] == '\'' && v[n-1] == '\'') {
		return v[1 : n-1]
	}
	return v
}

// readLogoff reads the [Logoff] section of scripts.ini's text.
func readLogoff(text string) logoff {
	lo := logoff{text: text, ls: lines(text), raw: map[key]string{}, line: map[key]int{}}
	lo.sec = findSection(lo.ls, "Logoff")
	lo.doubt = lo.sec.doubt
	// Note the first doubt only: one is enough to leave the file be.
	doubt := func(why string, a ...any) {
		if lo.doubt == "" {
			lo.doubt = fmt.Sprintf(why, a...)
		}
	}
	if lo.sec.heading < 0 {
		return lo
	}
	for i := lo.sec.start; i < lo.sec.end; i++ {
		l := lo.ls[i]
		name, value, hasValue := strings.Cut(l, "=")
		m := scriptKey.FindStringSubmatch(strings.TrimSpace(name))
		if m == nil {
			// No script's key: a comment, another key, or an index
			// Windows never reads. It stays as it is, and runs nothing.
			continue
		}
		// An index too large for a number is one Windows never reaches.
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		k := key{index: n, param: strings.EqualFold(m[2], "parameters")}
		if !hasValue {
			// A script's key with no "=": whether Windows reads it as an
			// empty value is not certain.
			doubt("line %d, %q, names %s without a value", i+1, strings.TrimSpace(l), k.name())
			continue
		}
		if _, seen := lo.raw[k]; seen {
			// Windows takes the first; a change that renumbers the scripts
			// could have it take the other.
			doubt("it names %s twice in [Logoff]", k.name())
			continue
		}
		lo.raw[k], lo.line[k] = value, i
	}
	// Windows runs the scripts from 0 up, and stops at the first missing.
	for i := 0; ; i++ {
		cmd, ok := lo.raw[key{index: i}]
		if !ok {
			break
		}
		// An empty program reads as a missing one, or as a script that
		// runs nothing: which, is not certain. The scripts stop here.
		if unquote(cmd) == "" {
			doubt("%s is empty", key{index: i}.name())
			break
		}
		lo.runs = append(lo.runs, Script{Index: i, CmdLine: unquote(cmd), Parameters: unquote(lo.raw[key{index: i, param: true}])})
	}
	return lo
}

// Logoff lists the sign-out scripts Windows runs, in their order: from
// index 0 up to the first one missing. A file that cannot be read for
// certain is read as Windows most likely reads it: a change refuses it.
func Logoff(text string) []Script { return readLogoff(text).runs }

// PowerShellFirst reads psscripts.ini's text for when Windows runs its
// sign-out scripts against scripts.ini's: [ScriptsConfig] EndExecutePSFirst,
// where the Group Policy editor writes the choice, "true" for before them
// and "false" for after, in any case. set is false when the file makes no
// choice wsl-spawner can read for certain: Windows' own policy then decides,
// which runs them after the others unless it is set to run them first.
func PowerShellFirst(text string) (first, set bool) {
	ls := lines(text)
	sec := findSection(ls, "ScriptsConfig")
	// No section, or one Windows may read otherwise: no choice to count on.
	if sec.heading < 0 || sec.doubt != "" {
		return false, false
	}
	for i := sec.start; i < sec.end; i++ {
		name, value, ok := strings.Cut(ls[i], "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "EndExecutePSFirst") {
			continue
		}
		// The first of two keys of one name is the one Windows takes; a
		// value that is neither word is no choice.
		v := unquote(value)
		return strings.EqualFold(v, "true"), strings.EqualFold(v, "true") || strings.EqualFold(v, "false")
	}
	return false, false
}

// Ours reports whether a script is the sign-out hook of the program at exe:
// the same program, without case, as Windows names files.
func Ours(s Script, exe string) bool { return strings.EqualFold(s.CmdLine, exe) }

// Copy reports whether a script is the sign-out hook of another copy of
// the program at exe, given params as a hook is: a program of the same
// file name in another folder, named by its full path, as an install writes
// it. One moved, or installed a second time elsewhere, leaves such a hook.
func Copy(s Script, exe, params string) bool {
	return !Ours(s, exe) && fullPath(s.CmdLine) && strings.EqualFold(fileName(s.CmdLine), fileName(exe)) &&
		strings.EqualFold(s.Parameters, params)
}

// fileName is the last part of a Windows path, after its last separator.
func fileName(p string) string { return p[strings.LastIndexAny(p, `\/`)+1:] }

// fullPath reports whether p is a Windows path from a drive's root (C:\...)
// or a share (\\server\...): one whose file can be looked for wherever the
// program runs. A bare name is found by Windows in the policy's own folder.
func fullPath(p string) bool {
	// A letter, a colon, then a separator: a drive's root.
	letter := len(p) >= 3 && strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", rune(p[0]))
	drive := letter && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
	return drive || strings.HasPrefix(p, `\\`)
}

// planned is a script a change writes: its program and parameters as they
// go into the file, after the "=".
type planned struct{ cmd, par string }

// rewrite is the file's text with want as the scripts Windows runs, in
// their order, indexed from 0. Every other line of the section stays as it
// is written, after the scripts; but a script of exe's that Windows does
// not run goes too. The other sections stay as they were; [Logoff] is made,
// first, when there was none. The text that comes out is read again: if
// Windows would run anything but want, the change is refused.
func (lo logoff) rewrite(want []planned, exe string) (string, error) {
	// A key at an index the scripts now reach, beyond those Windows ran: a
	// Parameters with no CmdLine, as nothing else stops there. A script
	// written there would meet it.
	for i := len(lo.runs); i < len(want); i++ {
		if _, ok := lo.raw[key{index: i, param: true}]; ok {
			return "", unsure("%s is there with no %s, where this change would put a script", key{index: i, param: true}.name(), key{index: i}.name())
		}
	}
	var body []string
	for i, s := range want {
		body = append(body, fmt.Sprintf("%dCmdLine=%s", i, s.cmd), fmt.Sprintf("%dParameters=%s", i, s.par))
	}
	// The lines that go: the keys of the scripts Windows ran, written again
	// above, and those of exe's scripts that it did not run.
	drop := map[int]bool{}
	for k, i := range lo.line {
		if k.index < len(lo.runs) {
			drop[i] = true
		}
	}
	for k, v := range lo.raw {
		if !k.param && k.index > len(lo.runs) && strings.EqualFold(unquote(v), exe) {
			drop[lo.line[k]] = true
			if i, ok := lo.line[key{index: k.index, param: true}]; ok {
				drop[i] = true
			}
		}
	}
	var out []string
	switch {
	case lo.sec.heading >= 0:
		// The heading as it was written, the scripts, then what else the
		// section had, blank lines left out.
		out = slices.Clone(lo.ls[:lo.sec.start])
		out = append(out, body...)
		for i := lo.sec.start; i < lo.sec.end; i++ {
			if !drop[i] && strings.TrimSpace(lo.ls[i]) != "" {
				out = append(out, lo.ls[i])
			}
		}
		out = append(out, lo.ls[lo.sec.end:]...)
	case len(want) == 0:
		// No section and nothing for one: the text stays, byte for byte,
		// and no file is made where there was none.
		return lo.text, nil
	default:
		// No [Logoff] yet: it comes first, as the Group Policy editor
		// writes it, above any other section.
		out = append(append([]string{"[Logoff]"}, body...), lo.ls...)
	}
	text := strings.Join(out, "\r\n") + "\r\n"
	// What Windows would run from the new text, against what was meant.
	again := readLogoff(text)
	if again.doubt != "" {
		return "", unsure("after the change, %s", again.doubt)
	}
	if len(again.runs) != len(want) {
		return "", unsure("it lists sign-out scripts after a missing index, which Windows does not run: with this change it would run them")
	}
	for i, s := range again.runs {
		if s.CmdLine != unquote(want[i].cmd) || s.Parameters != unquote(want[i].par) {
			return "", unsure("its sign-out scripts would not read back as written")
		}
	}
	return text, nil
}

// others is what Windows runs of lo's scripts but exe's, and but those of
// the programs drop names, as they are written.
func (lo logoff) others(exe string, drop []string) []planned {
	var keep []planned
	for _, s := range lo.runs {
		// A program drop names goes as exe's does: by its name, without case.
		if Ours(s, exe) || slices.ContainsFunc(drop, func(d string) bool { return Ours(s, d) }) {
			continue
		}
		keep = append(keep, planned{lo.raw[key{index: s.Index}], lo.raw[key{index: s.Index, param: true}]})
	}
	return keep
}

// WithHook is scripts.ini's text with exe's hook last among the sign-out
// scripts, so every other one, which may need WSL, runs before WSL stops. A
// hook of exe's already there is moved last, once. The scripts of the
// programs drop names go: the hooks of copies that are gone. Every other
// script stays as Windows runs it, written as it was. A text that cannot be
// changed so for certain is an *Unsure error.
func WithHook(text, exe, params string, drop ...string) (string, error) {
	lo := readLogoff(text)
	if lo.doubt != "" {
		return "", unsure("%s", lo.doubt)
	}
	return lo.rewrite(append(lo.others(exe, drop), planned{exe, params}), exe)
}

// WithoutHook is scripts.ini's text without exe's hook, nor the scripts of
// the programs drop names, the others indexed again from 0 in their order,
// as Windows runs them. A text that cannot be changed so for certain is an
// *Unsure error.
func WithoutHook(text, exe string, drop ...string) (string, error) {
	lo := readLogoff(text)
	if lo.doubt != "" {
		return "", unsure("%s", lo.doubt)
	}
	return lo.rewrite(lo.others(exe, drop), exe)
}

// Add is WithHook for a made-up PC's text, as the demo keeps: a text that
// cannot be changed for certain comes back as it was.
func Add(text, exe, params string, drop ...string) string {
	if out, err := WithHook(text, exe, params, drop...); err == nil {
		return out
	}
	return text
}

// Remove is WithoutHook for a made-up PC's text: a text that cannot be
// changed for certain comes back as it was.
func Remove(text, exe string, drop ...string) string {
	if out, err := WithoutHook(text, exe, drop...); err == nil {
		return out
	}
	return text
}

// bomUTF16LE and bomUTF8 are the byte order marks a policy file may start
// with: the Group Policy editor's, and UTF-8's, which Windows' profile
// functions may not take for one.
var (
	bomUTF16LE = []byte{0xff, 0xfe}
	bomUTF8    = []byte{0xef, 0xbb, 0xbf}
)

// decodeScripts is scripts.ini's text from its bytes, when wsl-spawner can
// read them for certain as Windows does: UTF-16 LE with its byte order
// mark, as the Group Policy editor writes it, or plain ASCII, which every
// code page reads the same. Any other bytes are an *Unsure error: written
// back as UTF-16, another tool's script would name another program.
func decodeScripts(b []byte) (string, error) {
	if len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
		return decodeUTF16("scripts.ini", b)
	}
	if why := notASCII(b); why != "" {
		return "", unsure("it is %s, which wsl-spawner cannot read for certain as Windows does", why)
	}
	return string(b), nil
}

// decodeUTF16 is the text of file's UTF-16 LE bytes after their mark, when
// they read back as the same bytes: a lone half of a pair, an odd last
// byte or a character 0 would be written back as something else.
func decodeUTF16(file string, b []byte) (string, error) {
	if len(b)%2 != 0 {
		return "", &Unsure{File: file, Why: "it is UTF-16 with an odd byte at its end"}
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
	}
	text := string(utf16.Decode(u))
	if !slices.Equal(encodeScripts(text), b) || strings.ContainsRune(text, 0) {
		return "", &Unsure{File: file, Why: "it is UTF-16 with characters that do not read back as written"}
	}
	return text, nil
}

// notASCII says what bytes with no UTF-16 mark are when they are not plain
// ASCII, or "" when they are.
func notASCII(b []byte) string {
	switch {
	case len(b) >= 3 && slices.Equal(b[:3], bomUTF8):
		return "UTF-8 with a byte order mark"
	case len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff:
		return "UTF-16 BE"
	case slices.Contains(b, 0):
		return "UTF-16 with no byte order mark"
	case slices.ContainsFunc(b, func(c byte) bool { return c >= 0x80 }):
		return "in a code page or in UTF-8, with characters outside ASCII"
	}
	return ""
}

// encodeScripts writes scripts.ini's text as UTF-16 LE with its mark, as
// the Group Policy editor does, which holds any program's name.
func encodeScripts(s string) []byte {
	out := slices.Clone(bomUTF16LE)
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// versionLine matches gpt.ini's version.
var versionLine = regexp.MustCompile(`(?i)^\s*Version\s*=\s*(\d+)\s*$`)

// Bumped is gpt.ini's bytes after a change of the user's side: its version
// up by one on the user's half (the high 16 bits), as the Group Policy
// editor counts, and the Scripts extension listed for the user, so Windows
// reads scripts.ini again. Only those two lines of [General] change; every
// other byte stays, in the encoding the file is in. A missing file is made.
// A file that cannot be changed so for certain is an *Unsure error.
func Bumped(b []byte) ([]byte, error) {
	switch {
	case len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe:
		// UTF-16 LE, as some tools write it: read, changed and written back
		// so.
		text, err := decodeUTF16("gpt.ini", b)
		if err != nil {
			return nil, err
		}
		out, err := bumpText(text)
		if err != nil {
			return nil, err
		}
		return encodeScripts(out), nil
	case len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff, slices.Contains(b, 0):
		// UTF-16 that is not little-endian with its mark: its lines are not
		// found as bytes.
		return nil, &Unsure{File: "gpt.ini", Why: "it is UTF-16 BE or UTF-16 with no byte order mark, which wsl-spawner cannot read for certain as Windows does"}
	}
	// One byte to a character for every line it changes, whatever code page
	// the rest is in: the lines are ASCII, and the rest is kept as bytes. A
	// UTF-8 mark stays where it was.
	mark := ""
	if len(b) >= 3 && slices.Equal(b[:3], bomUTF8) {
		mark, b = string(bomUTF8), b[3:]
	}
	out, err := bumpText(string(b))
	if err != nil {
		return nil, err
	}
	return []byte(mark + out), nil
}

// bumpText is Bumped on gpt.ini's text.
func bumpText(text string) (string, error) {
	bad := func(why string, a ...any) error { return &Unsure{File: "gpt.ini", Why: fmt.Sprintf(why, a...)} }
	ls := lines(text)
	sec := findSection(ls, "General")
	if sec.doubt != "" {
		return "", bad("%s", sec.doubt)
	}
	if sec.heading < 0 {
		// Only an empty file is made whole: keys above any heading would
		// land in the new section.
		for _, l := range ls {
			if strings.TrimSpace(l) != "" {
				return "", bad("it has no [General] section")
			}
		}
		ls = []string{"[General]"}
		sec = section{heading: 0, start: 1, end: 1}
	}
	version, ext := -1, -1
	for i := sec.start; i < sec.end; i++ {
		name, value, ok := strings.Cut(ls[i], "=")
		if !ok {
			continue
		}
		switch {
		case strings.EqualFold(strings.TrimSpace(name), "Version"):
			if version >= 0 {
				return "", bad("it has Version twice")
			}
			m := versionLine.FindStringSubmatch(ls[i])
			v, err := uint64(0), error(nil)
			if m != nil {
				v, err = strconv.ParseUint(m[1], 10, 32)
			}
			if m == nil || err != nil {
				return "", bad("its Version, %q, is not a number wsl-spawner can count on", strings.TrimSpace(value))
			}
			// The machine's half stays; the user's goes up, round past
			// its largest to 1, never to 0, which reads as no policy.
			user, machine := (v>>16)+1, v&0xffff
			if user > 0xffff {
				user = 1
			}
			ls[i], version = "Version="+strconv.FormatUint(user<<16|machine, 10), i
		case strings.EqualFold(strings.TrimSpace(name), "gPCUserExtensionNames"):
			if ext >= 0 {
				return "", bad("it has gPCUserExtensionNames twice")
			}
			merged, err := withScripts(strings.TrimSpace(value))
			if err != nil {
				return "", bad("%s", err)
			}
			// A list that has the Scripts extension already stays as it is
			// written.
			if merged != strings.TrimSpace(value) {
				ls[i] = "gPCUserExtensionNames=" + merged
			}
			ext = i
		}
	}
	// What is missing goes at the end of [General], before the next
	// section.
	var add []string
	if version < 0 {
		add = append(add, "Version=65536")
	}
	if ext < 0 {
		add = append(add, "gPCUserExtensionNames=["+scriptsCSE+scriptsTool+"]")
	}
	ls = slices.Insert(ls, sec.end, add...)
	return strings.Join(ls, "\r\n") + "\r\n", nil
}

// extGroup matches one bracket of an extension list: an extension's GUID,
// then the GUIDs of its tools.
var (
	extGroup = regexp.MustCompile(`^\[((?:\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\})+)\]`)
	extGUID  = regexp.MustCompile(`\{[^}]*\}`)
)

// withScripts is a gPCUserExtensionNames value with the Scripts extension
// and its tool in it, as the Group Policy editor keeps the list: a bracket
// for each extension, in the order of their GUIDs, and its tools' GUIDs in
// theirs. The other brackets stay as they are written. A value that is not
// such a list is an error.
func withScripts(v string) (string, error) {
	var groups []string
	for rest := v; rest != ""; {
		m := extGroup.FindStringSubmatch(rest)
		if m == nil {
			return "", fmt.Errorf("its gPCUserExtensionNames, %q, is not a list of extensions wsl-spawner can read for certain", v)
		}
		groups, rest = append(groups, m[0]), rest[len(m[0]):]
	}
	// The GUIDs of a bracket, the extension's first.
	guids := func(g string) []string { return extGUID.FindAllString(g, -1) }
	for i, g := range groups {
		ids := guids(g)
		if !strings.EqualFold(ids[0], scriptsCSE) {
			continue
		}
		if slices.ContainsFunc(ids[1:], func(id string) bool { return strings.EqualFold(id, scriptsTool) }) {
			// Listed already, tool and all: nothing changes.
			return v, nil
		}
		// The extension is there with other tools: its tool joins them,
		// before the first whose GUID comes after its own.
		tools, at := ids[1:], len(ids)-1
		for j, id := range tools {
			if strings.ToUpper(id) > scriptsTool {
				at = j
				break
			}
		}
		groups[i] = "[" + ids[0] + strings.Join(slices.Insert(slices.Clone(tools), at, scriptsTool), "") + "]"
		return strings.Join(groups, ""), nil
	}
	// A bracket of its own, before the first whose extension's GUID comes
	// after it.
	at := len(groups)
	for i, g := range groups {
		if strings.ToUpper(guids(g)[0]) > scriptsCSE {
			at = i
			break
		}
	}
	return strings.Join(slices.Insert(groups, at, "["+scriptsCSE+scriptsTool+"]"), ""), nil
}
