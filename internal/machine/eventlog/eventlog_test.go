package eventlog

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The events below are shaped as Windows renders them, copied once from a
// real PC's System and Application logs; every value that was that PC's
// (its name, its user, times, versions, paths, numbers) is made up.
const ns = `xmlns='http://schemas.microsoft.com/win/2004/08/events/event'`

// event is an event's XML with the provider, number, time and EventData given.
func event(provider string, id int, qualifiers bool, at, data string) string {
	q := ""
	if qualifiers {
		q = ` Qualifiers='32768'`
	}
	return "<Event " + ns + "><System><Provider Name='" + provider + "'/><EventID" + q + ">" + fmt.Sprint(id) +
		"</EventID><Version>0</Version><Level>4</Level><TimeCreated SystemTime='" + at + "'/><EventRecordID>12345</EventRecordID>" +
		"<Channel>System</Channel><Computer>PC</Computer><Security/></System><EventData>" + data + "</EventData></Event>"
}

var (
	// Kernel-Power 41, at the boot after a crash.
	crash41 = event("Microsoft-Windows-Kernel-Power", 41, false, "2025-03-04T05:06:57.1234567Z",
		"<Data Name='BugcheckCode'>0</Data><Data Name='BugcheckParameter1'>0x0</Data><Data Name='SleepInProgress'>0</Data>"+
			"<Data Name='LongPowerButtonPressDetected'>false</Data>")
	// EventLog 6008: its binary data is the moment of the unexpected
	// shutdown, local time then UTC: 2025-03-04 14:05:06.789 and 05:05:06.789.
	crash6008 = event("EventLog", 6008, true, "2025-03-04T05:07:10.7654321Z",
		"<Data>\u200e2025-\u200e03-\u200e04</Data><Data>\n</Data><Data>\n</Data><Data>4321</Data><Data>\n</Data><Data>\n</Data>"+
			"<Binary>E9070300020004000E00050006001503E90703000200040005000500060015033C0000003C000000000000000000000000000000000000000000000000000000</Binary>")
	// A bugcheck's 1001.
	bug1001 = event("Microsoft-Windows-WER-SystemErrorReporting", 1001, false, "2025-03-04T05:07:20.0000000Z",
		`<Data Name='param1'>0x000000d1 (0x1, 0x2, 0x8, 0x1)</Data><Data Name='param2'>C:\WINDOWS\Minidump\030425-1-01.dmp</Data>`)
	// User32 1074: a shutdown asked for from the Start menu.
	ask1074 = event("User32", 1074, true, "2025-03-05T10:00:00.5000000Z",
		`<Data Name='param1'>C:\WINDOWS\SystemApps\Microsoft.Windows.StartMenuExperienceHost_cw5n1h2txyewy\StartMenuExperienceHost.exe (PC)</Data>`+
			`<Data Name='param2'>PC</Data><Data Name='param4'>0x0</Data><Data Name='param5'>power off</Data><Data Name='param6'>
</Data><Data Name='param7'>PC\someone</Data>`)
	// Kernel-General 13, as the system goes down.
	down13 = event("Microsoft-Windows-Kernel-General", 13, false, "2025-03-05T10:00:12.0000000Z",
		"<Data Name='StopTime'>2025-03-05T10:00:12.0000000Z</Data>")
	// Windows Update's start of WSL's install, and Windows Installer's
	// first event of it, which names its file.
	wu43 = event("Microsoft-Windows-WindowsUpdateClient", 43, false, "2025-03-06T01:02:03.0000000Z",
		"<Data Name='updateTitle'>Windows Subsystem for Linux 9.8.7.0 (x64)</Data><Data Name='updateGuid'>{00000000-0000-0000-0000-000000000001}</Data>"+
			"<Data Name='updateRevisionNumber'>1</Data>")
	msi1040 = event("MsiInstaller", 1040, false, "2025-03-06T01:02:04.0000000Z",
		`<Data>C:\WINDOWS\SoftwareDistribution\Download\0123456789abcdef\img\wsl.9.8.7.0.x64.msi</Data><Data>4321</Data>`+
			"<Data>(NULL)</Data><Data>(NULL)</Data><Data>(NULL)</Data><Data>(NULL)</Data>")
	msi1033 = event("MsiInstaller", 1033, false, "2025-03-06T01:03:00.0000000Z",
		"<Data>Windows Subsystem for Linux</Data><Data>9.8.7.0</Data><Data>1033</Data><Data>0</Data><Data>Microsoft Corporation</Data>"+
			"<Data>(NULL)</Data><Binary>7B30307D</Binary>")
	// Another product's install: not WSL's.
	other1033 = event("MsiInstaller", 1033, false, "2025-03-06T01:04:00.0000000Z",
		"<Data>Some Other Product</Data><Data>1.2.3</Data><Data>1033</Data><Data>0</Data><Data>Someone Else</Data><Data>(NULL)</Data>")
)

