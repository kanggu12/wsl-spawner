//go:build windows

package winapi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultUILng = kernel32.NewProc("GetUserDefaultUILanguage")
	procGetConsoleWindow    = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleOutputCP  = kernel32.NewProc("GetConsoleOutputCP")
)

// OpenReport opens the file at path for a copy started as an administrator
// to write its answer into, when it is an empty file of its own: opened as
// it is, a link not followed and nothing made, then refused if it is a
// link, a folder, a file another name also leads to (a hard link), or holds
// anything. Whoever named the path, that copy writes over nothing else.
func OpenReport(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// No sharing while it is checked and written: nothing swaps it meanwhile.
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var fi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &fi); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	if fi.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || fi.NumberOfLinks != 1 || fi.FileSizeHigh != 0 || fi.FileSizeLow != 0 {
		windows.CloseHandle(h)
		return nil, ErrNotAReport
	}
	return os.NewFile(uintptr(h), path), nil
}

// FixedDisk reports whether path is on a fixed disk of this PC, which is
// there whatever else runs as Windows starts a program by itself: not a
// share, a distribution's own files or a mapped drive, nor a drive that can
// be taken out, an optical one or a RAM disk.
func FixedDisk(path string) bool {
	return fixedDisk(path, func(root string) uint32 {
		p, err := windows.UTF16PtrFromString(root)
		if err != nil {
			return 0
		}
		return windows.GetDriveType(p)
	})
}

// Korean reports whether Windows shows its interface in Korean.
func Korean() bool {
	id, _, _ := procGetUserDefaultUILng.Call()
	// The primary language is the low ten bits; 0x12 is Korean.
	return id&0x3ff == 0x12
}

// LocalAppData is the per-user folder for this tool's history.
func LocalAppData() string { return knownFolder("LOCALAPPDATA", windows.FOLDERID_LocalAppData) }

// AppData is the per-user folder for its settings.
func AppData() string { return knownFolder("APPDATA", windows.FOLDERID_RoamingAppData) }

// knownFolder is the folder the environment variable names or, when it is
// empty, the one Windows keeps for it: an empty variable would put the
// program's files under whatever folder it was started in. A test gets ""
// instead, so it never reaches the person's own folders.
func knownFolder(env string, id *windows.KNOWNFOLDERID) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	if testing.Testing() {
		return ""
	}
	p, err := windows.KnownFolderPath(id, 0)
	if err != nil {
		return ""
	}
	return p
}

// Running reports whether process pid runs and is the program named exe. A
// process id is given to another program once its own ends, so the name is
// compared too. A process this user may not look at counts as not running:
// it is not one of this user's runs.
func Running(pid int, exe string) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	// An ended process keeps its id while a handle is open: ask whether it
	// still runs.
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil || code != stillActive {
		return false
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(windows.UTF16ToString(buf[:n])), exe)
}

// ProcessStart is when process pid started, and false when Windows does not
// say (no such process, or no right to ask).
func ProcessStart(pid int) (time.Time, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, created.Nanoseconds()), true
}

// stillActive is the exit code of a process that has not ended (STILL_ACTIVE).
const stillActive = 259

// Colors turns on escape-code colors on the console behind fd, and reports
// whether the console takes them; false for a pipe or a file. The mode it
// changes is put back by RestoreConsole.
func Colors(fd uintptr) bool {
	var mode uint32
	h := windows.Handle(fd)
	if windows.GetConsoleMode(h, &mode) != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	if windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) != nil {
		return false
	}
	// The first mode seen is the one the console had.
	consoleMu.Lock()
	defer consoleMu.Unlock()
	if _, ok := consoleModes[h]; !ok {
		consoleModes[h] = mode
	}
	return true
}

// consoleModes are the consoles' modes before Colors changed them.
var (
	consoleMu    sync.Mutex
	consoleModes = map[windows.Handle]uint32{}
)

// ClassicConsole reports whether this process draws, on its standard
// output, in a console window of its own: the Windows console host, with the
// console's own fonts, rather than a terminal that runs the console as a
// pseudoconsole (Windows Terminal, VS Code's, any other), which draws with
// fonts of its own and never shows that console's window. The window decides,
// not WT_SESSION: a console window opened from Windows Terminal inherits that
// variable.
func ClassicConsole() bool {
	// Output to a file or a pipe is drawn by nobody.
	var mode uint32
	if windows.GetConsoleMode(windows.Handle(os.Stdout.Fd()), &mode) != nil {
		return false
	}
	// No console window at all: nothing to draw in.
	h, _, _ := procGetConsoleWindow.Call()
	if h == 0 {
		return false
	}
	return windows.IsWindowVisible(windows.HWND(h))
}

