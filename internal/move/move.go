// Package move moves an installed user's data from YarGPT's folder,
// ~/.yargpt, to Handful's, ~/.handful (Handful is YarGPT's new name), and
// back again (spec ad09e72c §4.3, §4.4). Every step looks at the disk first
// and does only what is missing, so a run that a crash cut short finishes on
// the next one.
package move

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"handful/internal/db"
)

const (
	// MovedFile, in the data folder, records the move. Written before the
	// folder moves, it tells a moved folder from a fresh install.
	MovedFile = "moved-from-yargpt.json"
	dbFile    = "yargpt.db"
	// BackupFile, in the data folder, is the database as YarGPT left it.
	BackupFile    = "backup/yargpt-before-handful.db"
	afterMoveFile = "handful-after-move.db"
)

// Paths are the folders a move touches.
type Paths struct {
	Old      string // ~/.yargpt
	New      string // ~/.handful
	Projects string // Claude Code's sessions, ~/.claude/projects
}

// HomePaths returns the folders of a move in the home folder home.
func HomePaths(home string) Paths {
	return Paths{
		Old:      filepath.Join(home, ".yargpt"),
		New:      filepath.Join(home, ".handful"),
		Projects: filepath.Join(home, ".claude", "projects"),
	}
}

// Moved is what MovedFile records.
type Moved struct {
	MovedAt    int64  `json:"movedAt"` // ms
	Version    string `json:"handfulVersion"`
	NoticeDone bool   `json:"noticeDone,omitempty"` // the user closed the welcome notice
}

// ErrBothFolders stops a start that finds both folders: either may hold
// the data the user wants.
var ErrBothFolders = errors.New("both ~/.yargpt and ~/.handful exist; keep one as ~/.handful and rename the other, then start again")

// BackupError is a backup that couldn't be written; nothing was migrated.
type BackupError struct{ Err error }

func (e *BackupError) Error() string {
	return "Couldn't back up your data before moving it: " + e.Err.Error() + ". Free some space and start again."
}

func (e *BackupError) Unwrap() error { return e.Err }

// Mover moves the data of one user.
type Mover struct {
	Paths
	Addr    string      // the server's address, which a running server holds
	Running func() bool // whether a server answers on Addr
	Now     func() time.Time
	Version string // Handful's version

	stopAfter, done int // a test stops the move after stopAfter steps
}

var errStopped = errors.New("stopped for a test")

// step counts a finished step; a test's mover stops after its stopAfter.
func (m *Mover) step() error {
	m.done++
	if m.done == m.stopAfter {
		return errStopped
	}
	return nil
}

// Data moves the data folder when it is still YarGPT's, links the old
// folder to the new one, and backs up a database that no Handful migration
// has touched. It reports whether it created the link. Run it before the
// server opens the database.
func (m *Mover) Data(ctx context.Context) (bool, error) {
	oldDir, oldLink, err := folderState(m.Old)
	if err != nil {
		return false, err
	}
	newDir, newLink, err := folderState(m.New)
	if err != nil {
		return false, err
	}
	if oldDir && (newDir || newLink) {
		return false, ErrBothFolders
	}
	if oldDir {
		if err := m.moveFolder(); err != nil {
			return false, err
		}
		oldLink, newDir = false, true
	}
	linked := false
	if newDir && !oldLink && exists(filepath.Join(m.New, MovedFile)) {
		if err := os.Symlink(m.New, m.Old); err != nil {
			return false, fmt.Errorf("link %s to %s: %w", m.Old, m.New, err)
		}
		linked = true
		if err := m.step(); err != nil {
			return false, err
		}
	}
	if _, err := db.BackupBeforeHandful(ctx, filepath.Join(m.New, dbFile), filepath.Join(m.New, BackupFile)); err != nil {
		return false, &BackupError{Err: err}
	}
	return linked, nil
}

// moveFolder copies the Claude Code sessions to the keys of their new
// folders, puts aside what an earlier move back left, records the move and
// renames the folder. Nothing before the rename changes the old folder's
// data.
func (m *Mover) moveFolder() error {
	if m.Running() {
		return fmt.Errorf("a YarGPT server is running on %s; stop it, then start handful again", m.Addr)
	}
	if err := m.copySessions(); err != nil {
		return err
	}
	if err := m.putAside(); err != nil {
		return err
	}
	data, err := json.Marshal(Moved{MovedAt: m.Now().UnixMilli(), Version: m.Version})
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(m.Old, MovedFile), data); err != nil {
		return err
	}
	if err := m.step(); err != nil {
		return err
	}
	if err := os.Rename(m.Old, m.New); err != nil {
		return fmt.Errorf("move %s to %s: %w", m.Old, m.New, err)
	}
	return m.step()
}

// putAside renames the backup and Handful's database that an earlier move
// back left in the old folder, so that this move backs up the data as it is
// now and a later move back restores that.
func (m *Mover) putAside() error {
	stamp := "-" + strconv.FormatInt(m.Now().UnixMilli(), 10)
	for _, name := range []string{BackupFile, afterMoveFile, afterMoveFile + "-wal", afterMoveFile + "-shm"} {
		path := filepath.Join(m.Old, name)
		if !exists(path) {
			continue
		}
		ext := filepath.Ext(path)
		base := path[:len(path)-len(ext)]
		if err := os.Rename(path, base+stamp+ext); err != nil {
			return err
		}
		if err := m.step(); err != nil {
			return err
		}
	}
	return nil
}

// folderState reports whether path is a real folder or a link.
func folderState(path string) (dir, link bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return info.IsDir(), info.Mode()&fs.ModeSymlink != 0, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// writeFile writes data to path whole: through a temporary file that it
// then renames.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadMoved returns what MovedFile in dataDir records, and false when the
// data wasn't moved.
func ReadMoved(dataDir string) (Moved, bool, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, MovedFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Moved{}, false, nil
	}
	if err != nil {
		return Moved{}, false, err
	}
	var mv Moved
	if err := json.Unmarshal(data, &mv); err != nil {
		return Moved{}, false, fmt.Errorf("read %s: %w", MovedFile, err)
	}
	return mv, true, nil
}

// CloseNotice records that the user closed the welcome notice of a move.
func CloseNotice(dataDir string) error {
	mv, ok, err := ReadMoved(dataDir)
	if err != nil || !ok {
		return err
	}
	mv.NoticeDone = true
	data, err := json.Marshal(mv)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(dataDir, MovedFile), data)
}
