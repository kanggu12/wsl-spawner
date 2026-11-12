package gpo

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// A scripts.ini as the Group Policy editor leaves it: a sign-out script of
// another tool's, and a sign-in one.
const editors = "\ufeff\r\n[Logoff]\r\n0CmdLine=C:\\Tools\\save-notes.cmd\r\n0Parameters=\r\n1CmdLine=C:\\Tools\\backup.ps1\r\n1Parameters=-Quick\r\n[Logon]\r\n0CmdLine=C:\\Tools\\mount.cmd\r\n0Parameters=\r\n"

const exe = `C:\Users\Ann\AppData\Local\Programs\wsl-spawner\wsl-spawnerw.exe`

func TestLogoffReadsTheScriptsInTheirOrder(t *testing.T) {
	got := Logoff(editors)
	want := []Script{{0, `C:\Tools\save-notes.cmd`, ""}, {1, `C:\Tools\backup.ps1`, "-Quick"}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
	if Logoff("") != nil || Logoff("[Logon]\r\n0CmdLine=x\r\n") != nil {
		t.Fatal("a file without sign-out scripts read some")
	}
}

func TestAddPutsTheHookLastAndKeepsTheRest(t *testing.T) {
	out := Add(editors, exe, "logoff")
	got := Logoff(out)
	if len(got) != 3 || got[2] != (Script{2, exe, "logoff"}) || got[0].CmdLine != `C:\Tools\save-notes.cmd` {
		t.Fatalf("%+v", got)
	}
	// The sign-in scripts stay as they were.
	if !strings.Contains(out, "[Logon]\r\n0CmdLine=C:\\Tools\\mount.cmd\r\n0Parameters=\r\n") {
		t.Fatalf("%q", out)
	}
	// Added again, it is there once, still last.
	again := Logoff(Add(out, strings.ToUpper(exe), "logoff"))
	if len(again) != 3 || !Ours(again[2], exe) {
		t.Fatalf("%+v", again)
	}
	// A hook put first by hand moves last.
	first := "[Logoff]\r\n0CmdLine=" + exe + "\r\n0Parameters=logoff\r\n1CmdLine=C:\\x.cmd\r\n1Parameters=\r\n"
	if got := Logoff(Add(first, exe, "logoff")); got[0].CmdLine != `C:\x.cmd` || !Ours(got[1], exe) {
		t.Fatalf("%+v", got)
	}
}

func TestAddMakesTheFileWhenThereIsNone(t *testing.T) {
	out := Add("", exe, "logoff")
	if out != "[Logoff]\r\n0CmdLine="+exe+"\r\n0Parameters=logoff\r\n" {
		t.Fatalf("%q", out)
	}
	// Above another section, as the editor writes it.
	if out := Add("[Logon]\r\n0CmdLine=m\r\n0Parameters=\r\n", exe, "logoff"); !strings.HasPrefix(out, "[Logoff]\r\n0CmdLine=") || !strings.Contains(out, "[Logon]") {
		t.Fatalf("%q", out)
	}
}

func TestRemoveIndexesTheOthersAgain(t *testing.T) {
	// The hook in the middle: what follows it moves up, so no index is
	// missing, which would stop Windows reading the rest.
	mid := "[Logoff]\r\n0CmdLine=C:\\a.cmd\r\n0Parameters=\r\n1CmdLine=" + exe + "\r\n1Parameters=logoff\r\n2CmdLine=C:\\b.cmd\r\n2Parameters=-x\r\n"
	got := Logoff(Remove(mid, exe))
	if !slices.Equal(got, []Script{{0, `C:\a.cmd`, ""}, {1, `C:\b.cmd`, "-x"}}) {
		t.Fatalf("%+v", got)
	}
	// The editor's file without the hook is as it was, in what it says.
	if !slices.Equal(Logoff(Remove(Add(editors, exe, "logoff"), exe)), Logoff(editors)) {
		t.Fatal("not as it was")
	}
	// The last one gone leaves the section empty, and nothing else changed.
	only := "[Logoff]\r\n0CmdLine=" + exe + "\r\n0Parameters=logoff\r\n[Logon]\r\n"
	if out := Remove(only, exe); out != "[Logoff]\r\n[Logon]\r\n" {
		t.Fatalf("%q", out)
	}
	// No file, or none with sign-out scripts: nothing to take out, and the
	// text stays byte for byte, so no file is made.
	for _, text := range []string{"", "[Logon]\n0CmdLine=m.cmd"} {
		if out, err := WithoutHook(text, exe); out != text || err != nil {
			t.Fatalf("%q: %q %v", text, out, err)
		}
	}
}

func TestCopyIsTheHookOfTheProgramInAnotherFolder(t *testing.T) {
	// Another copy's hook: the same file name, in another folder, by its
	// full path, given the hook's parameters, as an install writes it.
	for s, want := range map[Script]bool{
		{0, `D:\old\wsl-spawnerw.exe`, "logoff"}:                       true,
		{0, `d:\OLD\WSL-SPAWNERW.EXE`, "Logoff"}:                       true,
		{0, `\\nas\tools\wsl-spawnerw.exe`, "logoff"}:                  true,
		{0, `D:/old/wsl-spawnerw.exe`, "logoff"}:                       true,
		{0, exe, "logoff"}:                                             false, // this copy's own
		{0, strings.ToUpper(exe), "logoff"}:                            false,
		{0, `D:\old\wsl-spawnerw.exe`, "logon"}:                        false, // not a hook
		{0, `D:\old\wsl-spawnerw.exe`, ""}:                             false,
		{0, `wsl-spawnerw.exe`, "logoff"}:                              false, // the policy's own folder
		{0, `D:\old\wsl-spawner.exe`, "logoff"}:                        false, // another program
		{0, `D:\old\not-wsl-spawnerw.exe`, "logoff"}:                   false,
		{0, `C:\Tools\save-notes.cmd`, ""}:                             false,
		{0, `1:\old\wsl-spawnerw.exe`, "logoff"}:                       false,
		{0, `D:old\wsl-spawnerw.exe`, "logoff"}:                        false,
		{0, `C:\Users\Bob\AppData\Local\wsl-spawnerw.exe`, "logoff"}:   true,
		{0, `C:\Users\Ann\AppData\Local\wsl-spawnerw.exe\x`, "logoff"}: false,
	} {
		if got := Copy(s, exe, "logoff"); got != want {
			t.Errorf("%+v: %v, want %v", s, got, want)
		}
	}
}

func TestAChangeTakesOutTheHooksOfCopiesThatAreGone(t *testing.T) {
	// A copy moved away left its hook first; a second copy that is there
	// has one too. The hooks named to drop go, whatever their case, with
	// the change; every other script stays in its order.
	const old, second = `D:\old\wsl-spawnerw.exe`, `E:\second\wsl-spawnerw.exe`
	text := "[Logoff]\r\n0CmdLine=" + old + "\r\n0Parameters=logoff\r\n1CmdLine=C:\\Tools\\save-notes.cmd\r\n1Parameters=\r\n" +
		"2CmdLine=" + second + "\r\n2Parameters=logoff\r\n"
	out, err := WithHook(text, exe, "logoff", strings.ToUpper(old))
	want := []Script{{0, `C:\Tools\save-notes.cmd`, ""}, {1, second, "logoff"}, {2, exe, "logoff"}}
	if got := Logoff(out); err != nil || !slices.Equal(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
	out, err = WithoutHook(out, exe, old)
	if got := Logoff(out); err != nil || !slices.Equal(got, want[:2]) {
		t.Fatalf("%+v %v", got, err)
	}
	// A copy's hook alone goes, this copy's not being there.
	if got := Logoff(Remove(text, exe, old)); !slices.Equal(got, []Script{{0, `C:\Tools\save-notes.cmd`, ""}, {1, second, "logoff"}}) {
		t.Fatalf("%+v", got)
	}
	// Nothing named: every copy's hook stays, as before.
	if got := Logoff(Remove(text, exe)); !slices.Equal(got, Logoff(text)) {
		t.Fatalf("%+v", got)
	}
}

// scriptsPair is the bracket gpt.ini lists for the Scripts extension and
// its tool.
const scriptsPair = "[" + scriptsCSE + scriptsTool + "]"

// bumped is Bumped on a text in one byte to a character, as gpt.ini is
// written, failing the test on a refusal.
func bumped(t *testing.T, s string) string {
	t.Helper()
	out, err := Bumped([]byte(s))
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return string(out)
}

func TestBumpedCountsTheUsersHalfAndListsTheScripts(t *testing.T) {
	// The editor's gpt.ini with a machine change and a user change.
	in := "[General]\r\ngPCMachineExtensionNames=[{35378EAC-683F-11D2-A89A-00C04FBBCFA2}{D02B1F72-3407-48AE-BA88-E8213C6761F1}]\r\nVersion=65539\r\n"
	out := bumped(t, in)
	if !strings.Contains(out, "Version=131075\r\n") || !strings.Contains(out, "gPCUserExtensionNames="+scriptsPair+"\r\n") {
		t.Fatalf("%q", out)
	}
	// Listed already: not listed twice.
	if again := bumped(t, out); strings.Count(again, "42B5FAAE") != 1 || !strings.Contains(again, "Version=196611") {
		t.Fatalf("%q", again)
	}
	// No file: one is made.
	if out := bumped(t, ""); out != "[General]\r\nVersion=65536\r\ngPCUserExtensionNames="+scriptsPair+"\r\n" {
		t.Fatalf("%q", out)
	}
	// The user's half at its largest goes round to 1, never to 0.
	if out := bumped(t, "[General]\r\nVersion="+"4294901762"+"\r\n"); !strings.Contains(out, "Version=65538") {
		t.Fatalf("%q", out)
	}
}

func TestPowerShellFirstIsTheChoicePsscriptsIniMakes(t *testing.T) {
	// [ScriptsConfig] EndExecutePSFirst, "true" or "false" in any case, as
	// the Group Policy editor writes it; anything else, or a section Windows
	// may read otherwise, makes no choice to count on.
	for text, want := range map[string][2]bool{
		"[ScriptsConfig]\r\nEndExecutePSFirst=true\r\n[Logoff]\r\n0CmdLine=a.ps1\r\n0Parameters=\r\n": {true, true},
		"[ScriptsConfig]\r\nStartExecutePSFirst=true\r\nEndExecutePSFirst=false\r\n":                  {false, true},
		"\r\n[scriptsconfig]\r\n endexecutepsfirst = TRUE \r\n":                                       {true, true},
		"[ScriptsConfig]\r\nEndExecutePSFirst=\"true\"\r\n":                                           {true, true},
		"[ScriptsConfig]\r\nEndExecutePSFirst=true\r\nEndExecutePSFirst=false\r\n":                    {true, true},
		"[ScriptsConfig]\r\nStartExecutePSFirst=true\r\n":                                             {false, false},
		"[ScriptsConfig]\r\nEndExecutePSFirst=yes\r\n":                                                {false, false},
		"[Logoff]\r\nEndExecutePSFirst=true\r\n":                                                      {false, false},
		"[ScriptsConfig]\r\nEndExecutePSFirst=true\r\n[ScriptsConfig]\r\nEndExecutePSFirst=false\r\n": {false, false},
		"": {false, false},
	} {
		if first, set := PowerShellFirst(text); first != want[0] || set != want[1] {
			t.Errorf("%q: first %v, set %v; want %v, %v", text, first, set, want[0], want[1])
		}
	}
}

func TestWindowsReadsOnlyWhatChangedOfTheUsersPolicy(t *testing.T) {
	// gpupdate for the user's side alone, without /force: every change
	// raises the user's half of gpt.ini's version (above), which is what
	// Windows reads a policy again for; /force would apply every policy
	// again, a domain's too.
	if !slices.Equal(RefreshArgs, []string{"/target:user"}) {
		t.Fatalf("gpupdate %q", RefreshArgs)
	}
}

func TestBumpedMergesTheScriptsExtensionAsTheEditorKeepsTheList(t *testing.T) {
	const (
		other    = "{11111111-1111-1111-1111-111111111111}"
		tool     = "{22222222-2222-2222-2222-222222222222}"
		later    = "{827D319E-6EAC-11D2-A4EA-00C04F79F83A}"
		laterOne = "{803E14A0-B4FB-11D0-A0D0-00A0C90F574B}"
	)
	for in, want := range map[string]string{
		// The extension with another tool of its own: its tool joins that
		// bracket, in the order of the GUIDs, and no second one is made.
		"[" + scriptsCSE + tool + "]": "[" + scriptsCSE + tool + scriptsTool + "]",
		// A bracket of its own goes where its GUID sorts: after a lower
		// one, before a higher one.
		"[" + other + tool + "][" + later + laterOne + "]": "[" + other + tool + "]" + scriptsPair + "[" + later + laterOne + "]",
		// Listed already, in lower case: nothing changes.
		strings.ToLower(scriptsPair): strings.ToLower(scriptsPair),
		// Empty: the bracket alone.
		"": scriptsPair,
	} {
		out := bumped(t, "[General]\r\nVersion=1\r\ngPCUserExtensionNames="+in+"\r\n")
		if !strings.Contains(out, "\r\ngPCUserExtensionNames="+want+"\r\n") || strings.Count(strings.ToUpper(out), "42B5FAAE") != 1 {
			t.Errorf("%q: %q", in, out)
		}
	}
}

func TestBumpedChangesOnlyItsTwoLinesOfGeneral(t *testing.T) {
	// Keys missing from [General] go into [General], not after another
	// section; every other byte stays, a code page's characters included.
	in := "[General]\r\ndisplayName=\xb1\xe8 policy\r\nVersion=0\r\n[Other]\r\nx=y\r\n"
	out := bumped(t, in)
	if out != "[General]\r\ndisplayName=\xb1\xe8 policy\r\nVersion=65536\r\ngPCUserExtensionNames="+scriptsPair+"\r\n[Other]\r\nx=y\r\n" {
		t.Fatalf("%q", out)
	}
	// A UTF-8 mark stays where it was.
	if out := bumped(t, "\xef\xbb\xbf[General]\r\nVersion=0\r\n"); !strings.HasPrefix(out, "\xef\xbb\xbf[General]\r\nVersion=65536\r\n") {
		t.Fatalf("%q", out)
	}
	// UTF-16 LE, as some tools write it, is written back so.
	b, err := Bumped(encodeScripts("[General]\r\nVersion=7\r\n"))
	if text, derr := decodeUTF16("gpt.ini", b); err != nil || derr != nil || text != "[General]\r\nVersion=65543\r\ngPCUserExtensionNames="+scriptsPair+"\r\n" {
		t.Fatalf("%q %v %v", text, err, derr)
	}
}

func TestBumpedLeavesAGptIniItCannotCountOn(t *testing.T) {
	for name, in := range map[string][]byte{
		"two versions":           []byte("[General]\r\nVersion=1\r\nVersion=2\r\n"),
		"a version too large":    []byte("[General]\r\nVersion=99999999999\r\n"),
		"a version not a number": []byte("[General]\r\nVersion=x\r\n"),
		"an odd extension list":  []byte("[General]\r\ngPCUserExtensionNames=[{42B5FAAE}]\r\n"),
		"no [General]":           []byte("[Other]\r\nx=y\r\n"),
		"UTF-16 with no mark":    []byte("[\x00G\x00"),
		"UTF-16 BE":              {0xfe, 0xff, 0, '['},
	} {
		var u *Unsure
		if out, err := Bumped(in); !errors.As(err, &u) || u.File != "gpt.ini" || out != nil {
			t.Errorf("%s: %q %v", name, out, err)
		}
	}
}

func TestKeysAreReadAsWindowsReadsThem(t *testing.T) {
	// Windows reads a key without regard to case, and without the blanks
	// around it or its value: each of these is the script it runs first,
	// and stays first, written as it was.
	for name, text := range map[string]string{
		"lower case":   "[Logoff]\r\n0cmdline=lower.cmd\r\n0parameters=-q\r\n",
		"blanks":       "[Logoff]\r\n  0CmdLine = lower.cmd\r\n0Parameters = -q\r\n",
		"heading case": "[logoff]\r\n0CmdLine=lower.cmd\r\n0Parameters=-q\r\n",
		"quotes":       "[Logoff]\r\n0CmdLine=\"lower.cmd\"\r\n0Parameters='-q'\r\n",
	} {
		out := Add(text, exe, "logoff")
		got := Logoff(out)
		if len(got) != 2 || got[0] != (Script{0, "lower.cmd", "-q"}) || !Ours(got[1], exe) {
			t.Errorf("%s: %+v\n%q", name, got, out)
		}
		// Nothing of it is left to be read as a second script 0.
		if n := strings.Count(strings.ToLower(out), "0cmdline"); n != 1 {
			t.Errorf("%s: 0CmdLine %d times: %q", name, n, out)
		}
	}
	// A value is written back as it was written, blanks and quotes too.
	out := Add("[Logoff]\r\n0CmdLine= \"C:\\Program Files\\x.cmd\"\r\n0Parameters=\r\n", exe, "logoff")
	if !strings.Contains(out, "\r\n0CmdLine= \"C:\\Program Files\\x.cmd\"\r\n") {
		t.Fatalf("%q", out)
	}
	// A hook of exe's written in quotes is exe's hook.
	if got := Logoff(Add("[Logoff]\r\n0CmdLine=\""+exe+"\"\r\n0Parameters=logoff\r\n1CmdLine=x.cmd\r\n", exe, "logoff")); len(got) != 2 || got[0].CmdLine != "x.cmd" {
		t.Fatalf("%+v", got)
	}
}

func TestAFileWindowsMayReadOtherwiseStaysAsItIs(t *testing.T) {
	// Where Windows' reading is not certain, a change could have another
	// script stop running, or one that does not run start: the text stays.
	for name, text := range map[string]string{
		"a key twice":            "[Logoff]\r\n0CmdLine=a.cmd\r\n0Parameters=\r\n0CmdLine=b.cmd\r\n",
		"a key twice, in a case": "[Logoff]\r\n0CmdLine=a.cmd\r\n0cmdline=b.cmd\r\n",
		"two sections":           "[Logoff]\r\n0CmdLine=a.cmd\r\n[Logon]\r\n[Logoff]\r\n1CmdLine=b.cmd\r\n",
		"blanks in the heading":  "[ Logoff ]\r\n0CmdLine=a.cmd\r\n",
		"text after the heading": "[Logoff] ; mine\r\n0CmdLine=a.cmd\r\n",
		"a heading not closed":   "[Logoff]\r\n0CmdLine=a.cmd\r\n[Logon\r\n1CmdLine=b.cmd\r\n",
		"a key with no value":    "[Logoff]\r\n0CmdLine\r\n",
		"an empty program":       "[Logoff]\r\n0CmdLine=\r\n0Parameters=\r\n1CmdLine=b.cmd\r\n",
		// A Parameters with no CmdLine where wsl-spawner's would go.
		"parameters alone": "[Logoff]\r\n0CmdLine=a.cmd\r\n0Parameters=\r\n1Parameters=-x\r\n",
	} {
		if out := Add(text, exe, "logoff"); out != text {
			t.Errorf("%s: added: %q", name, out)
		}
		if out := Remove(text, exe); out != text {
			t.Errorf("%s: removed: %q", name, out)
		}
	}
}

func TestScriptsAfterAMissingIndexStayUnrun(t *testing.T) {
	// Windows runs 0 and 1 and stops at the missing 2: the script at 4
	// does not run, and still does not with wsl-spawner's at 2.
	gap := "[Logoff]\r\n0CmdLine=a.cmd\r\n0Parameters=\r\n1CmdLine=b.cmd\r\n1Parameters=\r\n4CmdLine=inert.cmd\r\n4Parameters=\r\n"
	out := Add(gap, exe, "logoff")
	if got := Logoff(out); len(got) != 3 || got[0].CmdLine != "a.cmd" || got[1].CmdLine != "b.cmd" || !Ours(got[2], exe) {
		t.Fatalf("%+v\n%q", got, out)
	}
	// Its lines stay as they were, for whatever keeps them there.
	if !strings.Contains(out, "\r\n4CmdLine=inert.cmd\r\n4Parameters=\r\n") {
		t.Fatalf("%q", out)
	}
	// Taken out again, the others are as they were, the one at 4 unrun.
	if got := Logoff(Remove(out, exe)); len(got) != 2 || got[1].CmdLine != "b.cmd" {
		t.Fatalf("%+v", got)
	}
	// Right after the missing index, wsl-spawner's at 2 would have the
	// script at 3 run: the text stays.
	next := "[Logoff]\r\n0CmdLine=a.cmd\r\n0Parameters=\r\n1CmdLine=b.cmd\r\n1Parameters=\r\n3CmdLine=inert.cmd\r\n3Parameters=\r\n"
	if out := Add(next, exe, "logoff"); out != next {
		t.Fatalf("%q", out)
	}
	// A hook taken out of the middle leaves the missing index missing.
	mid := "[Logoff]\r\n0CmdLine=" + exe + "\r\n0Parameters=logoff\r\n1CmdLine=a.cmd\r\n1Parameters=\r\n3CmdLine=inert.cmd\r\n3Parameters=\r\n"
	if got := Logoff(Remove(mid, exe)); len(got) != 1 || got[0].CmdLine != "a.cmd" {
		t.Fatalf("%+v", got)
	}
	// A hook of exe's that Windows does not run goes too, as it is moved
	// last or taken out.
	unrun := "[Logoff]\r\n0CmdLine=a.cmd\r\n0Parameters=\r\n5CmdLine=" + exe + "\r\n5Parameters=logoff\r\n"
	if out := Remove(unrun, exe); strings.Contains(out, "5CmdLine") || len(Logoff(out)) != 1 {
		t.Fatalf("%q", out)
	}
	if out := Add(unrun, exe, "logoff"); strings.Contains(out, "5CmdLine") || len(Logoff(out)) != 2 {
		t.Fatalf("%q", out)
	}
}

func TestScriptsIsReadOnlyWhereItIsCertain(t *testing.T) {
	// The reviewer's case: a script that wrote scripts.ini in Korean
	// Windows' code page. Written back as UTF-16, the program's name would
	// be lost; it is refused, and nothing is written.
	cp949 := []byte("[Logoff]\r\n0CmdLine=C:\\Users\\\xb1\xe8\\x.cmd\r\n0Parameters=\r\n")
	for name, b := range map[string][]byte{
		"a code page":             cp949,
		"UTF-8 with no mark":      []byte("[Logoff]\r\n0CmdLine=C:\\Users\\\xea\xb9\x80\\x.cmd\r\n"),
		"UTF-8 with its mark":     append([]byte{0xef, 0xbb, 0xbf}, "[Logoff]\r\n0CmdLine=x.cmd\r\n"...),
		"UTF-16 with no mark":     []byte("[\x00L\x00o\x00g\x00o\x00f\x00f\x00]\x00"),
		"UTF-16 BE":               {0xfe, 0xff, 0, '['},
		"UTF-16 with an odd end":  {0xff, 0xfe, '[', 0, 'L'},
		"UTF-16 with half a pair": {0xff, 0xfe, 0x00, 0xd8, '[', 0},
	} {
		text, err := decodeScripts(b)
		var u *Unsure
		if !errors.As(err, &u) || text != "" {
			t.Errorf("%s: read as %q, %v", name, text, err)
		}
	}
	// The editor's UTF-16 LE, a program in Korean included, and plain
	// ASCII read as written, and the first is written back byte for byte.
	editorsFile := encodeScripts("\r\n[Logoff]\r\n0CmdLine=C:\\Users\\\uae40\\x.cmd\r\n0Parameters=\r\n")
	if text, err := decodeScripts(editorsFile); err != nil || !slices.Equal(encodeScripts(text), editorsFile) || Logoff(text)[0].CmdLine != "C:\\Users\\\uae40\\x.cmd" {
		t.Fatalf("%q %v", text, err)
	}
	if text, err := decodeScripts([]byte("[Logoff]\r\n0CmdLine=x.cmd\r\n")); err != nil || Logoff(text)[0].CmdLine != "x.cmd" {
		t.Fatalf("%q %v", text, err)
	}
	// No file and an empty one read as no scripts.
	if text, err := decodeScripts(nil); err != nil || text != "" {
		t.Fatalf("%q %v", text, err)
	}
}

func TestWithHookSaysWhyItLeavesAFile(t *testing.T) {
	// The words name the file and what is uncertain.
	_, err := WithHook("[Logoff]\r\n0CmdLine=a.cmd\r\n0cmdline=b.cmd\r\n", exe, "logoff")
	var u *Unsure
	if !errors.As(err, &u) || err.Error() != "scripts.ini: it names 0CmdLine twice in [Logoff]" {
		t.Fatalf("%v", err)
	}
	_, err = WithHook("[Logoff]\r\n0CmdLine=a.cmd\r\n0Parameters=\r\n2CmdLine=b.cmd\r\n", exe, "logoff")
	if !errors.As(err, &u) || !strings.Contains(err.Error(), "after a missing index") {
		t.Fatalf("%v", err)
	}
}

func TestAProgramWithABlankInItsPathIsWrittenInQuotes(t *testing.T) {
	// A user named Ann Lee has the program in C:\Users\Ann Lee\...: written
	// bare, Windows reads it as the program C:\Users\Ann.exe, or runs
	// nothing, and the hook would never stop anything. In quotes it is the
	// program, and reads back as the path it is.
	spaced := `C:\Users\Ann Lee\AppData\Local\Programs\wsl-spawner\wsl-spawnerw.exe`
	out := Add(editors, spaced, "logoff")
	if !strings.Contains(out, "2CmdLine=\""+spaced+"\"\r\n2Parameters=logoff\r\n") {
		t.Fatalf("%q", out)
	}
	got := Logoff(out)
	if len(got) != 3 || got[2] != (Script{2, spaced, "logoff"}) || !Ours(got[2], spaced) {
		t.Fatalf("%+v", got)
	}
	// Once there, once: added again it is moved, not made twice, and taken
	// out it is gone, whatever case the path is spelled in.
	if again := Logoff(Add(out, strings.ToUpper(spaced), "logoff")); len(again) != 3 {
		t.Fatalf("%+v", again)
	}
	if rest := Logoff(Remove(out, spaced)); len(rest) != 2 {
		t.Fatalf("%+v", rest)
	}
	// A path with no blank is written as it always was, bare.
	if bare := Add(editors, exe, "logoff"); !strings.Contains(bare, "2CmdLine="+exe+"\r\n") {
		t.Fatalf("%q", bare)
	}
}
