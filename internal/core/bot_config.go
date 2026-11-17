package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
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

// filePath returns the file_path of a Write or Edit input, trimmed as Claude
// Code's file tools trim it before use; ok is false when it has none.
func filePath(input json.RawMessage) (path string, ok bool) {
	var in struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", false
	}
	path = strings.TrimFunc(in.FilePath, isTrimmedSpace)
	return path, path != ""
}

// isTrimmedSpace reports whether JavaScript's String.prototype.trim removes r:
// the Zs space separators, the ASCII white space, U+2028, U+2029 and U+FEFF.
// unicode.IsSpace differs: it adds U+0085 and lacks U+FEFF.
func isTrimmedSpace(r rune) bool {
	return unicode.Is(unicode.Zs, r) || strings.ContainsRune("\t\n\v\f\r\u2028\u2029\ufeff", r)
}

// reachesBotConfig reports whether any of targets, resolved, is a file that sets
// bots up in the data folder. A path that does not resolve counts as one.
func reachesBotConfig(dataDir string, targets []string) bool {
	tree, err := configTree(dataDir)
	if err != nil {
		return true
	}
	for _, target := range targets {
		real, err := realPath(target)
		if err != nil || isBotConfig(tree, real) {
			return true
		}
	}
	return false
}

// treeEntry is a folder or file of the data folder, as found on disk, and its
// path in the data folder.
type treeEntry struct {
	rel  []string
	info fs.FileInfo
}

// configTree returns the data folder and the entries in it that a path to a
// file that sets bots up can go through. A path reaches an entry by identity,
// whatever its spelling: other letter case, a firmlink, a volfs path or a hard link.
func configTree(dataDir string) ([]treeEntry, error) {
	data, err := os.Stat(dataDir)
	if err != nil {
		return nil, err
	}
	tree := []treeEntry{{nil, data}}
	rels := [][]string{{"skills"}, {"bots"}, {"AGENTS.md"}, {"CLAUDE.md"}}
	bots, err := os.ReadDir(filepath.Join(dataDir, "bots"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, bot := range bots {
		rels = append(rels, []string{"bots", bot.Name()}, []string{"bots", bot.Name(), "claude"})
	}
	for _, rel := range rels {
		if info, err := os.Stat(filepath.Join(append([]string{dataDir}, rel...)...)); err == nil {
			tree = append(tree, treeEntry{rel, info})
		}
	}
	return tree, nil
}

// isBotConfig reports whether path, resolved, is a file that sets bots up. It
// finds the nearest of path and its folders that is an entry of tree, and
// reads the rest of path from that entry.
func isBotConfig(tree []treeEntry, path string) bool {
	for dir := path; ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil {
			for _, entry := range tree {
				if os.SameFile(info, entry.info) {
					rest := strings.FieldsFunc(path[len(dir):], func(r rune) bool { return r == filepath.Separator })
					return isConfigName(append(slices.Clone(entry.rel), rest...))
				}
			}
		}
		if dir == filepath.Dir(dir) {
			return false
		}
	}
}

// isConfigName reports whether rel, a path in the data folder, names a file
// that sets bots up. A name that is not an entry of the tree either does not
// exist yet or is no such file, so names compare with Unicode case folding: a
// file made under a folded name answers to the entry's name on a
// case-insensitive file system.
func isConfigName(rel []string) bool {
	if len(rel) == 0 {
		return false
	}
	if strings.EqualFold(rel[0], "skills") {
		return true
	}
	if len(rel) == 1 {
		return strings.EqualFold(rel[0], "AGENTS.md") || strings.EqualFold(rel[0], "CLAUDE.md")
	}
	return len(rel) >= 3 && strings.EqualFold(rel[0], "bots") && strings.EqualFold(rel[2], "claude")
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
// follows each symlink, and ".." goes up from the folder reached so far. A ".."
// that the OS reads as a folder other than that parent is an error. A name that
// does not exist stays as written, since a write creates it.
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
		switch name {
		case "", ".":
			continue
		case "..":
			if !parentIsLexical(resolved) {
				return "", fmt.Errorf("resolve %s: .. in %s is not its parent folder", path, resolved)
			}
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, name)
		// Names in /proc, such as self, name the process that opens the path:
		// the bot's, not the server's.
		if next == "/proc" {
			return "", fmt.Errorf("resolve %s: the path goes through /proc", path)
		}
		link, err := readLink(next)
		if err != nil {
			return "", err
		}
		if link == "" {
			resolved = next
			continue
		}
		if links++; links > maxLinks {
			return "", fmt.Errorf("resolve %s: more than %d symlinks", path, maxLinks)
		}
		if filepath.IsAbs(link) {
			resolved = sep
		}
		pending = append(strings.Split(link, sep), pending...)
	}
	return resolved, nil
}

// parentIsLexical reports whether ".." in folder dir is filepath.Dir(dir), or
// dir is not a folder. In a firmlinked folder under /System/Volumes/Data or a
// /.vol folder on macOS, ".." is another folder.
func parentIsLexical(dir string) bool {
	parent, err := os.Stat(dir + string(filepath.Separator) + "..")
	if err != nil {
		return true
	}
	lexical, err := os.Stat(filepath.Dir(dir))
	return err == nil && os.SameFile(parent, lexical)
}

// readLink returns the target of the symlink at path, or "" when path is not a
// symlink or does not exist.
func readLink(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) || err == nil && info.Mode()&fs.ModeSymlink == 0 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return os.Readlink(path)
}
