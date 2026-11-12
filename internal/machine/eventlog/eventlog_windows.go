//go:build windows

package eventlog

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows' event log API, which reads the logs without a program of its
// own: no console flashes, whatever this program's build.
var (
	wevtapi       = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtQuery  = wevtapi.NewProc("EvtQuery")
	procEvtNext   = wevtapi.NewProc("EvtNext")
	procEvtRender = wevtapi.NewProc("EvtRender")
	procEvtClose  = wevtapi.NewProc("EvtClose")
)

// The API's flags: the query names a log by its channel and reads it
// oldest first; an event is rendered as its XML.
const (
	evtQueryChannelPath      = 0x1
	evtQueryForwardDirection = 0x100
	evtRenderEventXML        = 1
)

// Limits of one read: so many events at most from each log, and so long
// for all of them. A read cut short by either is an error: what it misses
// may be what ended the distributions.
const (
	maxEvents = 512
	readTime  = 3 * time.Second
)

// EndingsBetween reads the events of the System and Application logs that
// end WSL's distributions, logged from 'from' to 'to', as Classify knows
// them (queries): the sign-outs of the user this process runs as. The whole
// read waits readTime at most, whatever call of Windows' takes long, and
// reads maxEvents of each log at most: a read cut short is an error, as
// what it misses may be what ended them.
func EndingsBetween(from, to time.Time) ([]Ending, error) {
	deadline := time.Now().Add(readTime)
	user := ""
	if u, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		user = u.User.Sid.String()
	}
	return within(readTime, func() ([]Ending, error) {
		var out []Ending
		for _, q := range queries(from, to, user) {
			evs, err := read(q.channel, q.xpath, deadline)
			if err != nil {
				return nil, fmt.Errorf("reading the %s log: %w", q.channel, err)
			}
			for _, e := range evs {
				if en, ok := Classify(e); ok {
					out = append(out, en)
				}
			}
		}
		return out, nil
	})
}

// read reads the events of channel that xpath picks, by deadline.
func read(channel, xpath string, deadline time.Time) ([]Event, error) {
	// The API is Windows' own; without it there is nothing to read.
	if err := wevtapi.Load(); err != nil {
		return nil, err
	}
	ch, err := windows.UTF16PtrFromString(channel)
	if err != nil {
		return nil, err
	}
	q, err := windows.UTF16PtrFromString(xpath)
	if err != nil {
		return nil, err
	}
	// No session: this PC's own logs, as the user running this may read them.
	h, _, callErr := procEvtQuery.Call(0, uintptr(unsafe.Pointer(ch)), uintptr(unsafe.Pointer(q)), evtQueryChannelPath|evtQueryForwardDirection)
	if h == 0 {
		return nil, callErr
	}
	defer procEvtClose.Call(h)
	var events []Event
	// The handles of the events each call gives, a few at a time.
	handles := make([]uintptr, 32)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return nil, ErrSlow
		}
		var got uint32
		r, _, callErr := procEvtNext.Call(h, uintptr(len(handles)), uintptr(unsafe.Pointer(&handles[0])),
			uintptr(left.Milliseconds()), 0, uintptr(unsafe.Pointer(&got)))
		if r == 0 {
			switch {
			case errors.Is(callErr, windows.ERROR_NO_MORE_ITEMS):
				// Every event the query picks is read.
				return events, nil
			case errors.Is(callErr, windows.ERROR_TIMEOUT):
				return nil, ErrSlow
			}
			return nil, callErr
		}
		// Each handle is rendered, then closed, whatever the rendering gives.
		for _, eh := range handles[:got] {
			x, rerr := render(eh)
			procEvtClose.Call(eh)
			if rerr != nil {
				err = rerr
				continue
			}
			// An event that does not parse is not one Classify knows.
			if e, perr := Parse(x); perr == nil {
				events = append(events, e)
			}
		}
		if err != nil {
			return nil, err
		}
		if len(events) > maxEvents {
			return nil, ErrTooMany
		}
	}
}

// render is the XML of the event at h.
func render(h uintptr) (string, error) {
	var used, props uint32
	// Asked with no room first: the answer is how many bytes it needs.
	r, _, err := procEvtRender.Call(0, h, evtRenderEventXML, 0, 0, uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&props)))
	if r == 0 && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return "", err
	}
	// The XML is UTF-16, ending with a zero; one more unit leaves room.
	buf := make([]uint16, used/2+1)
	r, _, err = procEvtRender.Call(0, h, evtRenderEventXML, uintptr(len(buf)*2), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&props)))
	if r == 0 {
		return "", err
	}
	return windows.UTF16ToString(buf), nil
}
