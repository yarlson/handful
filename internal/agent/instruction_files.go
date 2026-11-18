package agent

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Claude Code reads its instruction files once, when its process starts. A process that stays
// open across turns keeps the copies it read, so Handful brings an edited file to it in the turn
// message.

const (
	// maxInstructionUpdate bounds one file's text in an update; a longer file is named only.
	maxInstructionUpdate = 32 << 10
	// maxRuleFiles bounds the walk of one rules folder.
	maxRuleFiles = 200
	// agentsMDPlugin is Claude Code's built-in plugin that loads AGENTS.md files.
	agentsMDPlugin = "cc-plugin-agents-md@builtin"
)

// instructionStamp tells whether an instruction file changed.
type instructionStamp struct {
	size    int64
	modTime time.Time
}

// loadedInstructions returns the instruction files that Claude Code loads for a process of a
// bot that works in dir, with the user-level folder configDir, by its rules for Handful's
// layout: the folders from dataDir down to dir (dir lies in dataDir, and turnSettings excludes
// every folder above it), then configDir. In the default mode, AGENTS.md files load only when
// none of the folders has a CLAUDE.md or CLAUDE.local.md file.
func loadedInstructions(dataDir, dir, configDir string) map[string]instructionStamp {
	dataDir, dir, configDir = absolute(dataDir), absolute(dir), absolute(configDir)
	mode := agentsMDMode(configDir)
	if mode == "managed-only" {
		return map[string]instructionStamp{}
	}
	var claudeMD, localMD, rules, agentsMD []string
	for _, folder := range instructionFolders(dataDir, dir) {
		claudeMD = append(claudeMD, existing(folder, "CLAUDE.md", ".claude/CLAUDE.md")...)
		localMD = append(localMD, existing(folder, "CLAUDE.local.md")...)
		rules = append(rules, ruleFiles(filepath.Join(folder, ".claude", "rules"))...)
		agentsMD = append(agentsMD, existing(folder, "AGENTS.md", ".claude/AGENTS.md")...)
	}
	// A CLAUDE.local.md does not load, as bots run without the local settings source, but it
	// still keeps AGENTS.md files out.
	if mode == "claude-md" || (mode != "claude-md-and-agents-md" && len(claudeMD)+len(localMD) > 0) {
		agentsMD = nil
	}
	files := slices.Concat(claudeMD, rules, agentsMD, existing(configDir, "CLAUDE.md"))
	files = append(files, ruleFiles(filepath.Join(configDir, "rules"))...)
	stamps := map[string]instructionStamp{}
	for _, path := range files {
		if info, err := os.Stat(path); err == nil {
			stamps[path] = instructionStamp{info.Size(), info.ModTime()}
		}
	}
	return stamps
}

// absolute returns path as an absolute path, so the bot can open it from its own working folder.
// When the server's working folder is gone, it returns path.
func absolute(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// instructionFolders returns dataDir and each folder below it down to dir.
func instructionFolders(dataDir, dir string) []string {
	rel, err := filepath.Rel(dataDir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return []string{dir}
	}
	folders := []string{dataDir}
	if rel == "." {
		return folders
	}
	folder := dataDir
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		folder = filepath.Join(folder, part)
		folders = append(folders, folder)
	}
	return folders
}

// existing returns the files among names in folder that are regular files.
func existing(folder string, names ...string) []string {
	var files []string
	for _, name := range names {
		path := filepath.Join(folder, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			files = append(files, path)
		}
	}
	return files
}

// ruleFiles returns the .md files in the rules folder dir and its subfolders, at most
// maxRuleFiles. A folder that cannot be read gives none.
func ruleFiles(dir string) []string {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && isRuleFile(path, entry) {
			files = append(files, path)
		}
		if len(files) == maxRuleFiles {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return files
}

// isRuleFile tells whether a walked entry is a rule file: a .md file that is, or links to, a
// regular file.
func isRuleFile(path string, entry fs.DirEntry) bool {
	if entry.IsDir() || !strings.HasSuffix(path, ".md") {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// agentsMDMode reads the AGENTS.md mode from the bot's settings.json in configDir. A missing,
// unreadable or unknown value gives "", the default mode.
func agentsMDMode(configDir string) string {
	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err != nil {
		return ""
	}
	var settings struct {
		PluginConfigs map[string]struct {
			Options struct {
				InstructionFiles string `json:"instructionFiles"`
			} `json:"options"`
		} `json:"pluginConfigs"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return ""
	}
	return settings.PluginConfigs[agentsMDPlugin].Options.InstructionFiles
}

// instructionsUpdate returns the turn-message block that brings a process that loaded the files
// in known up to date with current: the text of each file that is new or changed, and each file
// that is gone. It returns "" when nothing differs. The files hold text that bots and the user
// wrote, so "</" in them is written as "<\/" and cannot end the block.
func instructionsUpdate(known, current map[string]instructionStamp) string {
	var b strings.Builder
	for _, path := range slices.Sorted(maps.Keys(current)) {
		if old, ok := known[path]; ok && old == current[path] {
			continue
		}
		b.WriteString(fileUpdate(path))
	}
	for _, path := range slices.Sorted(maps.Keys(known)) {
		if _, ok := current[path]; !ok {
			b.WriteString(path + " was deleted, so what it said no longer applies.\n\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	text := "Instruction files changed after Claude Code loaded them for this session. Each file below replaces the copy with the same path in your context.\n\n" + b.String()
	return "<instructions-update>\n" + strings.ReplaceAll(text, "</", `<\/`) + "</instructions-update>\n"
}

// fileUpdate is one file's part of an instructions update.
func fileUpdate(path string) string {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path + " was deleted, so what it said no longer applies.\n\n"
	}
	if err != nil || len(data) > maxInstructionUpdate {
		return path + " changed. Read it again before you go on, and follow its current text.\n\n"
	}
	return "Contents of " + path + ":\n\n" + strings.TrimRight(string(data), "\n") + "\n\n"
}
