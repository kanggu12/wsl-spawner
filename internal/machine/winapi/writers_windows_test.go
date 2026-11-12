package winapi

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// icacls changes the access list of path as Windows' own tool does: a test
// makes the folders it changes itself, under its own temporary folder.
func icacls(t *testing.T, path string, args ...string) {
	t.Helper()
	if out, err := exec.Command("icacls", append([]string{path}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("icacls %s %v: %v\n%s", path, args, err, out)
	}
}

func TestWhoElseCanChangeReadsTheRealAccessLists(t *testing.T) {
	if _, err := exec.LookPath("icacls"); err != nil {
		t.Skip("no icacls here")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "wsl-spawnerw.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Made under this user's own temporary folder: only this user, the
	// system and the administrators reach it.
	if who, at, err := WhoElseCanChange(exe, true); err != nil || who != "" {
		t.Fatalf("a folder of this user's own: %q at %q, %v", who, at, err)
	}
	// For a standard user, whose own folder would run as an administrator,
	// the user is not one to trust: it is their folder that is the way in.
	if who, _, err := WhoElseCanChange(exe, false); err != nil || who == "" {
		t.Fatalf("a folder of the user's own, for one who is not trusted: %q, %v", who, err)
	}
	// Everyone may modify the folder, as Authenticated Users may one made
	// under a drive's root: another user could put a program in its place.
	icacls(t, dir, "/grant", "*S-1-1-0:(OI)(CI)M")
	if who, at, err := WhoElseCanChange(exe, true); err != nil || !strings.Contains(who, "Everyone") || at == "" {
		t.Fatalf("a folder anyone may modify: %q at %q, %v", who, at, err)
	}
	// Only reading and running it: nobody else changes anything.
	icacls(t, dir, "/remove:g", "*S-1-1-0")
	icacls(t, dir, "/grant", "*S-1-1-0:(OI)(CI)RX")
	if who, at, err := WhoElseCanChange(exe, true); err != nil || who != "" {
		t.Fatalf("a folder anyone may only read: %q at %q, %v", who, at, err)
	}
	// Everyone may delete what is in the folder above: the folder could be
	// taken away and made again.
	icacls(t, filepath.Dir(dir), "/grant", "*S-1-1-0:(DC)")
	if who, at, err := WhoElseCanChange(exe, true); err != nil || !strings.Contains(who, "Everyone") || at != filepath.Dir(dir) {
		t.Fatalf("a folder above that anyone may delete in: %q at %q, %v", who, at, err)
	}
	// Creating folders in the folder above, as Users may at a drive's root,
	// changes nothing that stands.
	icacls(t, filepath.Dir(dir), "/remove:g", "*S-1-1-0")
	icacls(t, filepath.Dir(dir), "/grant", "*S-1-1-0:(AD)")
	if who, at, err := WhoElseCanChange(exe, true); err != nil || who != "" {
		t.Fatalf("a folder above where anyone may create folders: %q at %q, %v", who, at, err)
	}
	// A program that is gone: whoever may add a file to the folder it stood
	// in may put one there again.
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	icacls(t, dir, "/grant", "*S-1-1-0:(OI)(CI)M")
	if who, at, err := WhoElseCanChange(exe, true); err != nil || !strings.Contains(who, "Everyone") || at != dir {
		t.Fatalf("a program gone from a folder anyone may modify: %q at %q, %v", who, at, err)
	}
	icacls(t, dir, "/remove:g", "*S-1-1-0")
	if who, at, err := WhoElseCanChange(exe, true); err != nil || who != "" {
		t.Fatalf("a program gone from a folder of this user's own: %q at %q, %v", who, at, err)
	}
}

func TestWhoElseCanChangeFollowsAJunctionToTheFoldersAboveWhereItLeads(t *testing.T) {
	if _, err := exec.LookPath("icacls"); err != nil {
		t.Skip("no icacls here")
	}
	// The program is reached through a junction, as a profile folder moved
	// to another drive is: root\a\j leads to root\p\b. Go's EvalSymlinks
	// goes through no junction, so the folders above where it leads, p
	// here, must be looked at by Windows' own final path.
	root := t.TempDir()
	a, parent := filepath.Join(root, "a"), filepath.Join(root, "p")
	b := filepath.Join(parent, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(b, "wsl-spawnerw.exe"), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := filepath.Join(a, "j")
	if out, err := exec.Command("cmd", "/d", "/c", "mklink", "/J", j, b).CombinedOutput(); err != nil {
		t.Skipf("no junction made here: %v %s", err, out)
	}
	// The junction goes before its target does, so nothing removes what
	// it leads to through it.
	t.Cleanup(func() { _ = os.Remove(j) })
	exe := filepath.Join(j, "wsl-spawnerw.exe")
	if who, at, err := WhoElseCanChange(exe, true); err != nil || who != "" {
		t.Fatalf("folders of this user's own: %q at %q, %v", who, at, err)
	}
	// Everyone may delete what is in p: b could be taken away and made
	// again, with another program in it.
	icacls(t, parent, "/grant", "*S-1-1-0:(DC)")
	// The folder it names is p, as the folder it is and not by its
	// spelling: Windows gives the path a junction leads to its long names,
	// and a temporary folder may be reached by a short one, as RUNNER~1 on
	// GitHub's runners.
	who, at, err := WhoElseCanChange(exe, true)
	if err != nil || !strings.Contains(who, "Everyone") || !sameFile(at, parent) {
		t.Fatalf("a folder above the junction's target that anyone may delete in: %q at %q (p is %q), %v", who, at, parent, err)
	}
}

// sameFile reports whether two paths name one file or folder, however each
// is spelled.
func sameFile(a, b string) bool {
	x, err := os.Stat(a)
	if err != nil {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && os.SameFile(x, y)
}

func TestCanDeleteAsksWithoutDeleting(t *testing.T) {
	if _, err := exec.LookPath("icacls"); err != nil {
		t.Skip("no icacls here")
	}
	dir := filepath.Join(t.TempDir(), "w")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "wsl-spawnerw.exe")
	if err := os.WriteFile(f, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	// This user's own file: they may, and it is still there after.
	if err := CanDelete(f); err != nil {
		t.Fatalf("a file of this user's own: %v", err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("asked, and the file went: %v", err)
	}
	// Only reading it, and nothing but reading in its folder, as an
	// administrator's install under Program Files gives a standard user:
	// they may not. The owner may always set the lists back, for the end.
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := "*" + user.User.Sid.String()
	t.Cleanup(func() {
		icacls(t, dir, "/grant", sid+":(OI)(CI)F")
		icacls(t, f, "/grant", sid+":F")
	})
	icacls(t, dir, "/inheritance:r", "/grant:r", sid+":(RX)")
	icacls(t, f, "/inheritance:r", "/grant:r", sid+":(R)")
	err = CanDelete(f)
	switch {
	case windowsTokenElevated():
		// Not asserted for an elevated process: on GitHub's runners, where
		// the tests run elevated, Windows let this open through though the
		// lists give this user reading alone. Whether an administrator at
		// work may delete is not what an uninstall asks: it asks for the
		// person at a terminal that is not elevated.
		t.Logf("elevated: a file this user may only read: %v", err)
	case !errors.Is(err, fs.ErrPermission):
		t.Fatalf("a file this user may only read: %v", err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("the file went: %v", err)
	}
}

func TestStandardUserSaysWhatThisProcessIs(t *testing.T) {
	// Not asserted either way: this machine's user may be either. It must
	// not fail, and an elevated process is never one.
	got := StandardUser()
	if windowsTokenElevated() && got {
		t.Fatal("an elevated process is taken for a standard user")
	}
}

// windowsTokenElevated reports whether this test runs elevated, as the
// runners' tests do.
func windowsTokenElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }
