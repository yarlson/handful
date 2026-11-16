package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BotConfigDir is the bot's Claude Code folder, which holds its own skills,
// instructions and settings. The folder may not exist yet.
func (s *Service) BotConfigDir(botID string) string {
	return filepath.Join(s.dataDir, "bots", botID, "claude")
}

// maxLinks is the most symlinks realPath follows in one path, as on macOS (MAXSYMLINKS).
const maxLinks = 32

// changesBotConfig reports whether a Write or Edit call changes a file that sets
// bots up: a bot's Claude Code folder (BotConfigDir), the shared skills, or the
// shared AGENTS.md or CLAUDE.md in the data folder. A call whose file cannot be
// read from its input or resolved counts as such a change.
func (s *Service) changesBotConfig(ctx context.Context, req ApprovalRequest) (bool, error) {
	if req.Tool != "Write" && req.Tool != "Edit" {
		return false, nil
	}
	path, ok := filePath(req.Input)
	paths, known := s.expandHome(path)
	if !ok || !known {
		return true, nil
	}
	targets, err := s.writeTargets(ctx, req, paths)
	if err != nil {
		return false, err
	}
	return reachesBotConfig(s.dataDir, targets), nil
}

// filePath returns the file_path of a Write or Edit input; ok is false when it has none.
func filePath(input json.RawMessage) (path string, ok bool) {
	var in struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", false
	}
	return in.FilePath, in.FilePath != ""
}

// reachesBotConfig reports whether any of targets, resolved, is a file that sets
// bots up in the data folder. A path that does not resolve counts as one.
func reachesBotConfig(dataDir string, targets []string) bool {
	data, err := realPath(dataDir)
	if err != nil {
		return true
	}
	for _, target := range targets {
		real, err := realPath(target)
		if err != nil || isBotConfig(data, real) {
			return true
		}
	}
	return false
}

// expandHome returns path, and also path with a leading "~" replaced by the
// home folder, as Claude Code expands it. ok is false when path starts with "~"
// and the home folder is not known.
func (s *Service) expandHome(path string) (paths []string, ok bool) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return []string{path}, true
	}
	return []string{path, s.home + path[1:]}, s.home != ""
}

// writeTargets returns the files that paths can name. A relative path
// is in the turn's working folder. Each path is given as written, where ".."
// goes up from the folder a symlink leads to, and cleaned, where ".." removes
// the name before it, because a tool may clean a path before the OS opens it.
func (s *Service) writeTargets(ctx context.Context, req ApprovalRequest, paths []string) ([]string, error) {
	var targets []string
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			dir, err := s.TurnWorkspace(ctx, req.BotID, req.TranscriptID)
			if err != nil {
				return nil, err
			}
			path = dir + string(filepath.Separator) + path
		}
		targets = append(targets, path, filepath.Clean(path))
	}
	return targets, nil
}

// realPath resolves an absolute path as the OS does when it opens the path: it
// follows each symlink, and ".." goes up from the folder reached so far. A name
// that does not exist stays as written, since a write creates it.
func realPath(path string) (string, error) {
	sep := string(filepath.Separator)
	if !filepath.IsAbs(path) {
		// The data folder can be relative to the server's working folder.
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", path, err)
		}
		path = cwd + sep + path
	}
	resolved, pending := sep, strings.Split(path, sep)
	for links := 0; len(pending) > 0; {
		name := pending[0]
		pending = pending[1:]
		if name == "" || name == "." {
			continue
		}
		if name == ".." {
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, name)
		info, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) || err == nil && info.Mode()&fs.ModeSymlink == 0 {
			resolved = next
			continue
		}
		if err != nil {
			return "", err
		}
		if links++; links > maxLinks {
			return "", fmt.Errorf("resolve %s: more than %d symlinks", path, maxLinks)
		}
		link, err := os.Readlink(next)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(link) {
			resolved = sep
		}
		pending = append(strings.Split(link, sep), pending...)
	}
	return resolved, nil
}

// isBotConfig reports whether path, resolved, is a file that sets bots up in
// data, the resolved data folder. Names compare without case, as on the
// default macOS file system.
func isBotConfig(data, path string) bool {
	sep := string(filepath.Separator)
	rest, ok := strings.CutPrefix(strings.ToLower(path), strings.ToLower(data)+sep)
	if !ok {
		return false
	}
	parts := strings.Split(rest, sep)
	if parts[0] == "skills" || rest == "agents.md" || rest == "claude.md" {
		return true
	}
	return len(parts) >= 3 && parts[0] == "bots" && parts[2] == "claude"
}
