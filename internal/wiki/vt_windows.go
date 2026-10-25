//go:build windows

package wiki

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on ANSI escape handling in the Windows console.
func enableVT(out io.Writer) {
	f, ok := out.(*os.File)
	if !ok {
		return
	}
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) == nil {
		_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
}
