package gpo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// What a change of the policy does with its two files, the same on every
// system: on Windows, change gives it the policy's own; tests give it files
// of their own, and a writer that fails where they need one to.

// file is a policy file as it was before a change: its bytes, or none.
type file struct {
	path  string
	b     []byte
	there bool
}

// readFile reads the file at p; there is false when there is none.
func readFile(p string) (file, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return file{path: p}, nil
	}
	if err != nil {
		return file{}, err
	}
	return file{path: p, b: b, there: true}, nil
}

// named is err with path in place of the file's short name, when it is an
// *Unsure: the person reads where the file is.
func named(err error, path string) error {
	var u *Unsure
	if errors.As(err, &u) {
		return &Unsure{File: path, Why: u.Why}
	}
	return err
}

// changeFiles rewrites the scripts.ini at scripts with edit and counts the
// change in the gpt.ini at gpt, writing through write. Both files are read,
// and both changes worked out, before either is written: a file that cannot
// be changed for certain stops it with nothing written. Each file is kept
// as it was before it is first changed. A write that fails puts back what it
// wrote over, and a gpt.ini that cannot be written puts scripts.ini back as
// it was too: the policy changes whole or not at all. made is told of a
// scripts.ini the change made, for the marks the Group Policy editor gives
// such a file.
func changeFiles(scripts, gpt string, edit func(string) (string, error), write func(string, []byte, os.FileMode) error, made func(string)) error {
	oldScripts, err := readFile(scripts)
	if err != nil {
		return err
	}
	text, err := decodeScripts(oldScripts.b)
	if err != nil {
		return named(err, scripts)
	}
	newText, err := edit(text)
	if err != nil {
		return named(err, scripts)
	}
	// The scripts as they are already: nothing to write, nothing to count.
	if newText == text {
		return nil
	}
	oldGpt, err := readFile(gpt)
	if err != nil {
		return err
	}
	newGpt, err := Bumped(oldGpt.b)
	if err != nil {
		return named(err, gpt)
	}
	// The person's files as they were, before anything is written.
	for _, f := range []file{oldScripts, oldGpt} {
		if err := keepOnce(f, write); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(scripts), 0o755); err != nil {
		return err
	}
	if err := write(scripts, encodeScripts(newText), 0o644); err != nil {
		return putBack(fmt.Errorf("writing %s: %w", scripts, err), write, oldScripts)
	}
	if !oldScripts.there {
		made(scripts)
	}
	if err := write(gpt, newGpt, 0o644); err != nil {
		// Without its count, Windows may not read the change: neither stays.
		return putBack(fmt.Errorf("writing %s: %w", gpt, err), write, oldGpt, oldScripts)
	}
	return nil
}

// putBack puts each file back as it was after err, through write, and says
// whether that worked: the change is then as if never made, or the error
// says which files are left and, for each, the copy that keeps it as it was
// before wsl-spawner first changed it.
func putBack(err error, write func(string, []byte, os.FileMode) error, fs ...file) error {
	var failed []string
	for _, f := range fs {
		// A write that failed before it changed a byte left nothing to put
		// back.
		if cur, err := os.ReadFile(f.path); f.there && err == nil && bytes.Equal(cur, f.b) {
			continue
		}
		var perr error
		if f.there {
			perr = write(f.path, f.b, 0o644)
		} else if perr = os.Remove(f.path); errors.Is(perr, os.ErrNotExist) {
			// Never made: nothing to take back.
			perr = nil
		}
		if perr != nil {
			failed = append(failed, f.path)
		}
	}
	if len(failed) > 0 {
		// Each file left is named with its own copy: both may be left, and
		// each copy holds only its own file.
		copies := make([]string, len(failed))
		for i, p := range failed {
			copies[i] = p + ".before-wsl-spawner"
		}
		// One file, or two: the words agree with how many.
		they, them, were, keep := "it", "it", "was", "keeps"
		if len(failed) > 1 {
			they, them, were, keep = "they", "them", "were", "keep"
		}
		// The copy is made at the first change only: it holds the file as
		// it was before wsl-spawner ever changed it, the person's own.
		return fmt.Errorf("%w; %s could not be put back as %s %s: %s %s %s as %s %s before wsl-spawner first changed %s",
			err, strings.Join(failed, " and "), they, were, strings.Join(copies, " and "), keep, them, they, were, them)
	}
	return fmt.Errorf("%w; the policy is as it was", err)
}

// keepOnce copies a file, as it was, to *.before-wsl-spawner beside it the
// first time wsl-spawner changes it: the person's own stays at hand. A file
// that was not there is kept as an empty one, which reads as no scripts, so
// a later change does not keep wsl-spawner's own file as the one from
// before. No change goes on without it.
func keepOnce(f file, write func(string, []byte, os.FileMode) error) error {
	keep := f.path + ".before-wsl-spawner"
	if _, err := os.Lstat(keep); err == nil {
		// Kept already, at the first change: it stays the one from before.
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		return err
	}
	// A file that was not there has no bytes: the copy is empty.
	if err := write(keep, f.b, 0o644); err != nil {
		return fmt.Errorf("keeping %s as it was: %w", f.path, err)
	}
	return nil
}
