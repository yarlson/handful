package move

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"handful/internal/db"
)

// Back takes the user back to YarGPT (spec ad09e72c §4.4): it restores the
// database as it was at the move and returns the folder to ~/.yargpt.
// Without yes, it only tells w what would be lost. Handful's database stays,
// as ~/.yargpt/handful-after-move.db. Each step checks whether it is done,
// so Back can run again after a crash.
func (m *Mover) Back(ctx context.Context, yes bool, w io.Writer) error {
	if m.Running() {
		return fmt.Errorf("a server is running on %s; quit Handful, then run -move-back again", m.Addr)
	}
	oldDir, _, err := folderState(m.Old)
	if err != nil {
		return err
	}
	newDir, _, err := folderState(m.New)
	if err != nil {
		return err
	}
	if oldDir && !newDir {
		_, err := fmt.Fprintln(w, "Your data is already back in ~/.yargpt.")
		return err
	}
	mv, ok, err := ReadMoved(m.New)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("~/.handful has no record of a move from YarGPT, so there is nothing to go back to")
	}
	if !exists(filepath.Join(m.New, BackupFile)) {
		return errors.New("~/.handful/backup has no copy of YarGPT's data, so there is nothing to go back to")
	}
	if err := m.tellLoss(ctx, mv.MovedAt, yes, w); err != nil || !yes {
		return err
	}
	if err := m.goBack(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "Done: your data is back in ~/.yargpt. Now open YarGPT v0.13.0.")
	return err
}

// tellLoss tells w what going back loses: what was made in Handful after
// movedAt (ms).
func (m *Mover) tellLoss(ctx context.Context, movedAt int64, yes bool, w io.Writer) error {
	path := filepath.Join(m.New, afterMoveFile)
	if !exists(path) {
		path = filepath.Join(m.New, dbFile)
	}
	conn, err := db.Open(ctx, path)
	if err != nil {
		return err
	}
	defer conn.Close()
	q := db.New(conn)
	messages, err := q.CountChatMessagesSince(ctx, movedAt)
	if err != nil {
		return err
	}
	memories, err := q.CountMemoriesSince(ctx, movedAt)
	if err != nil {
		return err
	}
	routines, err := q.CountRoutinesChangedSince(ctx, movedAt)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Going back to YarGPT restores your data as it was on %s.\nLost on the way back: %s, %s, %s made in Handful since then.\n",
		time.UnixMilli(movedAt).Format("2006-01-02 15:04"),
		count(messages, "chat message", "chat messages"), count(memories, "memory", "memories"), count(routines, "routine change", "routine changes"))
	if !yes {
		text += "They stay in ~/.yargpt/handful-after-move.db. Run again with -yes to go back.\n"
	}
	_, err = io.WriteString(w, text)
	return err
}

func count(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.FormatInt(n, 10) + " " + many
}

// goBack keeps Handful's database as handful-after-move.db, restores the
// backup as yargpt.db, removes the link and renames the folder back.
func (m *Mover) goBack() error {
	for _, suffix := range []string{"-wal", "-shm", ""} {
		from, to := filepath.Join(m.New, dbFile+suffix), filepath.Join(m.New, afterMoveFile+suffix)
		if !exists(from) || exists(to) {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		if err := m.step(); err != nil {
			return err
		}
	}
	if err := m.restoreBackup(); err != nil {
		return err
	}
	_, link, err := folderState(m.Old)
	if err != nil {
		return err
	}
	if link {
		if err := os.Remove(m.Old); err != nil {
			return err
		}
		if err := m.step(); err != nil {
			return err
		}
	}
	if err := os.Rename(m.New, m.Old); err != nil {
		return fmt.Errorf("move %s back to %s: %w", m.New, m.Old, err)
	}
	return m.step()
}

// restoreBackup copies the backup to yargpt.db, through a temporary file,
// unless yargpt.db is there already.
func (m *Mover) restoreBackup() error {
	path := filepath.Join(m.New, dbFile)
	if exists(path) {
		return nil
	}
	if err := copyFile(filepath.Join(m.New, BackupFile), path); err != nil {
		return err
	}
	return m.step()
}

// copyFile copies from to to whole: through a temporary file that it then
// renames.
func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := to + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		return errors.Join(err, dst.Close(), os.Remove(tmp))
	}
	if err := dst.Close(); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return os.Rename(tmp, to)
}
