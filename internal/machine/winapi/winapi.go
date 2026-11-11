// Package winapi holds the few Windows calls this tool needs about processes,
// folders, disks and the console: which known folders hold its files,
// whether a process still runs, what the console can show, which disk a
// program is on, and this program started again, on its own or as an
// administrator. Off Windows the calls report that they cannot answer.
package winapi

import (
	"errors"
	"strings"
)

// ErrNotWindows is returned off Windows.
var ErrNotWindows = errors.New("not on Windows")

// ErrDeclined is a request to run as an administrator that the person
// answered no to.
var ErrDeclined = errors.New("the administrator's permission was not given")

// ErrNotAReport is a file a copy started as an administrator will not write
// its answer into: only an empty file of its own, as the one that asked made
// it, takes it.
var ErrNotAReport = errors.New("not an empty file of its own")

// driveFixed is the type Windows gives a fixed disk's drive
// (GetDriveType's DRIVE_FIXED).
const driveFixed = 3

// fixedDisk reports whether the Windows path is on a fixed disk of this PC,
// by its form and by driveType, the type Windows gives a drive's root, as
// GetDriveType answers: not a share (\\server\share, a distribution's own
// files under \\wsl.localhost, or a drive mapped to one), nor a drive that
// can be taken out, an optical one or a RAM disk. A path whose drive cannot
// be told is not on one.
func fixedDisk(path string, driveType func(root string) uint32) bool {
	p := path
	// \\?\C:\x and \\.\C:\x are C:\x written long; \\?\UNC\server\share
	// is a share.
	if strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) {
		p = p[4:]
		if len(p) >= 4 && strings.EqualFold(p[:4], `UNC\`) {
			return false
		}
	}
	// A share, written either way.
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `//`) {
		return false
	}
	// A drive letter and its colon, or nothing to tell the drive by.
	if len(p) < 2 || p[1] != ':' || !('a' <= p[0]|0x20 && p[0]|0x20 <= 'z') {
		return false
	}
	return driveType(strings.ToUpper(p[:1])+`:\`) == driveFixed
}
