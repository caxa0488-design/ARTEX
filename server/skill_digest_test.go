package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func syntheticSkill(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	name := "sample-skill"
	if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte("Synthetic non-executable test fixture.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir, name
}
func TestManagedSkillDigest(t *testing.T) {
	dir, name := syntheticSkill(t)
	first, err := digestManagedSkill(t.Context(), dir, name)
	if err != nil {
		t.Fatal(err)
	}
	second, err := digestManagedSkill(t.Context(), dir, name)
	if err != nil || first != second || len(first.Hash) != 64 || first.Files != 1 {
		t.Fatalf("unstable digest: %+v %+v %v", first, second, err)
	}
	if err = os.WriteFile(filepath.Join(dir, name, "notes.txt"), []byte("another harmless fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := digestManagedSkill(t.Context(), dir, name)
	if err != nil || changed.Hash == first.Hash || changed.Files != 2 {
		t.Fatalf("content change not detected: %v", err)
	}
	if err = os.Rename(filepath.Join(dir, name, "notes.txt"), filepath.Join(dir, name, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	renamed, err := digestManagedSkill(t.Context(), dir, name)
	if err != nil || renamed.Hash == changed.Hash {
		t.Fatalf("path change not detected: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = digestManagedSkill(ctx, dir, name); err == nil {
		t.Fatal("cancel ignored")
	}
	for _, bad := range []string{"../sample-skill", ".", "a/b", "a\\b", "a b", ""} {
		if _, err = digestManagedSkill(t.Context(), dir, bad); err == nil {
			t.Fatalf("accepted invalid name %q", bad)
		}
	}
}
func TestManagedSkillDigestRejectsUnsafeTree(t *testing.T) {
	for _, kind := range []string{"missing manifest", "empty manifest", "internal symlink", "root symlink", "too many files", "too large"} {
		t.Run(kind, func(t *testing.T) {
			dir, name := syntheticSkill(t)
			skill := filepath.Join(dir, name)
			switch kind {
			case "missing manifest":
				if err := os.Remove(filepath.Join(skill, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			case "empty manifest":
				if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "internal symlink":
				if err := os.Symlink("SKILL.md", filepath.Join(skill, "linked.txt")); err != nil {
					t.Fatal(err)
				}
			case "root symlink":
				if err := os.Symlink(name, filepath.Join(dir, "alias")); err != nil {
					t.Fatal(err)
				}
				name = "alias"
			case "too many files":
				for i := 0; i < 256; i++ {
					if err := os.WriteFile(filepath.Join(skill, fmt.Sprintf("item-%03d.txt", i)), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "too large":
				f, err := os.Create(filepath.Join(skill, "large.txt"))
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate((16 << 20) + 1)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := digestManagedSkill(t.Context(), dir, name); err == nil {
				t.Fatal("unsafe or unbounded tree accepted")
			}
		})
	}
}
