package gpo

import (
	"errors"
	"testing"
	"time"
)

// memStore is a WaitStore held here: the policy's wait and the note.
type memStore struct {
	wait    Wait
	note    *Raised
	failSet bool
}

func (m *memStore) Wait() (Wait, error) { return m.wait, nil }
func (m *memStore) SetWait(w Wait) error {
	if m.failSet {
		return errors.New("access is denied")
	}
	m.wait = w
	return nil
}
func (m *memStore) Note() (Raised, bool, error) {
	if m.note == nil {
		return Raised{}, false, nil
	}
	return *m.note, true, nil
}
func (m *memStore) SetNote(r *Raised) error {
	if r == nil {
		m.note = nil
		return nil
	}
	n := *r
	m.note = &n
	return nil
}

func TestWaitForIsTheStopLimitAMinuteAndOneForEachOtherScript(t *testing.T) {
	for _, c := range []struct {
		limit  time.Duration
		others int
		want   int
	}{
		{300 * time.Second, 0, 360},
		{300 * time.Second, 2, 480},
		// Up to the whole minute.
		{301 * time.Second, 0, 420},
		{3600 * time.Second, 0, 3660},
		// No more than the policy takes.
		{3600 * time.Second, 600, MaxWaitSeconds},
		{10 * time.Second, -1, 120},
	} {
		if got := WaitFor(c.limit, c.others); got != c.want {
			t.Errorf("%v, %d others: got %d, want %d", c.limit, c.others, got, c.want)
		}
	}
}

func TestShortIsAsTheCheckJudgesIt(t *testing.T) {
	limit := 300 * time.Second
	for effective, want := range map[int]bool{0: false, 359: true, 360: false, 600: false, 300: true} {
		if got := Short(effective, limit); got != want {
			t.Errorf("%d s: got %v", effective, got)
		}
	}
}

func TestRaiseNotesWhatWasThereAndPutBackPutsItBack(t *testing.T) {
	// None set: raised, then taken out again, for Windows' own.
	s := &memStore{}
	r, err := Raise(s, 420)
	if err != nil || r.To != 420 || r.Before.Set || !s.wait.Set || s.wait.Seconds != 420 || s.note == nil {
		t.Fatalf("got %+v %v, store %+v", r, err, s)
	}
	if got, ok := StillRaised(s); !ok || got.To != 420 {
		t.Fatalf("still raised: %+v %v", got, ok)
	}
	back, done, err := PutBack(s)
	if err != nil || !done || back.Set || s.wait.Set || s.note != nil {
		t.Fatalf("put back %+v %v %v, store %+v", back, done, err, s)
	}
	// One set: back to it.
	s = &memStore{wait: Wait{Seconds: 300, Set: true}}
	Raise(s, 420)
	if back, done, _ := PutBack(s); !done || !back.Set || back.Seconds != 300 || s.wait.Seconds != 300 {
		t.Fatalf("put back %+v %v, store %+v", back, done, s)
	}
}

func TestRaisedTwiceKeepsWhatWasThereFirst(t *testing.T) {
	s := &memStore{wait: Wait{Seconds: 300, Set: true}}
	Raise(s, 420)
	Raise(s, 480)
	if s.note.Before.Seconds != 300 || s.wait.Seconds != 480 {
		t.Fatalf("store %+v %+v", s.wait, *s.note)
	}
	PutBack(s)
	if s.wait.Seconds != 300 {
		t.Fatalf("back to %+v", s.wait)
	}
}

func TestAWaitChangedSinceStaysAndTheNoteGoes(t *testing.T) {
	s := &memStore{}
	Raise(s, 420)
	// Someone set another since: it is theirs.
	s.wait = Wait{Seconds: 900, Set: true}
	if _, ok := StillRaised(s); ok {
		t.Fatal("still raised by wsl-spawner")
	}
	back, done, err := PutBack(s)
	if err != nil || done || back.Seconds != 900 || s.wait.Seconds != 900 || s.note != nil {
		t.Fatalf("put back %+v %v %v, store %+v", back, done, err, s)
	}
	// A raise noted over a wait someone changed since starts from theirs.
	s = &memStore{wait: Wait{Seconds: 300, Set: true}}
	Raise(s, 420)
	s.wait = Wait{Seconds: 400, Set: true}
	Raise(s, 480)
	if s.note.Before.Seconds != 400 {
		t.Fatalf("note %+v", *s.note)
	}
}

func TestNothingToPutBackWithoutANote(t *testing.T) {
	s := &memStore{wait: Wait{Seconds: 1200, Set: true}}
	if back, done, err := PutBack(s); err != nil || done || back != (Wait{}) || s.wait.Seconds != 1200 {
		t.Fatalf("got %+v %v %v", back, done, err)
	}
}

func TestARaiseThatCannotSetTheWaitLeavesItsNote(t *testing.T) {
	// The note comes first: a wait that cannot be set is said, and a note
	// over a wait not changed puts nothing back.
	s := &memStore{wait: Wait{Seconds: 300, Set: true}, failSet: true}
	if _, err := Raise(s, 420); err == nil {
		t.Fatal("no error")
	}
	s.failSet = false
	if back, done, _ := PutBack(s); done || back.Seconds != 300 || s.note != nil {
		t.Fatalf("put back %+v %v, store %+v", back, done, s)
	}
}

func TestEffectiveIsWindowsOwnWhereNoneIsSet(t *testing.T) {
	if (Wait{}).Effective() != DefaultWait || (Wait{Seconds: 0, Set: true}).Effective() != 0 || (Wait{Seconds: 1200, Set: true}).Effective() != 1200 {
		t.Fatal("effective")
	}
}
