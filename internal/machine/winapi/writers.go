package winapi

// The access rights, as an access mask holds them, that let a holder change
// something: write into a file or add one to a folder, delete, set who may
// do what, take ownership. A name for each, as Windows names them.
const (
	rightWriteData   = 0x00000002 // FILE_WRITE_DATA, FILE_ADD_FILE on a folder
	rightAppendData  = 0x00000004 // FILE_APPEND_DATA, FILE_ADD_SUBDIRECTORY on a folder
	rightDeleteChild = 0x00000040 // FILE_DELETE_CHILD
	rightDelete      = 0x00010000 // DELETE
	rightWriteDAC    = 0x00040000 // WRITE_DAC
	rightWriteOwner  = 0x00080000 // WRITE_OWNER
	rightGenericWrt  = 0x40000000 // GENERIC_WRITE, as an unmapped entry may hold it
	rightGenericAll  = 0x10000000 // GENERIC_ALL
)

// role is what an object is to a program Windows starts: the program's
// own file, the folder it is in, or a folder above that.
type role int

const (
	roleProgram role = iota
	roleFolder
	roleAbove
	// roleRoot is the root of a drive above the program: no one deletes
	// or renames a drive's root, so DELETE on it changes nothing, though
	// Windows gives Authenticated Users Modify on the root of a drive it
	// formats; deleting a folder in it, setting who may do what and taking
	// ownership still change what stands below.
	roleRoot
)

// canChange reports whether an access mask, of an entry that applies to an
// object in a role, lets its holder change what Windows starts at the
// program: to write into the program, to put a file next to it (a library or
// a manifest it would take up) or take it away, or, above its folder, to
// take away or rename the folder it stands in. Creating a folder in one
// above is what Windows lets every user do at the root of a drive, and
// changes nothing that stands.
func canChange(mask uint32, r role) bool {
	always := uint32(rightDelete | rightWriteDAC | rightWriteOwner | rightGenericAll)
	switch r {
	case roleProgram:
		return mask&(always|rightWriteData|rightAppendData|rightGenericWrt) != 0
	case roleFolder:
		return mask&(always|rightWriteData|rightAppendData|rightDeleteChild|rightGenericWrt) != 0
	case roleRoot:
		return mask&(always&^rightDelete|rightDeleteChild) != 0
	}
	return mask&(always|rightDeleteChild) != 0
}
