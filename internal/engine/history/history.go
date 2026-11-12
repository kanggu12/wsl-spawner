// Package history keeps what wsl-spawner did, one JSON object a line in
// history.jsonl beside its other files: each stop, by hand or as Windows
// signed out, each start at sign-in, with how every distribution went.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// maxEntries is how many entries the file keeps: past that, the oldest go.
const maxEntries = 500

// maxLine is the longest line read; a longer one is no entry of this
// program's, and is skipped.
const maxLine = 64 << 10

// Entry is one thing wsl-spawner did.
type Entry struct {
	// At is when it started; Took how long it took, in seconds.
	At   time.Time `json:"at"`
	Took float64   `json:"took_seconds"`
	// What is "stop" or "start".
	What string `json:"what"`
	// By says who asked: "hand" (a command or the screen), "signout" (the
	// Windows sign-out hook) or "logon" (the start at sign-in).
	By string `json:"by"`
	// Shutdown is set when WSL itself was stopped after the distributions.
	Shutdown bool `json:"shutdown,omitempty"`
	// Distros is how each distribution went.
	Distros []Distro `json:"distributions"`
	// Err is what went wrong around them, as WSL not answering.
	Err string `json:"error,omitempty"`
	// Version is the program's version that did it.
	Version string `json:"version,omitempty"`
}

// Distro is how one distribution went.
type Distro struct {
	Name string `json:"name"`
	// Result is a stop's "clean", "limit", "failed" or "error" (its steps
	// did not run to their end), or a start's "started" or "error".
	Result string  `json:"result"`
	Took   float64 `json:"took_seconds"`
	// Steps are a stop's steps inside the distribution, in order.
	Steps []Step `json:"steps,omitempty"`
	// Err says what went wrong.
	Err string `json:"error,omitempty"`
}

// Late is the step a stop's result names: the first that ran out of time,
// for one out of time; the first that failed otherwise, for one with a step
// failed. False for any other result, or a stop whose steps were not kept.
func (d Distro) Late() (Step, bool) {
	var failed *Step
	for i, s := range d.Steps {
		switch {
		case d.Result == "limit" && s.Code == 124:
			return s, true
		case d.Result == "failed" && s.Code != 0 && s.Code != 124:
			return s, true
		case d.Result == "failed" && s.Code != 0 && failed == nil:
			failed = &d.Steps[i]
		}
	}
	if failed != nil {
		return *failed, true
	}
	return Step{}, false
}

// Step is one step of a stop.
type Step struct {
	// Step is its name: "hook", "units", "docker", "podman", "sync"; Hook
	// the hook's file name.
	Step string `json:"step"`
	Hook string `json:"hook,omitempty"`
	// Count is how many containers or units it stopped.
	Count int `json:"count,omitempty"`
	// Code is its exit code; 124 ran out of time.
	Code int     `json:"code"`
	Took float64 `json:"took_seconds"`
}

// Store is the history file.
type Store struct{ Path string }

// Open returns the store in dir (created on the first write).
func Open(dir string) Store { return Store{Path: filepath.Join(dir, "history.jsonl")} }

// writing is the file a write is made in before it takes the history's
// place. Only one program at a time can make it, so it is also what keeps
// two programs adding at once from each writing the file without the
// other's entry; it goes as its write takes the history's place.
const writing = "history-new.tmp"

// How long an Add waits for another program's write to end, and how old
// a write's file is when a program that ended halfway left it: a write
// takes a moment, and the file's time is when it was made or written.
const (
	writeWait = 15 * time.Second
	leftAfter = 10 * time.Second
	// renameWait is how long a write's file waits to take the history's
	// place: Windows refuses the rename while a program reads the history,
	// which takes a moment.
	renameWait = 5 * time.Second
)

// errBusy is a write of another program's that did not end in time.
var errBusy = errors.New("another program is writing the history")

