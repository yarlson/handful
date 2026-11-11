package move

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Claude Code keeps a folder's sessions in Projects under a key made of the
// folder's path: every character that isn't a letter or a digit becomes "-",
// and a key over 200 characters is cut and gets a hash (Claude Code 2.1.322).
var keyChars = regexp.MustCompile(`[^a-zA-Z0-9]`)

const maxKey = 200

// sessionKey returns the key of Claude Code's sessions in folder dir, and
// false for a key that Claude Code would hash, which the move doesn't copy:
// such a bot starts a fresh session after the move.
func sessionKey(dir string) (string, bool) {
	key := keyChars.ReplaceAllString(dir, "-")
	return key, len(key) <= maxKey
}

// workFolders lists the folders, relative to the data folder, in which bots
// run Claude Code: each bot's own and each team's.
func workFolders(dataDir string) ([]string, error) {
	var out []string
	for _, kind := range []string{"bots", "teams"} {
		found, err := filepath.Glob(filepath.Join(dataDir, kind, "*", "workspace"))
		if err != nil {
			return nil, err
		}
		for _, dir := range found {
			rel, err := filepath.Rel(dataDir, dir)
			if err != nil {
				return nil, err
			}
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

// copySessions copies the sessions of every work folder under the old data
// folder to the key of the same folder under the new one. It never moves or
// changes the originals, so going back needs nothing, and a key that already
// exists is kept.
func (m *Mover) copySessions() error {
	folders, err := workFolders(m.Old)
	if err != nil {
		return err
	}
	for _, rel := range folders {
		oldKey, ok := sessionKey(filepath.Join(m.Old, rel))
		newKey, ok2 := sessionKey(filepath.Join(m.New, rel))
		if !ok || !ok2 {
			continue
		}
		if err := m.copySession(filepath.Join(m.Projects, oldKey), filepath.Join(m.Projects, newKey)); err != nil {
			return err
		}
	}
	return nil
}

// copySession copies the sessions folder from to to, through a temporary
// folder that it renames when the copy is whole.
func (m *Mover) copySession(from, to string) error {
	if !exists(from) || exists(to) {
		return nil
	}
	tmp := filepath.Join(filepath.Dir(to), ".handful-move-"+filepath.Base(to))
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := copyTree(from, tmp); err != nil {
		return fmt.Errorf("copy the Claude Code sessions in %s: %w", from, errors.Join(err, os.RemoveAll(tmp)))
	}
	if err := m.step(); err != nil {
		return err
	}
	if err := os.Rename(tmp, to); err != nil {
		return err
	}
	return m.step()
}

// copyTree copies the folder from to the new folder to with the same modes:
// sessions are private, so their copies stay so. A link is copied as a link.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Mkdir(dst, info.Mode().Perm())
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, dst)
		}
		return copyFile(path, dst, info.Mode().Perm())
	})
}
