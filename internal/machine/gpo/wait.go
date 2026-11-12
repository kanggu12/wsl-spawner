package gpo

import "time"

// Windows waits for the sign-out scripts, all of them together, as long as
// the policy "Specify maximum wait time for Group Policy scripts" says
// (MaxGPOScriptWait), or 600 seconds where nothing sets it; 0 waits as long
// as they take. Past it, Windows goes on without them. wsl-spawner's hook,
// the last of them, gets what the others leave. hook install raises a wait
// too short for it, and notes what was there; hook remove puts that back,
// as long as the wait is still the one it set. The rules are here; the
// registry, which needs an administrator to change, is the Windows file's.

// DefaultWait is how long Windows waits where no policy sets it.
const DefaultWait = 600

// MaxWaitSeconds is the most the policy takes, as its editor bounds it.
const MaxWaitSeconds = 32000

// Wait is the policy's wait as the registry holds it: Seconds when Set,
// Windows' own DefaultWait when not.
type Wait struct {
	Seconds int
	Set     bool
}

// Effective is how long Windows waits by w: its seconds, or Windows' own.
func (w Wait) Effective() int {
	if !w.Set {
		return DefaultWait
	}
	return w.Seconds
}

// Raised is wsl-spawner's note of a wait it raised: To is what it set,
// Before what was there, which hook remove puts back.
type Raised struct {
	To     int
	Before Wait
}

// WaitFor is how long the sign-out scripts need, wsl-spawner's hook last
// among them: its stop_limit, a minute for WSL to end after it, and a minute
// for each other sign-out script, which Windows' one wait holds too. It is
// whole minutes, and no more than the policy takes.
func WaitFor(stopLimit time.Duration, others int) int {
	need := stopLimit + time.Minute + time.Duration(max(others, 0))*time.Minute
	// Up to the whole minute.
	secs := int((need + time.Minute - 1) / time.Minute * 60)
	return min(secs, MaxWaitSeconds)
}

// Short reports whether a wait of effective seconds (0: as long as they
// take) is too short for stopLimit, as check judges it: less than it and a
// minute for WSL to end.
func Short(effective int, stopLimit time.Duration) bool {
	return effective != 0 && time.Duration(effective)*time.Second < stopLimit+time.Minute
}

// WaitStore is where the policy's wait and wsl-spawner's note of it are
// kept: the registry, or a test's own.
type WaitStore interface {
	// Wait is the policy's wait; SetWait sets it, or with Set false takes
	// it out, leaving Windows' own.
	Wait() (Wait, error)
	SetWait(Wait) error
	// Note is wsl-spawner's note, ok false when there is none; SetNote
	// writes it, or with nil takes it out.
	Note() (r Raised, ok bool, err error)
	SetNote(*Raised) error
}

// Raise sets the wait to seconds, and notes what was there first: the note
// comes first, so a wait set is never left without it. A wait raised before
// and not changed since keeps the note's own Before, what was there before
// wsl-spawner ever raised it.
func Raise(s WaitStore, seconds int) (Raised, error) {
	cur, err := s.Wait()
	if err != nil {
		return Raised{}, err
	}
	r := Raised{To: seconds, Before: cur}
	if old, ok, err := s.Note(); err != nil {
		return Raised{}, err
	} else if ok && cur.Set && cur.Seconds == old.To {
		r.Before = old.Before
	}
	if err := s.SetNote(&r); err != nil {
		return Raised{}, err
	}
	return r, s.SetWait(Wait{Seconds: seconds, Set: true})
}

// PutBack puts back the wait wsl-spawner raised, where it is still the one
// it set: back is what Windows waits then, and done whether it changed. A
// wait someone changed since is theirs, and stays. The note goes either way.
func PutBack(s WaitStore) (back Wait, done bool, err error) {
	r, ok, err := s.Note()
	if err != nil || !ok {
		return Wait{}, false, err
	}
	cur, err := s.Wait()
	if err != nil {
		return Wait{}, false, err
	}
	back = cur
	if cur.Set && cur.Seconds == r.To {
		if err := s.SetWait(r.Before); err != nil {
			return Wait{}, false, err
		}
		back, done = r.Before, true
	}
	return back, done, s.SetNote(nil)
}

// StillRaised is the note of a wait wsl-spawner raised that is still the one it
// set: what hook remove would put back. ok is false for none.
func StillRaised(s WaitStore) (Raised, bool) {
	r, ok, err := s.Note()
	if err != nil || !ok {
		return Raised{}, false
	}
	cur, err := s.Wait()
	if err != nil || !cur.Set || cur.Seconds != r.To {
		return Raised{}, false
	}
	return r, true
}
