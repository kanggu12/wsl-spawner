//go:build windows

package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestAnAddWaitsForAWritesFileOnItsWayOut(t *testing.T) {
	// Another program's write ended, and its file is on its way out: a
	// handle of its is still open, and Windows keeps the name taken until
	// it closes, refusing to make the file again as access denied. The Add
	// waits for that as for a write under way.
	dir := t.TempDir()
	s := Open(dir)
	p := filepath.Join(dir, "history-new.tmp")
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	// Open so that it may be deleted while open, then mark it deleted the
	// classic way: pending until this handle closes, whatever this Windows
	// does by default.
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	deleteFile := byte(1)
	if err := windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &deleteFile, uint32(unsafe.Sizeof(deleteFile))); err != nil {
		windows.CloseHandle(h)
		t.Fatal(err)
	}
	// The name is taken and the file is gone at once, as the runner saw.
	if _, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600); !os.IsPermission(err) {
		windows.CloseHandle(h)
		t.Skipf("this Windows does not refuse a name on its way out: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Add(Entry{What: "stop"}) }()
	// The other program lets go a moment later.
	time.Sleep(300 * time.Millisecond)
	windows.CloseHandle(h)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting")
	}
	if all, _ := s.All(); len(all) != 1 {
		t.Fatalf("%+v", all)
	}
}

func TestClearWaitsForAReaderToLetGo(t *testing.T) {
	// The screen reads the history as it is cleared: Windows keeps an open
	// file from going, and the clear waits for the reader to let go.
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Add(Entry{What: "stop"}); err != nil {
		t.Fatal(err)
	}
	r, err := os.Open(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Clear() }()
	time.Sleep(300 * time.Millisecond)
	r.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if all, _ := s.All(); len(all) != 0 {
		t.Fatalf("%+v", all)
	}
}
