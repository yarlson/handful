package agent

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxGlobalInstructions bounds <data>/AGENTS.md. The system prompt is one
// argv string, and Linux caps one argument at 128 KiB.
const maxGlobalInstructions = 32 << 10

func (r *Runner) globalInstructionsPath() string {
	return filepath.Join(r.svc.DataDir(), "AGENTS.md")
}

// globalInstructions reads <data>/AGENTS.md, the user's instructions for
// every bot. A missing or blank file gives "".
func (r *Runner) globalInstructions() (string, error) {
	path := r.globalInstructionsPath()
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxGlobalInstructions+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxGlobalInstructions {
		return "", fmt.Errorf("%s is larger than %d KiB; shorten it", path, maxGlobalInstructions>>10)
	}
	return strings.TrimSpace(string(data)), nil
}
