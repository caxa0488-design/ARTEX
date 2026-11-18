//go:build unix

package server

import (
	"golang.org/x/sys/unix"
	"os"
)

func openDigestFile(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}
