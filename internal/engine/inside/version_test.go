package inside

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// startScripts are the start scripts that were cut, by version, with the
// sha256 of each: a copy in a distribution says its version on its first
// line, and a check tells an older one from this by that line alone. A
// script that is changed is therefore a new version: raise StartScriptVersion,
// make the first line of start.sh say it, and add the new hash here. Version 1
// is what 0.1.0 and 0.1.1 put in distributions, version 2 what 0.1.2 put.
var startScripts = map[string]string{
	"1": "13cdcf88a258e6c2b801d2b08318c8f2a883d28a811ec230ab88922e13e5fc7f",
	"2": "080bd11b031952d0c1095b395835047deefd0dbb5b0df577410ca4b3d5180e14",
	"3": "9150dfa4a9d02ad7fb90c50eda84dad29c37e8f4a06f8f9974391c09f5123702",
}

func TestTheStartScriptsVersionMovesWithItsContent(t *testing.T) {
	sum := sha256.Sum256([]byte(startScript))
	got := hex.EncodeToString(sum[:])
	want, ok := startScripts[StartScriptVersion]
	if !ok {
		t.Fatalf("version %s of the start script is not in startScripts: add it with %s", StartScriptVersion, got)
	}
	if got != want {
		t.Fatalf("start.sh changed under version %s (sha256 %s, not %s): a copy in a distribution would not be told from this one. Raise StartScriptVersion, make the first line say it, and add the hash to startScripts", StartScriptVersion, got, want)
	}
	// The first line names the version, as a distribution's copy says it.
	if first, _, _ := strings.Cut(startScript, "\n"); first != StartScriptMark+StartScriptVersion {
		t.Fatalf("start.sh's first line is %q, not %q", first, StartScriptMark+StartScriptVersion)
	}
	// No version is cut twice, and none is newer than the one in use.
	seen := map[string]string{}
	for v, h := range startScripts {
		if other, dup := seen[h]; dup {
			t.Errorf("versions %s and %s have one hash", v, other)
		}
		seen[h] = v
	}
}
