package move

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RenameSkillTools rewrites the old tool names, mcp__yargpt__<name>, to
// Handful's, mcp__handful__<name>, in the skills under dataDir/skills, as
// the database migration does for saved text. It changes only the files that
// still hold an old name, so it can run at every start.
func RenameSkillTools(dataDir string) error {
	return renameSkillTools(dataDir, "mcp__yargpt__", "mcp__handful__")
}

// renameSkillTools replaces from with to in the skills under dataDir/skills.
func renameSkillTools(dataDir, from, to string) error {
	err := filepath.WalkDir(filepath.Join(dataDir, "skills"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !strings.HasSuffix(path, ".md") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(data, []byte(from)) {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return writeFile(path, bytes.ReplaceAll(data, []byte(from), []byte(to)), info.Mode().Perm())
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
