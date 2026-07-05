//go:build !darwin

package clone

import "errors"

// cowClone is unsupported off darwin for now; Tree falls back to a plain
// copy. Linux reflink (btrfs/XFS) can slot in here later.
func cowClone(src, dst string) error {
	return errors.New("cow clone unsupported on this platform")
}
