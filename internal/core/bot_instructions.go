package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// InstructionsFile is the file that holds a bot's standing instructions:
// the CLAUDE.md in its Claude Code folder, which Claude Code loads as user
// instructions. The user may also edit it by hand.
func (s *Service) InstructionsFile(botID string) string {
	return filepath.Join(s.BotConfigDir(botID), "CLAUDE.md")
}

// instructionsOf returns a room's purpose, or a bot's standing instructions
// as its file holds them, "" when it has no file.
func (s *Service) instructionsOf(b Bot) (string, error) {
	if b.IsRoom() {
		return b.Instructions, nil
	}
	data, err := os.ReadFile(s.InstructionsFile(b.ID))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the instructions of bot %s: %w", b.ID, err)
	}
	return string(data), nil
}

// writeInstructions replaces a bot's instructions file with text, whole: a
// turn that starts meanwhile reads the old file or the new one.
func (s *Service) writeInstructions(botID, text string) error {
	path := s.InstructionsFile(botID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create the Claude Code folder of bot %s: %w", botID, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "CLAUDE.md.*")
	if err != nil {
		return fmt.Errorf("create a file for the instructions of bot %s: %w", botID, err)
	}
	_, err = tmp.WriteString(text)
	if err = errors.Join(err, tmp.Close()); err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("save the instructions of bot %s: %w", botID, err), os.Remove(tmp.Name()))
	}
	return nil
}

// MoveInstructionsToFiles copies each bot's instructions from the database,
// where versions before the instructions file kept them, to its instructions
// file, once. A bot that already has the file keeps it. The database keeps
// the text. A run stopped part way leaves each bot moved or not, so the next
// start finishes the move.
func (s *Service) MoveInstructionsToFiles(ctx context.Context) error {
	rows, err := s.q.ListInstructionsToMove(ctx)
	if err != nil {
		return fmt.Errorf("list the bot instructions to move to files: %w", err)
	}
	for _, r := range rows {
		_, err := os.Lstat(s.InstructionsFile(r.ID))
		if errors.Is(err, fs.ErrNotExist) {
			err = s.writeInstructions(r.ID, r.Instructions)
		}
		if err != nil {
			return fmt.Errorf("move the instructions of bot %s to its file: %w", r.ID, err)
		}
		if err := s.q.MarkInstructionsMoved(ctx, r.ID); err != nil {
			return fmt.Errorf("mark the instructions of bot %s moved: %w", r.ID, err)
		}
	}
	return nil
}
