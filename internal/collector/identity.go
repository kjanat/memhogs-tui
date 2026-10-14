package collector

import "cmp"

// ExecutableIdentity distinguishes executable files, including replacements at
// the same path. Platforms without file identity use their observed path/name.
type ExecutableIdentity struct {
	Path   string
	Device uint64
	Inode  uint64
}

// GroupID is a comparable, strategy-specific identity. Names and package hints
// never identify an application instance; its verified root lifetime does.
type GroupID struct {
	Mode       Grouping
	Root       ProcessIdentity
	Executable ExecutableIdentity
}

// Compare orders identities deterministically without serializing display text.
func (id GroupID) Compare(other GroupID) int {
	return cmp.Or(cmp.Compare(id.Mode, other.Mode),
		cmp.Compare(id.Root.PID, other.Root.PID), cmp.Compare(id.Root.StartTime, other.Root.StartTime),
		cmp.Compare(id.Executable.Path, other.Executable.Path),
		cmp.Compare(id.Executable.Device, other.Executable.Device), cmp.Compare(id.Executable.Inode, other.Executable.Inode))
}

func executableNameID(name string) GroupID {
	return GroupID{Mode: GroupExecutable, Executable: ExecutableIdentity{Path: name}}
}
