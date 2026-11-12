package winapi

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StandardUser reports whether this process runs as a standard user: its
// token is neither elevated nor an administrator's filtered one, so to
// change the policy Windows would ask for an administrator's own name and
// password, and a program this user can change would run as that
// administrator. What it cannot tell, it takes for one.
func StandardUser() bool {
	t := windows.GetCurrentProcessToken()
	if t.IsElevated() {
		return false
	}
	// 1 is the default: a standard user's, or an account UAC is off for; 2
	// is full, an elevated administrator's; 3 is limited, an
	// administrator's filtered token, which may elevate with consent.
	var kind, n uint32
	if err := windows.GetTokenInformation(t, windows.TokenElevationType, (*byte)(unsafe.Pointer(&kind)), 4, &n); err != nil {
		return true
	}
	return kind == 1
}

// trustedSIDs are who may change what Windows starts for every user of the
// PC without that being a way in for another user: the person who runs
// this, when trustUser, the system, the administrators, Windows' own
// installer and the placeholder for an object's creator.
func trustedSIDs(trustUser bool) ([]*windows.SID, error) {
	var out []*windows.SID
	if trustUser {
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return nil, fmt.Errorf("reading who runs this: %w", err)
		}
		out = append(out, user.User.Sid)
	}
	for _, s := range []string{
		"S-1-5-18",     // the system
		"S-1-5-32-544", // the administrators
		"S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464", // TrustedInstaller
		"S-1-3-0", // the creator of an object
	} {
		sid, err := windows.StringToSid(s)
		if err != nil {
			return nil, err
		}
		out = append(out, sid)
	}
	return out, nil
}

// isTrusted reports whether sid is one of them.
func isTrusted(sid *windows.SID, trusted []*windows.SID) bool {
	for _, t := range trusted {
		if windows.EqualSid(sid, t) {
			return true
		}
	}
	return false
}

// accountOf names a SID as Windows shows it, DOMAIN\name, or as it is.
func accountOf(sid *windows.SID) string {
	if name, domain, _, err := sid.LookupAccount(""); err == nil && name != "" {
		if domain != "" {
			return domain + `\` + name
		}
		return name
	}
	return sid.String()
}

// whoOn names who, beyond the trusted, can change the object at p in a role:
// whose it is, or whose entry in its access list lets them. "" when none.
// An entry that only applies to what is made below the object, inherited
// by it, is not one for it.
func whoOn(p string, r role, trusted []*windows.SID) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", fmt.Errorf("reading who may change %s: %w", p, err)
	}
	// An owner may set the access list as they like.
	if owner, _, err := sd.Owner(); err == nil && owner != nil && !isTrusted(owner, trusted) {
		return accountOf(owner), nil
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return "", fmt.Errorf("reading who may change %s: %w", p, err)
	}
	// No access list at all lets everyone do everything: a disk that keeps
	// none, FAT's, is one.
	if dacl == nil {
		return "Everyone", nil
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return "", fmt.Errorf("reading who may change %s: %w", p, err)
		}
		// Only what allows, and what is for this object itself.
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !isTrusted(sid, trusted) && canChange(uint32(ace.Mask), r) {
			return accountOf(sid), nil
		}
	}
	return "", nil
}

// WhoElseCanChange names who, beyond the person who runs this (when
// trustUser), the system and the administrators, can change what Windows
// starts at path, a program: the program, the folder it is in, or a folder
// above that, by the owner and the access list of each, as the program is
// reached by the path and, where the path goes through a link, by where it
// leads. A program that is not there is as good as whoever can put one
// there: its nearest folder that is there is taken for its own. It returns
// who and at which of them; "" when none does.
func WhoElseCanChange(path string, trustUser bool) (who, at string, err error) {
	trusted, err := trustedSIDs(trustUser)
	if err != nil {
		return "", "", err
	}
	paths := []string{filepath.Clean(path)}
	// Where the path leads, through links and junctions both: Go's
	// EvalSymlinks goes through no junction since Go 1.23, Windows' own
	// final path of an open handle goes through every kind.
	if real, err := finalPath(path); err == nil && !strings.EqualFold(real, paths[0]) {
		paths = append(paths, real)
	}
	for _, p := range paths {
		r := roleProgram
		// A program that is gone: the folder it was in is the one to look
		// at, as anyone who may add a file there may add it again.
		for {
			if _, err := os.Lstat(p); err == nil || !errors.Is(err, fs.ErrNotExist) || filepath.Dir(p) == p {
				break
			}
			p, r = filepath.Dir(p), roleFolder
		}
		for obj := p; ; obj = filepath.Dir(obj) {
			// A drive's root, above the program, is no one's to delete.
			if r == roleAbove && filepath.Dir(obj) == obj {
				r = roleRoot
			}
			if w, err := whoOn(obj, r, trusted); err != nil {
				return "", "", err
			} else if w != "" {
				return w, obj, nil
			}
			// The folder the program is in, then the folders above it.
			if r == roleFolder {
				r = roleAbove
			} else if r == roleProgram {
				r = roleFolder
			}
			if parent := filepath.Dir(obj); parent == obj {
				break
			}
		}
	}
	return "", "", nil
}

// finalPath is where path leads as Windows resolves it, through symbolic
// links and junctions, as a plain path (C:\..., or \\server\share\...). For
// a path that is not there, it is where its nearest folder that is there
// leads, with the rest of the path after it: a program that is gone is
// looked at where it would be put again.
func finalPath(path string) (string, error) {
	path = filepath.Clean(path)
	rest := ""
	for {
		if _, err := os.Lstat(path); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) || filepath.Dir(path) == path {
			return "", err
		}
		rest = filepath.Join(filepath.Base(path), rest)
		path = filepath.Dir(path)
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	// No access asked for: only the name is read. Backup semantics opens
	// a folder as well as a file.
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	// FILE_NAME_NORMALIZED and VOLUME_NAME_DOS, both 0: the name as the
	// file system has it, after a drive letter.
	const normalizedDOS = 0x0
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), normalizedDOS)
	if err != nil {
		return "", err
	}
	if int(n) > len(buf) {
		return "", fmt.Errorf("the final path of %s is too long", path)
	}
	real := windows.UTF16ToString(buf[:n])
	// \\?\C:\... is C:\...; \\?\UNC\server\share\... is \\server\share\...
	switch {
	case strings.HasPrefix(real, `\\?\UNC\`):
		real = `\\` + real[len(`\\?\UNC\`):]
	case strings.HasPrefix(real, `\\?\`):
		real = real[len(`\\?\`):]
	}
	if rest != "" {
		real = filepath.Join(real, rest)
	}
	return filepath.Clean(real), nil
}

// CanDelete asks Windows whether this process may delete the file at path,
// and deletes nothing: it opens the file for deletion, as the link it may
// be, and closes it again. Windows lets the open through for DELETE on the
// file or the right to delete what is in its folder; an error that is
// fs.ErrPermission when it does not.
func CanDelete(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	return windows.CloseHandle(h)
}
