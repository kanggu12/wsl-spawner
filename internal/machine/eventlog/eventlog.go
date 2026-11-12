// Package eventlog reads what Windows' event logs say about the moments
// that end WSL's distributions without wsl-spawner's stop: Windows ending
// without a clean shutdown, WSL itself updated, Windows shutting down or
// restarting, and the person signing out; and Windows' boots, which bound
// them. It only reads, as anyone signed in at the PC may: the System and
// Application logs grant interactive users read access. What it finds is a
// hint, not proof: any program a signed-in user runs may write an event of
// Windows Installer's to the Application log.
package eventlog

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind is what an event says ended the distributions.
type Kind string

const (
	// Crash: Windows ended without shutting down cleanly, as a crash, a
	// hang or a power cut leaves it.
	Crash Kind = "crash"
	// Update: WSL itself was updated, which ends every distribution as the
	// new version goes in.
	Update Kind = "update"
	// Shutdown: Windows shut down or restarted.
	Shutdown Kind = "shutdown"
	// Signout: the person signed out, which ends their WSL.
	Signout Kind = "signout"
	// Boot: Windows started. It ends nothing; a crash is logged at the
	// boot after it.
	Boot Kind = "boot"
)

// Ending is one event that ends WSL's distributions: what it says, and
// when it happened.
type Ending struct {
	Kind Kind
	// At is when it happened: the moment the event names, where it names
	// one (an unexpected shutdown's own time), else when it was logged.
	At time.Time
	// Moment is set when At is a moment the event names, not when it was
	// logged: EventLog 6008's, Windows' last stamp of being alive before it
	// went down, logged at the boot after.
	Moment bool
}

// ErrNotWindows is every read off Windows: there are no event logs to read.
var ErrNotWindows = errors.New("there are no Windows event logs off Windows")

// Event is one event as Windows renders it in XML: who logged it, its
// number, when, and the values it carries.
type Event struct {
	// Provider is the name of what logged it, as the XML's Provider Name.
	Provider string
	// ID is the event's number, its qualifiers left out.
	ID int
	// Time is when it was logged, in UTC.
	Time time.Time
	// Data are the event's values in their order, named or not.
	Data []string
	// Binary is the binary data some classic events carry, decoded from
	// its hex.
	Binary []byte
}