func TestParseReadsWhatClassifyNeeds(t *testing.T) {
	e, err := Parse(crash6008)
	if err != nil {
		t.Fatal(err)
	}
	// The qualifiers are left out of the number; the time is UTC.
	if e.Provider != "EventLog" || e.ID != 6008 || !e.Time.Equal(time.Date(2025, 3, 4, 5, 7, 10, 765432100, time.UTC)) {
		t.Fatalf("got %+v", e)
	}
	if len(e.Data) != 6 || e.Data[3] != "4321" || len(e.Binary) != 64 {
		t.Fatalf("data %q, %d bytes of binary", e.Data, len(e.Binary))
	}
	// Garbage, an event without its number and one without its time are
	// errors.
	for _, bad := range []string{"<Event", strings.Replace(crash41, ">41<", "><", 1), strings.Replace(crash41, "2025-03-04T05:06:57.1234567Z", "", 1)} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("no error for %.60q", bad)
		}
	}
}

func TestClassifyKnowsEachEnding(t *testing.T) {
	for _, c := range []struct {
		name, xml string
		kind      Kind
		at        time.Time
	}{
		{"Kernel-Power 41", crash41, Crash, time.Date(2025, 3, 4, 5, 6, 57, 123456700, time.UTC)},
		// The moment 6008 names, in UTC: before it was logged.
		{"EventLog 6008", crash6008, Crash, time.Date(2025, 3, 4, 5, 5, 6, 789000000, time.UTC)},
		{"bugcheck 1001", bug1001, Crash, time.Date(2025, 3, 4, 5, 7, 20, 0, time.UTC)},
		{"User32 1074", ask1074, Shutdown, time.Date(2025, 3, 5, 10, 0, 0, 500000000, time.UTC)},
		{"Kernel-General 13", down13, Shutdown, time.Date(2025, 3, 5, 10, 0, 12, 0, time.UTC)},
		{"Windows Update 43", wu43, Update, time.Date(2025, 3, 6, 1, 2, 3, 0, time.UTC)},
		{"Windows Installer 1040", msi1040, Update, time.Date(2025, 3, 6, 1, 2, 4, 0, time.UTC)},
		{"Windows Installer 1033", msi1033, Update, time.Date(2025, 3, 6, 1, 3, 0, 0, time.UTC)},
	} {
		e, err := Parse(c.xml)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got, ok := Classify(e)
		if !ok || got.Kind != c.kind || !got.At.Equal(c.at) {
			t.Errorf("%s: got %v %v, want %s at %v", c.name, got, ok, c.kind, c.at)
		}
	}
}

func TestClassifyLeavesOthersOut(t *testing.T) {
	// Another product's install, and an event of a number the provider
	// logs for something else.
	for _, x := range []string{other1033, strings.Replace(crash41, ">41<", ">42<", 1)} {
		e, err := Parse(x)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := Classify(e); ok {
			t.Errorf("classified %s %d as %v", e.Provider, e.ID, got)
		}
	}
}

func TestA6008WithoutItsMomentIsWhenItWasLogged(t *testing.T) {
	// Binary data too short, or with a month out of range: the time it was
	// logged stands in.
	for _, bin := range []string{"0000000000000000", strings.Repeat("00", 16) + "E9070D00" + strings.Repeat("00", 12)} {
		x := strings.Replace(crash6008, crash6008[strings.Index(crash6008, "<Binary>"):strings.Index(crash6008, "</Binary>")], "<Binary>"+bin, 1)
		e, err := Parse(x)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := Classify(e)
		if !ok || got.Kind != Crash || !got.At.Equal(e.Time) {
			t.Errorf("binary %s: got %v %v", bin, got, ok)
		}
	}
}

func TestNamesWSLByProductOrInstallerFile(t *testing.T) {
	for _, c := range []struct {
		data []string
		want bool
	}{
		{[]string{"Product: Windows Subsystem for Linux -- Installation completed successfully."}, true},
		{[]string{`C:\Users\someone\Downloads\wsl.2.6.1.0.arm64.msi`}, true},
		{[]string{`wsl.10.0.0.0.x64.msi`}, true},
		// Not WSL's installer, though it says wsl.
		{[]string{`C:\tools\wsl-helper.msi`, "wsl.exe"}, false},
		{[]string{"Some Other Product"}, false},
	} {
		if got := namesWSL(Event{Data: c.data}); got != c.want {
			t.Errorf("%q: got %v", c.data, got)
		}
	}
}

func TestEarliestIsTheFirstToEnd(t *testing.T) {
	t0 := time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC)
	es := []Ending{{Kind: Shutdown, At: t0.Add(3 * time.Hour)}, {Kind: Update, At: t0.Add(time.Hour)}, {Kind: Crash, At: t0.Add(2 * time.Hour)}}
	if got, ok := Earliest(es); !ok || got.Kind != Update {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := Earliest(nil); ok {
		t.Fatal("an ending out of none")
	}
}

