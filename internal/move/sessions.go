package move

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/google/uuid"
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

// CopyTranscript copies Claude Code session sessionID, run in work folder
// dir, from the Claude Code folder from to the Claude Code folder to: the
// transcript and the folder of its subagents and tool output. It never
// changes the originals and keeps a copy that exists. The transcript, copied
// last, marks a whole copy, so a copy that stopped partway finishes on the
// next call.
func CopyTranscript(from, to, dir, sessionID string) error {
	if !isSessionID(sessionID) {
		return nil
	}
	// Claude Code names the folder by the real path of its working folder.
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	key, ok := sessionKey(dir)
	if !ok {
		return nil
	}
	src, dst := filepath.Join(from, "projects", key), filepath.Join(to, "projects", key)
	transcript := sessionID + ".jsonl"
	info, err := os.Stat(filepath.Join(src, transcript))
	if errors.Is(err, fs.ErrNotExist) || exists(filepath.Join(dst, transcript)) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	if exists(filepath.Join(src, sessionID)) && !exists(filepath.Join(dst, sessionID)) {
		tmp := filepath.Join(dst, ".handful-copy-"+sessionID)
		if err := os.RemoveAll(tmp); err != nil {
			return err
		}
		if err := copyTree(filepath.Join(src, sessionID), tmp); err != nil {
			return fmt.Errorf("copy the folder of Claude Code session %s: %w", sessionID, errors.Join(err, os.RemoveAll(tmp)))
		}
		if err := os.Rename(tmp, filepath.Join(dst, sessionID)); err != nil {
			return err
		}
	}
	return copyFile(filepath.Join(src, transcript), filepath.Join(dst, transcript), info.Mode().Perm())
}

// isSessionID reports whether id is a session ID in the form Claude Code
// writes: a UUID in lowercase with hyphens.
func isSessionID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
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
