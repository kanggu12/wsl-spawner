package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
)

// workflowWords is what a step of a workflow looks for in the program's
// output: the pattern in quotes right after lead, in the workflow's file.
func workflowWords(t *testing.T, file, lead string) *regexp.Regexp {
	t.Helper()
	// The tests run in this package's folder, three below the module's.
	b, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", file))
	if err != nil {
		t.Fatal(err)
	}
	_, after, found := strings.Cut(string(b), lead)
	if !found {
		t.Fatalf("%s has no %q", file, lead)
	}
	words, _, found := strings.Cut(after, "'")
	if !found || words == "" {
		t.Fatalf("%s: no pattern after %q", file, lead)
	}
	return regexp.MustCompile(words)
}

func TestTheWorkflowTellsARefusedHookByTheWordsTheProgramSays(t *testing.T) {
	// On a runner the checkout is where every user may write, and the step
	// tells the hook's refusal from an install by words in what the program
	// printed. Said another way in the program, the step once took the
	// refusal for no refusal, and passed. The words it looks for are found
	// in the refusal as the program says it.
	words := workflowWords(t, "wsl.yml", "$LASTEXITCODE -eq 1 -and $said -match '")
	a, _ := pc(t)
	p := newPolicy(t, a, another, false)
	a.PC.WhoElseCanChange = func(string, bool) (string, string, error) { return `BUILTIN\Users`, `D:\a\dist`, nil }
	_, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitFail || !words.MatchString(errOut) || len(gpo.Logoff(p.text)) != 1 {
		t.Fatalf("the step looks for %q; exit %d: %s", words, code, errOut)
	}
}

func TestTheWorkflowTellsAScoopInstallLeftToScoopByTheWordsTheProgramSays(t *testing.T) {
	// The release's check of the Scoop manifest runs uninstall and looks
	// for the line that leaves the program to Scoop, with exit code 0.
	words := workflowWords(t, "installers.yml", "$LASTEXITCODE -ne 0 -or $said -notmatch '")
	fi := installed(t, filepath.Join("scoop", "apps", "wsl-spawner", "current"))
	write(t, filepath.Join(fi.dir, "scoop-manifest.json"), "{}")
	code, out, errOut := fi.run(t, "", "uninstall", "--yes")
	if code != exitOK || !words.MatchString(out+errOut) || !there(fi.exe) {
		t.Fatalf("the step looks for %q; exit %d: %s%s", words, code, out, errOut)
	}
}

func TestTheWorkflowTellsAProgramLeftToAnAdministratorByTheWordsTheProgramSays(t *testing.T) {
	// The release's check with a real standard user runs their uninstall
	// of a program under Program Files and looks, with exit code 0, for the
	// line that says the program stays.
	words := workflowWords(t, "installers.yml", "$code -ne 0 -or $said -notmatch '")
	fi := installed(t, "wsl-spawner")
	fi.a.PC.CanDelete = func(string) error { return fs.ErrPermission }
	code, out, errOut := fi.run(t, "", "uninstall", "--yes")
	if code != exitOK || !words.MatchString(out+errOut) || !there(fi.exe) {
		t.Fatalf("the step looks for %q; exit %d: %s%s", words, code, out, errOut)
	}
}

func TestTheWorkflowTellsWhatAStandardUsersCheckSeesByTheWordsTheProgramSays(t *testing.T) {
	// The same check has the standard user run check, and looks for the
	// hook said as installed and for no finding that someone can change
	// its program: the two phrases as check says them.
	found := workflowWords(t, "installers.yml", "if ($said -notmatch '")
	changeable := workflowWords(t, "installers.yml", "' -or $said -match '")
	a, _ := pc(t)
	checkPC(t, a, "[Logoff]\r\n0CmdLine=C:\\Tools\\wsl-spawnerw.exe\r\n0Parameters=logoff\r\n")
	out, _, _ := cli(t, a, false, "check")
	if !found.MatchString(out) || changeable.MatchString(out) {
		t.Fatalf("the step looks for %q and for no %q in\n%s", found, changeable, out)
	}
	// With someone who can change the program, the finding has the words
	// the step looks for.
	a.PC.WhoElseCanChange = func(string, bool) (string, string, error) { return `BUILTIN\Users`, `C:\Tools`, nil }
	if out, _, _ = cli(t, a, false, "check"); !changeable.MatchString(out) {
		t.Fatalf("the step looks for %q in\n%s", changeable, out)
	}
}
