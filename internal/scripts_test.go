package internal

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kanggu12/wsl-spawner/internal/testutil/shtest"
)

// releaseLines are the release workflow's lines.
func releaseLines(t *testing.T) []string {
	t.Helper()
	root, _ := moduleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
}

// publishScript is the script of the release workflow's publish step, as
// its run block holds it: the lines indented under "run: |", that indent
// taken off.
func publishScript(t *testing.T) string {
	t.Helper()
	ls := releaseLines(t)
	i := slices.Index(ls, "      - name: publish")
	if i < 0 {
		t.Fatal("release.yml has no publish step")
	}
	j := slices.Index(ls[i:], "        run: |")
	if j < 0 {
		t.Fatal("the publish step has no run block")
	}
	var script []string
	for _, l := range ls[i+j+1:] {
		// The block ends at the first line indented less than it.
		if l != "" && !strings.HasPrefix(l, "          ") {
			break
		}
		script = append(script, strings.TrimPrefix(l, "          "))
	}
	return strings.Join(script, "\n")
}

func TestARunOfTheReleaseStartedAgainNeverMixesTwoBuilds(t *testing.T) {
	// A run started again after the release went out finishes it. Once
	// checksums.txt or the Scoop manifest is out, people check the zips
	// against it: a zip missing there would be this run's, built again,
	// maybe with a newer Go, and not the one they list. Nothing goes up
	// then; otherwise what is missing goes up, as before.
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash, which the workflow's steps run in")
	}
	script := publishScript(t)
	zips := []string{"wsl-spawner-9.9.9-windows-amd64.zip", "wsl-spawner-9.9.9-windows-arm64.zip"}
	all := append(slices.Clone(zips), "checksums.txt", "install.ps1", "wsl-spawner.json")
	for _, tc := range []struct {
		name string
		// have is what the release holds already.
		have []string
		ok   bool
		// uploaded is what goes up, in order.
		uploaded []string
	}{
		{"a zip missing beside the checksums", []string{zips[0], "checksums.txt", "install.ps1", "wsl-spawner.json"}, false, nil},
		{"a zip missing beside the manifest", []string{zips[0], "install.ps1", "wsl-spawner.json"}, false, nil},
		{"the checksums and the manifest missing", []string{zips[0]}, true, []string{zips[1], "checksums.txt", "install.ps1", "wsl-spawner.json"}},
		{"the install script missing", []string{zips[0], zips[1], "checksums.txt", "wsl-spawner.json"}, true, []string{"install.ps1"}},
		{"nothing missing", all, true, nil},
	} {
		work := t.TempDir()
		// The files a run made, in dist/.
		if err := os.MkdirAll(filepath.Join(work, "dist"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range all {
			if err := os.WriteFile(filepath.Join(work, "dist", f), []byte(f), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// A made-up gh: the release is there, with what it holds; each
		// upload is noted.
		dir := shtest.Tools(t, "grep")
		have := filepath.Join(work, "have")
		if err := os.WriteFile(have, []byte(strings.Join(tc.have, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		uploaded := filepath.Join(work, "uploaded")
		shtest.Stub(t, dir, "gh", `case "$1 $2" in
'release view') case "$*" in *--json*) while IFS= read -r l; do printf '%s\n' "$l"; done <'`+have+`' ;; esac ;;
'release upload') printf '%s\n' "${4##*/}" >>'`+uploaded+`' ;;
*) echo "gh: not expected: $*" >&2; exit 64 ;;
esac`)
		cmd := exec.Command(bash, "--noprofile", "--norc", "-eo", "pipefail", "-c", script)
		cmd.Dir = work
		cmd.Env = []string{"PATH=" + dir, "GITHUB_REF_NAME=v9.9.9", "RUNNER_TEMP=" + work}
		out, err := cmd.CombinedOutput()
		if (err == nil) != tc.ok {
			t.Errorf("%s: %v\n%s", tc.name, err, out)
		}
		b, _ := os.ReadFile(uploaded)
		var got []string
		if s := strings.TrimSpace(string(b)); s != "" {
			got = strings.Split(s, "\n")
		}
		if !slices.Equal(got, tc.uploaded) {
			t.Errorf("%s: uploaded %q\n%s", tc.name, got, out)
		}
	}
}

func TestTheDemoImageWaitsForTheRelease(t *testing.T) {
	// A release that fails its own checks pushes no demo image, :latest
	// among its tags.
	ls := releaseLines(t)
	i := slices.Index(ls, "  demo-image:")
	if i < 0 {
		t.Fatal("release.yml has no demo-image job")
	}
	for _, l := range ls[i+1:] {
		if v, ok := strings.CutPrefix(l, "    needs: "); ok {
			if !strings.Contains(v, "release") {
				t.Fatalf("demo-image needs %s", v)
			}
			return
		}
		// The next job, and no needs on the way.
		if strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "   ") && strings.HasSuffix(l, ":") {
			break
		}
	}
	t.Fatal("demo-image needs nothing")
}

func TestTheInstallScriptIsASCII(t *testing.T) {
	// `irm ... | iex` in Windows PowerShell 5.1 decodes a download that names
	// no character set as Latin-1, and GitHub names none for a release
	// asset: a character outside ASCII would reach the script as two or three
	// others, in a message or in the code itself.
	root, _ := moduleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "scripts", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range bytes.Split(b, []byte("\n")) {
		for _, c := range line {
			if c > 0x7f {
				t.Errorf("scripts/install.ps1:%d holds a character outside ASCII: %q", i+1, line)
				break
			}
		}
	}
}