// xmlEvent is the part of Windows' event XML Parse reads. The names are
// matched without their namespace, which every element shares.
type xmlEvent struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		// The number may carry Qualifiers as an attribute; the text is
		// the number itself.
		EventID     string `xml:"EventID"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	EventData struct {
		Data []struct {
			Value string `xml:",chardata"`
		} `xml:"Data"`
		Binary string `xml:"Binary"`
	} `xml:"EventData"`
}

// Parse reads one event from the XML Windows renders it as.
func Parse(s string) (Event, error) {
	var x xmlEvent
	if err := xml.Unmarshal([]byte(s), &x); err != nil {
		return Event{}, err
	}
	id, err := strconv.Atoi(strings.TrimSpace(x.System.EventID))
	if err != nil {
		return Event{}, errors.New("an event without a number")
	}
	// Windows writes the time in UTC with up to seven digits after the
	// second, which RFC 3339 with nanoseconds reads.
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(x.System.TimeCreated.SystemTime))
	if err != nil {
		return Event{}, errors.New("an event without its time")
	}
	e := Event{Provider: x.System.Provider.Name, ID: id, Time: at.UTC()}
	for _, d := range x.EventData.Data {
		e.Data = append(e.Data, d.Value)
	}
	// Binary data that is not hex says nothing: the event stands without it.
	if b, err := hex.DecodeString(strings.TrimSpace(x.EventData.Binary)); err == nil {
		e.Binary = b
	}
	return e, nil
}

// The providers whose events Classify knows, as the logs name them.
const (
	kernelPower = "Microsoft-Windows-Kernel-Power"
	kernelGen   = "Microsoft-Windows-Kernel-General"
	eventLog    = "EventLog"
	bugCheck    = "Microsoft-Windows-WER-SystemErrorReporting"
	user32      = "User32"
	msi         = "MsiInstaller"
	update      = "Microsoft-Windows-WindowsUpdateClient"
	winlogon    = "Microsoft-Windows-Winlogon"
)

// Classify says whether an event ends WSL's distributions, and how: a crash
// (Kernel-Power 41 and EventLog 6008 at the boot after it, a bugcheck's
// 1001), an update of WSL (Windows Installer's or Windows Update's events
// that name WSL), a shutdown or restart (User32 1074 as one is asked for,
// EventLog 6006, Kernel-Power 109 and Kernel-General 13 as it goes), or a
// sign-out (Winlogon 7002); or is a boot (Kernel-General 12, EventLog 6005).
func Classify(e Event) (Ending, bool) {
	switch {
	case e.Provider == kernelPower && e.ID == 41, e.Provider == bugCheck && e.ID == 1001:
		// Logged at the boot after: when it happened is not known, only
		// that it was before.
		return Ending{Kind: Crash, At: e.Time}, true
	case e.Provider == eventLog && e.ID == 6008:
		// It names the moment of the unexpected shutdown, in its binary
		// data: when that can be read, that is when.
		if at, ok := shutdownTime(e.Binary); ok {
			return Ending{Kind: Crash, At: at, Moment: true}, true
		}
		return Ending{Kind: Crash, At: e.Time}, true
	case e.Provider == user32 && e.ID == 1074, e.Provider == eventLog && e.ID == 6006,
		e.Provider == kernelPower && e.ID == 109, e.Provider == kernelGen && e.ID == 13:
		return Ending{Kind: Shutdown, At: e.Time}, true
	case (e.Provider == msi || e.Provider == update) && namesWSL(e):
		return Ending{Kind: Update, At: e.Time}, true
	case e.Provider == winlogon && e.ID == 7002:
		return Ending{Kind: Signout, At: e.Time}, true
	case e.Provider == kernelGen && e.ID == 12, e.Provider == eventLog && e.ID == 6005:
		return Ending{Kind: Boot, At: e.Time}, true
	}
	return Ending{}, false
}

// wslProduct is the name WSL's installer gives the product: it is not
// translated, whatever the language of Windows.
const wslProduct = "windows subsystem for linux"

// wslPackage is the file name of WSL's installer as Windows Installer's
// first event of an install names it: wsl.<version>.<architecture>.msi.
var wslPackage = regexp.MustCompile(`(?i)(^|[\\/])wsl\.[0-9][0-9.]*\.(x64|arm64)\.msi$`)

// namesWSL reports whether an installer's or Windows Update's event is
// about WSL: one of its values is WSL's product name or installer file.
func namesWSL(e Event) bool {
	for _, d := range e.Data {
		d = strings.TrimSpace(d)
		if strings.Contains(strings.ToLower(d), wslProduct) || wslPackage.MatchString(d) {
			return true
		}
	}
	return false
}

// shutdownTime reads the moment an unexpected shutdown happened from the
// binary data of EventLog's 6008: two SYSTEMTIMEs of eight 16-bit numbers
// each, the local time then UTC; the second is read.
func shutdownTime(b []byte) (time.Time, bool) {
	if len(b) < 32 {
		return time.Time{}, false
	}
	n := func(i int) int { return int(binary.LittleEndian.Uint16(b[16+2*i:])) }
	// Year, month, day of the week, day, hour, minute, second, millisecond.
	y, mo, d, h, mi, s, ms := n(0), n(1), n(3), n(4), n(5), n(6), n(7)
	// A field out of its range says the data is not what it should be.
	if y < 1601 || mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 59 || ms > 999 {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(mo), d, h, mi, s, ms*int(time.Millisecond), time.UTC), true
}

// Earliest is the ending among es that came first, as a start of a
// distribution ends once: the first thing that ends it is what did.
func Earliest(es []Ending) (Ending, bool) {
	if len(es) == 0 {
		return Ending{}, false
	}
	first := es[0]
	for _, e := range es[1:] {
		if e.At.Before(first.At) {
			first = e
		}
	}
	return first, true
}

// query is one log's read: its channel and the XPath that picks the events
// Classify knows, between two moments, so only those are rendered.
type query struct{ channel, xpath string }

// UpdateWindow is how long after a read begins an update's events are read:
// an update stops WSL as it installs, so only an install about then is of
// use, and every other product's install of the day is left out.
const UpdateWindow = 20 * time.Minute

// queries are the reads of the logs between from and to: Windows' crashes,
// shutdowns and boots over all of it, and the sign-outs of the user whose
// SID is user (another user's ends none of theirs; none for a SID that is
// not one); updates, of every product, only within UpdateWindow of from (or
// to, if sooner), as their events are many on a busy day.
func queries(from, to time.Time, user string) []query {
	upTo := to
	if end := from.Add(UpdateWindow); end.Before(upTo) {
		upTo = end
	}
	when := func(a, b time.Time) string {
		return "TimeCreated[@SystemTime>='" + xpathTime(a) + "' and @SystemTime<='" + xpathTime(b) + "']"
	}
	qs := []query{
		{"System", "*[System[" + when(from, to) + " and (" +
			"(Provider[@Name='" + kernelPower + "'] and (EventID=41 or EventID=109)) or " +
			"(Provider[@Name='" + eventLog + "'] and (EventID=6005 or EventID=6006 or EventID=6008)) or " +
			"(Provider[@Name='" + bugCheck + "'] and EventID=1001) or " +
			"(Provider[@Name='" + user32 + "'] and EventID=1074) or " +
			"(Provider[@Name='" + kernelGen + "'] and (EventID=12 or EventID=13)))]]"},
		{"System", "*[System[" + when(from, upTo) + " and Provider[@Name='" + update + "'] and (EventID=19 or EventID=20 or EventID=43)]]"},
		// Windows Installer's: an install begun (1040), a product installed,
		// removed, changed or updated (1033 to 1036), and its end (11707,
		// 11724, 11728); Classify keeps those that name WSL.
		{"Application", "*[System[" + when(from, upTo) + " and " +
			"Provider[@Name='" + msi + "'] and (EventID=1033 or EventID=1034 or EventID=1035 or EventID=1036 or " +
			"EventID=1040 or EventID=11707 or EventID=11724 or EventID=11728)]]"},
	}
	if sid.MatchString(user) {
		qs = append(qs, query{"System", "*[System[" + when(from, to) + " and Provider[@Name='" + winlogon + "'] and EventID=7002]" +
			" and EventData[Data[@Name='UserSid']='" + user + "']]"})
	}
	return qs
}

// sid is a user's SID as Windows writes it.
var sid = regexp.MustCompile(`^S-1-[0-9]+(-[0-9]+)+$`)

// xpathTime is a moment as the logs' XPath compares it: UTC, to the
// millisecond.
func xpathTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// within runs read, and waits for it d at most: a read still at work then
// is said to be slow, and its answer, when it comes, is left. Every call of
// Windows' a read makes is bounded so, the query's and the rendering's as
// much as the wait for the next events.
func within(d time.Duration, read func() ([]Ending, error)) ([]Ending, error) {
	type answer struct {
		es  []Ending
		err error
	}
	// Room for the answer that comes too late: the read never blocks on it.
	done := make(chan answer, 1)
	go func() {
		es, err := read()
		done <- answer{es, err}
	}()
	select {
	case a := <-done:
		return a.es, a.err
	case <-time.After(d):
		return nil, ErrSlow
	}
}

// ErrSlow is a read that took longer than it waits.
var ErrSlow = errors.New("the event logs took too long to read")

// ErrTooMany is a read that met more events than it reads.
var ErrTooMany = errors.New("more events than wsl-spawner reads at once")
