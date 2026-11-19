//go:build !unix

package server

import (
	"errors"
	"os"
)

func openDigestFile(root *os.Root, path string) (*os.File, error) {
	return nil, errors.New("safe skill digest is currently supported on Unix servers only")
}
