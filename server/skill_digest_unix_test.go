//go:build unix

package server

import (
	"golang.org/x/sys/unix"
	"path/filepath"
	"testing"
)

func TestManagedSkillDigestRejectsSpecialFile(t *testing.T) {
	dir, name := syntheticSkill(t)
	if err := unix.Mkfifo(filepath.Join(dir, name, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := digestManagedSkill(t.Context(), dir, name); err == nil {
		t.Fatal("special file accepted")
	}
}
