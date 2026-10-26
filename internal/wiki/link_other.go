//go:build !windows

package wiki

import "os"

// linkDir makes link point to the existing folder target.
func linkDir(target, link string) error {
	return os.Symlink(target, link)
}
