package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

type skillDigest struct {
	Hash  string `json:"hash"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// Hash only a selected registered directory; never return or interpret contents.
// Root confines path resolution on supported server platforms; symlinks and
// special files are rejected. Detected changes during reads fail instead of being accepted. This is a
// management observation, not an atomic filesystem snapshot or runtime lock.
func digestManagedSkill(ctx context.Context, dir, name string) (skillDigest, error) {
	var out skillDigest
	if !validSkillName(name) {
		return out, errors.New("invalid skill directory name")
	}
	parent, err := os.OpenRoot(dir)
	if err != nil {
		return out, errors.New("skill root unavailable")
	}
	defer parent.Close()
	initial, err := parent.Lstat(name)
	if err != nil || !initial.IsDir() || initial.Mode()&os.ModeSymlink != 0 {
		return out, errors.New("skill directory missing or unsafe")
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return out, errors.New("cannot open skill directory")
	}
	defer root.Close()
	sum := sha256.New()
	entries := 0
	manifest := false
	observed := map[string]fs.FileInfo{}
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("skill tree unavailable")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		if entries > 512 {
			return errors.New("skill tree exceeds 512 entries")
		}
		observedInfo, statError := root.Lstat(path)
		if statError != nil {
			return errors.New("skill tree changed during traversal")
		}
		if observedInfo.Mode()&os.ModeSymlink != 0 || observedInfo.IsDir() != entry.IsDir() {
			return errors.New("skill symlink or changed directory entry")
		}
		if path == "." && !os.SameFile(initial, observedInfo) {
			return errors.New("skill directory changed during open")
		}
		observed[path] = observedInfo
		if path == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("skill symlinks are not accepted")
		}
		if entry.IsDir() {
			return nil
		}
		before, e := root.Lstat(path)
		if e != nil || !before.Mode().IsRegular() {
			return errors.New("skill special file or unstable path")
		}
		out.Files++
		out.Bytes += before.Size()
		if out.Files > 256 || out.Bytes > 16<<20 {
			return errors.New("skill digest exceeds 256 files or 16 MiB")
		}
		file, e := openDigestFile(root, path)
		if e != nil {
			return errors.New("skill file unavailable")
		}
		opened, e := file.Stat()
		if e != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
			file.Close()
			return errors.New("skill file changed during open")
		}
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(file, 16<<20+1))
		after, statErr := file.Stat()
		file.Close()
		final, pathErr := root.Lstat(path)
		if e != nil || statErr != nil || pathErr != nil || n != before.Size() || !os.SameFile(before, final) || final.Mode()&os.ModeSymlink != 0 || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !before.ModTime().Equal(final.ModTime()) {
			return errors.New("skill content changed during hashing")
		}
		fmt.Fprintf(sum, "%d:%s:%d:%x\n", len(path), path, n, h.Sum(nil))
		if path == "SKILL.md" && n > 0 {
			manifest = true
		}
		return nil
	})
	if err != nil {
		return skillDigest{}, err
	}
	for path, before := range observed {
		if err := ctx.Err(); err != nil {
			return skillDigest{}, err
		}
		now, e := root.Lstat(path)
		if e != nil || !os.SameFile(before, now) || before.Mode() != now.Mode() || before.Size() != now.Size() || !before.ModTime().Equal(now.ModTime()) {
			return skillDigest{}, errors.New("skill tree changed during hashing")
		}
	}
	final, err := parent.Lstat(name)
	if err != nil || !os.SameFile(initial, final) || !final.IsDir() || final.Mode()&os.ModeSymlink != 0 {
		return skillDigest{}, errors.New("skill directory changed during hashing")
	}
	if !manifest || out.Bytes == 0 {
		return skillDigest{}, errors.New("nonempty SKILL.md is required")
	}
	out.Hash = hex.EncodeToString(sum.Sum(nil))
	return out, nil
}
