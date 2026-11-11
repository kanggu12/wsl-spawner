package config

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"time"
)

// soonWait is how long reading or replacing the settings file tries again
// on Windows, where another program reading it, as the screen does every
// few seconds, keeps it from being replaced, and a replacement just made
// keeps it from being opened, each for a moment.
const soonWait = 5 * time.Second

// readSoon is os.ReadFile, tried again on Windows while it is refused. A
// file that is not there is said at once.
func readSoon(path string) ([]byte, error) {
	deadline := time.Now().Add(soonWait)
	for {
		b, err := os.ReadFile(path)
		if err == nil || errors.Is(err, fs.ErrNotExist) || runtime.GOOS != "windows" || time.Now().After(deadline) {
			return b, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// renameSoon is os.Rename, tried again on Windows while it is refused.
func renameSoon(from, to string) error {
	deadline := time.Now().Add(soonWait)
	for {
		err := os.Rename(from, to)
		if err == nil || runtime.GOOS != "windows" || time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
