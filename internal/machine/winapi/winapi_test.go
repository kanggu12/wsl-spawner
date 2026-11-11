package winapi

import "testing"

func TestFixedDiskIsADriveOfThisPCThatStays(t *testing.T) {
	// Made-up drives, by GetDriveType's types: C: and D: fixed, E: one that
	// can be taken out, F: optical, R: a RAM disk, Z: mapped to a share.
	types := map[string]uint32{`C:\`: 3, `D:\`: 3, `E:\`: 2, `F:\`: 5, `R:\`: 6, `Z:\`: 4}
	driveType := func(root string) uint32 { return types[root] }
	for path, want := range map[string]bool{
		`C:\Users\Ann\AppData\Local\Programs\wsl-spawner\wsl-spawnerw.exe`: true,
		`d:\tools\wsl-spawnerw.exe`:                                        true,
		`\\?\C:\Users\Ann\wsl-spawner\wsl-spawnerw.exe`:                    true,
		`\\.\C:\tools\wsl-spawnerw.exe`:                                    true,
		// A distribution's own files, through WSL's share: at sign-in that
		// starts the distribution, at sign-out the hook runs from what it stops.
		`\\wsl.localhost\Ubuntu\home\ann\bin\wsl-spawnerw.exe`: false,
		`\\wsl$\Ubuntu\home\ann\bin\wsl-spawnerw.exe`:          false,
		`\\server\share\wsl-spawnerw.exe`:                      false,
		`//server/share/wsl-spawnerw.exe`:                      false,
		`\\?\UNC\server\share\wsl-spawnerw.exe`:                false,
		`Z:\tools\wsl-spawnerw.exe`:                            false,
		`E:\wsl-spawnerw.exe`:                                  false,
		`F:\wsl-spawnerw.exe`:                                  false,
		`R:\wsl-spawnerw.exe`:                                  false,
		// No drive to tell it by.
		`wsl-spawnerw.exe`:  false,
		`1:\x.exe`:          false,
		``:                  false,
		`Q:\not\there.exe`:  false,
		`C:relative\to.exe`: true,
	} {
		if got := fixedDisk(path, driveType); got != want {
			t.Errorf("%q: %v", path, got)
		}
	}
}
