package clone

import (
	"os"

	"golang.org/x/sys/unix"
)

// ioctlFileClone is a seam over unix.IoctlFileClone so tests can exercise
// the reflink success path on filesystems without reflink support (CI
// runners use ext4).
var ioctlFileClone = unix.IoctlFileClone

// cowClone clones a tree with per-file FICLONE reflinks (btrfs, XFS).
// Unlike darwin's clonefile there is no recursive variant: directories and
// symlinks are recreated, regular files share extents copy-on-write. Any
// failure (e.g. ext4) makes Tree fall back to a plain copy.
func cowClone(src, dst string) error {
	return walkTree(src, dst, reflinkFile)
}

func reflinkFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if err := ioctlFileClone(int(out.Fd()), int(in.Fd())); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