// ConsoleEastAsian reports whether the console writes in an East Asian code
// page (Japanese 932, Simplified Chinese 936, Korean 949, Traditional
// Chinese 950), where the console host draws characters Unicode leaves
// ambiguous two cells wide.
func ConsoleEastAsian() bool {
	cp, _, _ := procGetConsoleOutputCP.Call()
	switch cp {
	case 932, 936, 949, 950:
		return true
	}
	return false
}

// RestoreConsole puts back the modes Colors changed, as the program ends:
// the console is left as the shell had it.
func RestoreConsole() {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	for h, mode := range consoleModes {
		_ = windows.SetConsoleMode(h, mode)
		delete(consoleModes, h)
	}
}

// errNoProgramInTests is what starting a program gives in a test: a test
// never starts this program again on the person's PC.
var errNoProgramInTests = errors.New("a test starts no program")

// StartDetached starts this program again with args, on its own: no
// console, no window, nothing tying it to the terminal it was asked from,
// so it goes on when that terminal closes. Where the job it runs in lets a
// process leave it, it leaves: a terminal's job may end everything in it.
func StartDetached(args []string) error {
	if testing.Testing() {
		return errNoProgramInTests
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	start := func(flags uint32) error {
		cmd := exec.Command(self, args...)
		// From Windows' own folder, as a program started with none.
		cmd.Dir = os.Getenv("SystemRoot")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	// A job that does not let its processes leave refuses the start: then
	// it starts in the job, as any program started from here would.
	if err := start(flags | windows.CREATE_BREAKAWAY_FROM_JOB); err == nil {
		return nil
	}
	return start(flags)
}

var (
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// shellExecuteInfo is SHELLEXECUTEINFOW, as ShellExecuteExW takes it.
type shellExecuteInfo struct {
	size          uint32
	mask          uint32
	hwnd          windows.Handle
	verb          *uint16
	file          *uint16
	parameters    *uint16
	directory     *uint16
	show          int32
	instApp       windows.Handle
	idList        uintptr
	class         *uint16
	keyClass      windows.Handle
	hotKey        uint32
	iconOrMonitor windows.Handle
	process       windows.Handle
}

// RunElevated starts the program at exe with args as an administrator,
// which Windows asks the person for (UAC), waits for it and returns its exit
// code. The program is started with no window of its own. An answer of no
// is ErrDeclined.
func RunElevated(exe string, args []string) (int, error) {
	if testing.Testing() {
		return 0, errNoProgramInTests
	}
	line, err := quotedArgs(args)
	if err != nil {
		return 0, err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	params, _ := windows.UTF16PtrFromString(line)
	dir, _ := windows.UTF16PtrFromString(os.Getenv("SystemRoot"))
	info := shellExecuteInfo{verb: verb, file: file, parameters: params, directory: dir,
		mask: 0x00000040 | 0x00000400, // SEE_MASK_NOCLOSEPROCESS | SEE_MASK_FLAG_NO_UI
		show: 0}                       // SW_HIDE
	info.size = uint32(unsafe.Sizeof(info))
	if r, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(e, windows.ERROR_CANCELLED) {
			return 0, ErrDeclined
		}
		return 0, fmt.Errorf("starting %s as an administrator: %w", exe, e)
	}
	defer windows.CloseHandle(info.process)
	if _, err := windows.WaitForSingleObject(info.process, windows.INFINITE); err != nil {
		return 0, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.process, &code); err != nil {
		return 0, err
	}
	return int(code), nil
}

// quotedArgs is args as one command line, each quoted as Windows programs
// read them back.
func quotedArgs(args []string) (string, error) {
	quoted := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, "\"\r\n") {
			return "", fmt.Errorf("cannot pass %q to a program started as an administrator", a)
		}
		quoted[i] = windows.EscapeArg(a)
	}
	return strings.Join(quoted, " "), nil
}

// ExcludedPorts is what netsh lists of the ports Windows keeps from
// programs for proto ("tcp" or "udp"): Hyper-V, WinNAT and others reserve
// ranges, and a program cannot bind a port in one. Any user may ask.
func ExcludedPorts(proto string) (string, error) {
	if proto != "tcp" && proto != "udp" {
		return "", fmt.Errorf("no protocol %q", proto)
	}
	cmd := exec.Command("netsh.exe", "interface", "ipv4", "show", "excludedportrange", "protocol="+proto)
	cmd.Dir = os.Getenv("SystemRoot")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("netsh: %w", err)
	}
	return string(out), nil
}
