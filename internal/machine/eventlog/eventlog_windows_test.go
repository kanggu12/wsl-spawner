//go:build windows

package eventlog

import (
	"testing"
	"time"
)

// These read this PC's own logs, and only read them, as any user signed in
// may: what they hold differs from PC to PC, so only how the read goes is
// checked.

func TestTheQueriesAreOnesWindowsTakes(t *testing.T) {
	// An hour still to come holds no event: a query Windows did not take
	// would fail here, whatever the logs hold.
	from := time.Now().Add(time.Hour)
	es, err := EndingsBetween(from, from.Add(time.Hour))
	if err != nil || len(es) != 0 {
		t.Fatalf("got %v, %v", es, err)
	}
}

func TestTheLastWeeksReadInTime(t *testing.T) {
	to := time.Now()
	from := to.Add(-14 * 24 * time.Hour)
	began := time.Now()
	es, err := EndingsBetween(from, to)
	// A log as full as a read stops at is said, not read in part.
	if err == ErrTooMany {
		t.Skip("more events than one read takes in these weeks")
	}
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took > readTime+time.Second {
		t.Fatalf("took %v", took)
	}
	for _, e := range es {
		// A crash's moment comes before the boot that logged it, which may
		// be before the weeks read began.
		known := map[Kind]bool{Crash: true, Update: true, Shutdown: true, Signout: true, Boot: true}
		if e.At.After(to) || e.At.Before(from.Add(-7*24*time.Hour)) || !known[e.Kind] {
			t.Errorf("an ending out of place: %+v", e)
		}
	}
}
