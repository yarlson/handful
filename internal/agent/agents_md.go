package agent

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"unicode"
)

// sharedInstructionFiles returns the paths of <data>/CLAUDE.md and <data>/AGENTS.md that hold
// text: the user's instructions for every bot, which Claude Code loads by itself because every
// working folder lies in the data folder.
func sharedInstructionFiles(dataDir string) ([]string, error) {
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("find the absolute path of the data folder: %w", err)
	}
	var files []string
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		path := filepath.Join(dataDir, name)
		ok, err := holdsText(path)
		if err != nil {
			return nil, fmt.Errorf("read the shared instructions %s: %w", path, err)
		}
		if ok {
			files = append(files, path)
		}
	}
	return files, nil
}

// holdsText tells whether path is a regular file with a character other than white space.
func holdsText(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return false, err
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	text := bufio.NewReader(f)
	for {
		c, _, err := text.ReadRune()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !unicode.IsSpace(c) {
			return true, nil
		}
	}
}
