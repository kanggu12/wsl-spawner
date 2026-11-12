package winapi

import "testing"

func TestWhatLetsAHolderChangeWhatWindowsStarts(t *testing.T) {
	for _, tc := range []struct {
		name string
		mask uint32
		r    role
		want bool
	}{
		// Read and run: never.
		{"read and run the program", 0x001200A9, roleProgram, false},
		{"read and run the folder", 0x001200A9, roleFolder, false},
		{"read and run above", 0x001200A9, roleAbove, false},
		// Modify (what Authenticated Users get on a folder made under a
		// drive's root): write, delete.
		{"modify the program", 0x001301BF, roleProgram, true},
		{"modify the folder", 0x001301BF, roleFolder, true},
		{"modify above", 0x001301BF, roleAbove, true},
		// Write a file into a folder (what Users get in ProgramData): a
		// library or a manifest put next to the program, but nothing
		// that stands above the folder is taken away.
		{"add a file to the folder", 0x2, roleFolder, true},
		{"add a file to a folder above", 0x2, roleAbove, false},
		// Create a folder (what Users get at a drive's root): nothing
		// that stands is changed above the folder.
		{"create a folder above", 0x4, roleAbove, false},
		{"create a folder in the folder", 0x4, roleFolder, true},
		// Delete what is in a folder, set who may do what, take ownership.
		{"delete child above", 0x40, roleAbove, true},
		{"write dac of the program", 0x40000, roleProgram, true},
		{"write owner above", 0x80000, roleAbove, true},
		{"generic all", 0x10000000, roleAbove, true},
		{"generic write the program", 0x40000000, roleProgram, true},
		// A drive's root, which no one deletes or renames: Modify on it
		// (what Windows gives Authenticated Users on a drive it formats)
		// changes nothing below; deleting what is in it, setting who may
		// do what or taking it over does.
		{"modify a drive's root", 0x001301BF, roleRoot, false},
		{"delete a drive's root", 0x10000, roleRoot, false},
		{"delete child at a drive's root", 0x40, roleRoot, true},
		{"write dac of a drive's root", 0x40000, roleRoot, true},
		{"write owner of a drive's root", 0x80000, roleRoot, true},
		{"generic all at a drive's root", 0x10000000, roleRoot, true},
		// Only attributes and extended attributes: nothing that runs.
		{"write attributes", 0x100 | 0x10, roleProgram, false},
	} {
		if got := canChange(tc.mask, tc.r); got != tc.want {
			t.Errorf("%s (%#x): %v, want %v", tc.name, tc.mask, got, tc.want)
		}
	}
}
