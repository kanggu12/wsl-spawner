package cli

import (
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
