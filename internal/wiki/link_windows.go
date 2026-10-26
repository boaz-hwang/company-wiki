//go:build windows

package wiki

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// linkDir makes link point to the existing folder target: a directory junction, which needs no
// administrator rights or developer mode, or a symbolic link if mklink is unavailable.
func linkDir(target, link string) error {
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err == nil {
		return nil
	}
	if serr := os.Symlink(target, link); serr != nil {
		return fmt.Errorf("mklink /J: %v %s; symlink: %v", err, strings.TrimSpace(string(out)), serr)
	}
	return nil
}
