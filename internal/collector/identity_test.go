package collector

import "testing"

func TestGroupIdentityHasStrategySpecificSemantics(t *testing.T) {
	root := ProcessIdentity{PID: 10, StartTime: 100}
	instance := GroupID{Mode: GroupApplication, Root: root}
	tree := GroupID{Mode: GroupTree, Root: root}
	reused := GroupID{Mode: GroupApplication, Root: ProcessIdentity{PID: 10, StartTime: 101}}
	image := GroupID{Mode: GroupExecutable, Executable: ExecutableIdentity{Path: "/bin/tool", Device: 1, Inode: 2}}
	replaced := image
	replaced.Executable.Inode++
	ids := map[GroupID]bool{instance: true, tree: true, reused: true, image: true, replaced: true}
	copyOfInstance := GroupID{Mode: GroupApplication, Root: root}
	if len(ids) != 5 || instance.Compare(copyOfInstance) != 0 || image.Compare(replaced) == 0 {
		t.Fatal("mode, process lifetime or executable replacement lost identity")
	}
}