func TestEachQueryIsWhole(t *testing.T) {
	// The moments go in each XPath, and the brackets close.
	for _, q := range queries(time.Unix(0, 0), time.Unix(3600, 0), "S-1-5-21-1-2-3-1001") {
		if strings.Contains(q.xpath, "%!") || strings.Count(q.xpath, "[") != strings.Count(q.xpath, "]") || strings.Count(q.xpath, "(") != strings.Count(q.xpath, ")") {
			t.Errorf("%s: %s", q.channel, q.xpath)
		}
	}
	if got := xpathTime(time.Date(2025, 3, 4, 14, 5, 6, 789000000, time.FixedZone("KST", 9*3600))); got != "2025-03-04T05:05:06.789Z" {
		t.Errorf("got %s", got)
	}
}

func TestUpdatesAreReadOnlyNearTheStartOfTheRead(t *testing.T) {
	// Two days read: the crashes, shutdowns and boots over all of it; the
	// updates, of every product, only the first UpdateWindow.
	from := time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC)
	to := from.Add(48 * time.Hour)
	whole, near := "@SystemTime<='"+xpathTime(to)+"'", "@SystemTime<='"+xpathTime(from.Add(UpdateWindow))+"'"
	for _, q := range queries(from, to, "") {
		updates := strings.Contains(q.xpath, update) || strings.Contains(q.xpath, msi)
		if updates && !strings.Contains(q.xpath, near) || !updates && !strings.Contains(q.xpath, whole) {
			t.Errorf("%s: %s", q.channel, q.xpath)
		}
	}
	// A read shorter than that reads updates to its end.
	for _, q := range queries(from, from.Add(time.Minute), "") {
		if !strings.Contains(q.xpath, "@SystemTime<='"+xpathTime(from.Add(time.Minute))+"'") {
			t.Errorf("%s: %s", q.channel, q.xpath)
		}
	}
}

func TestASignOutReadIsTheUsersOwn(t *testing.T) {
	count := func(user string) (n int, x string) {
		for _, q := range queries(time.Unix(0, 0), time.Unix(60, 0), user) {
			if strings.Contains(q.xpath, winlogon) {
				n, x = n+1, q.xpath
			}
		}
		return n, x
	}
	if n, x := count("S-1-5-21-1-2-3-1001"); n != 1 || !strings.Contains(x, "Data[@Name='UserSid']='S-1-5-21-1-2-3-1001'") {
		t.Fatalf("%d: %s", n, x)
	}
	// What is not a SID is not put in a query.
	for _, user := range []string{"", "S-1-5-21-1' or '1'='1", "Administrator"} {
		if n, _ := count(user); n != 0 {
			t.Errorf("%q: read", user)
		}
	}
}

func TestAReadThatTakesTooLongIsSlow(t *testing.T) {
	// Whatever call of Windows' holds it: the read gives up in its time.
	block := make(chan struct{})
	defer close(block)
	began := time.Now()
	_, err := within(20*time.Millisecond, func() ([]Ending, error) { <-block; return nil, nil })
	if err != ErrSlow || time.Since(began) > time.Second {
		t.Fatalf("got %v after %v", err, time.Since(began))
	}
	// One in time gives its answer.
	es, err := within(time.Second, func() ([]Ending, error) { return []Ending{{Kind: Boot}}, nil })
	if err != nil || len(es) != 1 {
		t.Fatalf("got %v %v", es, err)
	}
}

func FuzzParse(f *testing.F) {
	// The events above, and XML no log writes, as seeds.
	for _, x := range []string{crash41, crash6008, msi1040, wu43, "<Event><System><EventID>6008</EventID><TimeCreated SystemTime='2025-01-01T00:00:00Z'/><Provider Name='EventLog'/></System><EventData><Binary>zz</Binary></EventData></Event>"} {
		f.Add(x)
	}
	f.Fuzz(func(t *testing.T, x string) {
		e, err := Parse(x)
		if err != nil {
			return
		}
		// Whatever it read, classifying it never fails, and an ending has
		// its kind.
		if en, ok := Classify(e); ok && en.Kind != Crash && en.Kind != Update && en.Kind != Shutdown {
			t.Fatalf("kind %q", en.Kind)
		}
	})
}

// Winlogon's 7002, at each sign-out, and the boot's own events.
var (
	signout7002 = event("Microsoft-Windows-Winlogon", 7002, false, "2025-03-05T09:59:40.0000000Z",
		"<Data Name='TSId'>1</Data><Data Name='UserSid'>S-1-5-21-1-2-3-1001</Data>")
	boot12   = event("Microsoft-Windows-Kernel-General", 12, false, "2025-03-05T11:00:00.0000000Z", "<Data Name='StartTime'>2025-03-05T10:59:59.5000000Z</Data>")
	boot6005 = event("EventLog", 6005, true, "2025-03-05T11:00:20.0000000Z", "<Binary>00</Binary>")
)

func TestASignOutAndABootAreToldApart(t *testing.T) {
	for _, c := range []struct {
		name, xml string
		kind      string
	}{
		{"Winlogon 7002", signout7002, "signout"},
		{"Kernel-General 12", boot12, "boot"},
		{"EventLog 6005", boot6005, "boot"},
	} {
		e, err := Parse(c.xml)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got, ok := Classify(e)
		if !ok || string(got.Kind) != c.kind || !got.At.Equal(e.Time) {
			t.Errorf("%s: got %v %v, want %s", c.name, got, ok, c.kind)
		}
	}
}
