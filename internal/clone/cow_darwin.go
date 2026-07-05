package clone

import "golang.org/x/sys/unix"

// cowClone clones an entire tree with one clonefile(2) call. APFS clones
// directories recursively; every file is copy-on-write.
func cowClone(src, dst string) error {
	return unix.Clonefile(src, dst, 0)
}