// begin makes the write's file, once no other program's write has it.
func (s Store) begin() (*os.File, error) {
	p := filepath.Join(filepath.Dir(s.Path), writing)
	deadline := time.Now().Add(writeWait)
	for {
		// Made only where it is not: one program's at a time.
		f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			// On Windows the file of a write that just ended may still be on
			// its way out: its name stays taken a moment, and making it again
			// is refused as access denied. That is waited for as a write
			// under way is.
			if runtime.GOOS != "windows" || !errors.Is(err, fs.ErrPermission) || time.Now().After(deadline) {
				return nil, err
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		// One a program that ended halfway left goes. Windows refuses to
		// remove one a program still has open, which is never taken over.
		// One dated well ahead was left before the clock was set back: a
		// write's file is dated as it is made.
		if fi, err := os.Stat(p); err == nil {
			if age := time.Since(fi.ModTime()); age > leftAfter || age < -leftAfter {
				_ = os.Remove(p)
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, errBusy
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Add appends an entry, dropping the oldest past maxEntries. Programs that
// add at once take turns: each adds to what the one before it wrote.
func (s Store) Add(e Entry) error {
	// A stop with nothing to stop has no distributions: an empty list, as
	// every list in the commands' JSON is, never null.
	if e.Distros == nil {
		e.Distros = []Distro{}
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	// Write a new file and rename it over the old one, so a crash leaves
	// one whole file or the other.
	f, err := s.begin()
	if err != nil {
		return err
	}
	tmp := f.Name()
	// Whatever goes wrong below, the write's file does not stay behind, and
	// the next program's write can begin.
	done := false
	defer func() {
		if !done {
			f.Close()
			os.Remove(tmp)
		}
	}()
	// Read with this write begun: no other comes between. A history that
	// cannot be read is left as it is: rewriting it from what was read
	// would drop every entry past the problem.
	lines, err := s.lines()
	if err != nil {
		return err
	}
	lines = append(lines, line)
	if len(lines) > maxEntries {
		lines = lines[len(lines)-maxEntries:]
	}
	w := bufio.NewWriter(f)
	for _, l := range lines {
		// Each line as it was read, its line break after it.
		if _, err := w.Write(l); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	// On the drive before the rename, so a power cut never leaves the new
	// name on an empty file.
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := renameSoon(tmp, s.Path); err != nil {
		os.Remove(tmp)
		done = true
		return err
	}
	done = true
	return nil
}

// openSoon opens the history to read it, trying again for a few seconds
// while it is refused: on Windows a write's file takes the history's place
// with a handle that, for a moment after, keeps any other from opening it.
// A history that is not there is said at once.
func openSoon(path string) (*os.File, error) {
	deadline := time.Now().Add(renameWait)
	for {
		f, err := os.Open(path)
		if err == nil || os.IsNotExist(err) || runtime.GOOS != "windows" || time.Now().After(deadline) {
			return f, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// removeSoon removes the history, trying again for a few seconds on
// Windows while a program reading it keeps it from going.
func removeSoon(path string) error {
	deadline := time.Now().Add(renameWait)
	for {
		err := os.Remove(path)
		if err == nil || os.IsNotExist(err) || runtime.GOOS != "windows" || time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// renameSoon renames from over to, trying again for a few seconds while it
// is refused: on Windows a program reading the history holds it open, and
// Go opens a file so that it cannot be replaced meanwhile. A reader lets go
// in a moment; an entry is not lost to it.
func renameSoon(from, to string) error {
	deadline := time.Now().Add(renameWait)
	for {
		err := os.Rename(from, to)
		if err == nil || runtime.GOOS != "windows" || time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Clear removes every run: the file, and a temporary one a write that was
// cut short left beside it. No history is nothing to clear, not an error.
func (s Store) Clear() error {
	if err := removeSoon(s.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Add removes its temporary file itself; only a crash leaves one.
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Path), "history-*.tmp"))
	for _, p := range left {
		_ = os.Remove(p)
	}
	return nil
}

// lines returns the file's lines as they are, oldest first, without their
// line breaks: those this version cannot read too, cut short or of a newer
// version, which stay as they are. Empty lines and lines longer than any
// entry of this program's are none, and go.
func (s Store) lines() ([][]byte, error) {
	f, err := openSoon(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out [][]byte
	r := bufio.NewReaderSize(f, maxLine)
	for {
		// A longer line comes in pieces, all of which are dropped, as All
		// drops them.
		line, err := r.ReadSlice('\n')
		long := errors.Is(err, bufio.ErrBufferFull)
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = r.ReadSlice('\n')
		}
		// A copy: the reader's buffer is used again for the next line.
		if t := bytes.TrimSpace(line); !long && len(t) > 0 {
			out = append(out, bytes.Clone(t))
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// All returns every entry, oldest first. A missing file is no history; a
// damaged line, however long, is skipped.
func (s Store) All() ([]Entry, error) {
	f, err := openSoon(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Entry
	r := bufio.NewReaderSize(f, maxLine)
	for {
		// ReadSlice returns at most a buffer's worth: a longer line comes in
		// pieces, all of which are dropped.
		line, err := r.ReadSlice('\n')
		long := errors.Is(err, bufio.ErrBufferFull)
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = r.ReadSlice('\n')
		}
		if !long {
			var e Entry
			if t := bytes.TrimSpace(line); len(t) > 0 && json.Unmarshal(t, &e) == nil {
				// An entry an earlier version wrote with no list gets an
				// empty one.
				if e.Distros == nil {
					e.Distros = []Distro{}
				}
				out = append(out, e)
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}
