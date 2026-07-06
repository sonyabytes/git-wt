//go:build !darwin && !linux

package clone

import "errors"

// cowClone is unsupported on this platform; Tree falls back to a plain copy.
func cowClone(src, dst string) error {
	return errors.New("cow clone unsupported on this platform")
}
