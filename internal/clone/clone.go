// Package clone copies directory trees using filesystem copy-on-write where
// available (APFS clonefile on darwin), falling back to a plain copy.
package clone

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// doCoW is a seam over the platform cowClone so tests can force either the
// CoW path or the plain-copy fallback regardless of the host filesystem.
var doCoW = cowClone

// Tree clones src to dst. dst must not exist. On APFS the whole tree is
// cloned in one clonefile call; otherwise the tree is walked and copied.
func Tree(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("clone destination already exists: %s", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := doCoW(src, dst); err == nil {
		return nil
	}
	return copyTree(src, dst)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Unreachable: the walk only yields paths under src.
		rel, err := filepath.Rel(src, path)
		if err != nil { // coverage-ignore
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			// Unreachable short of a race: Lstat just reported a symlink.
			link, err := os.Readlink(path)
			if err != nil { // coverage-ignore
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(path, target, info.Mode().Perm())
		default:
			return nil // sockets, devices: skip
		}
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
