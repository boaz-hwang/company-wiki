//go:build !windows

package wiki

import "io"

func enableVT(io.Writer) {}
