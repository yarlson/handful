package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

const (
	skillFile          = "SKILL.md"
	maxSkillBody       = 64000
	maxSkillDescLength = 1000
)

// Skill is a SKILL.md file in the shared skills directory. Claude Code loads
// these files itself through each workspace's .claude/skills link.
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body,omitempty"`
}

var (
	nonSlug   = regexp.MustCompile(`[^a-z0-9]+`)
	validSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// skillID derives the folder name from the skill name.
func skillID(name string) (string, error) {
	id := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if id == "" || len(id) > 64 {
		return "", invalid("skill name %q must contain 1 to 64 letters or digits", name)
	}
	return id, nil
}

func (s *Service) skillPath(id string) (string, error) {
	if !validSlug.MatchString(id) {
		return "", invalid("skill id %q is not a valid folder name", id)
	}
	return filepath.Join(s.SkillsDir(), id, skillFile), nil
}

// WriteSkill creates or replaces a skill and returns it.
func (s *Service) WriteSkill(name, description, body string) (Skill, error) {
	name = strings.TrimSpace(name)
	description = strings.Join(strings.Fields(description), " ")
	body = strings.TrimSpace(body)
	id, err := skillID(name)
	if err != nil {
		return Skill{}, err
	}
	if description == "" || utf8.RuneCountInString(description) > maxSkillDescLength {
		return Skill{}, invalid("skill description must be 1 to %d characters", maxSkillDescLength)
	}
	if body == "" || len(body) > maxSkillBody {
		return Skill{}, invalid("skill body must be 1 to %d bytes", maxSkillBody)
	}
	path, err := s.skillPath(id)
	if err != nil {
		return Skill{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Skill{}, fmt.Errorf("create skill folder %s: %w", id, err)
	}
	// JSON strings are valid YAML double-quoted scalars, so they escape safely.
	nameJSON, _ := json.Marshal(id)
	descJSON, _ := json.Marshal(description)
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s\n", nameJSON, descJSON, body)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return Skill{}, fmt.Errorf("write skill %s: %w", id, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return Skill{}, fmt.Errorf("save skill %s: %w", id, err)
	}
	return Skill{ID: id, Name: id, Description: description, Body: body}, nil
}

func (s *Service) DeleteSkill(id string) error {
	path, err := s.skillPath(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return notFound("skill", id)
	}
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		return fmt.Errorf("delete skill %s: %w", id, err)
	}
	return nil
}

// isFolder reports whether entry d of dir is a folder, or a link to one. A link that leads
// nowhere is no folder: a missing target, a path through a file, or a loop.
func isFolder(dir string, d fs.DirEntry) (bool, error) {
	if d.Type()&fs.ModeSymlink == 0 {
		return d.IsDir(), nil
	}
	info, err := os.Stat(filepath.Join(dir, d.Name()))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read skill link %s: %w", d.Name(), err)
	}
	return info.IsDir(), nil
}

func (s *Service) GetSkill(id string) (Skill, error) {
	path, err := s.skillPath(id)
	if err != nil {
		return Skill{}, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Skill{}, notFound("skill", id)
	}
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %s: %w", id, err)
	}
	return parseSkill(id, string(raw)), nil
}

// ListSkills returns every skill without its body, sorted by id. A skill's folder may be a link
// to a folder elsewhere, as Claude Code allows.
func (s *Service) ListSkills() ([]Skill, error) {
	dirs, err := os.ReadDir(s.SkillsDir())
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	var out []Skill
	for _, d := range dirs {
		folder, err := isFolder(s.SkillsDir(), d)
		if err != nil {
			return nil, err
		}
		if !folder {
			continue
		}
		sk, err := s.GetSkill(d.Name())
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sk.Body = ""
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// parseSkill reads the name and description from the YAML frontmatter. It
// handles the flat key: value form that WriteSkill produces and most
// hand-written skills use; other keys are ignored.
func parseSkill(id, content string) Skill {
	sk := Skill{ID: id, Name: id, Body: content}
	if !strings.HasPrefix(content, "---\n") {
		return sk
	}
	rest := content[4:]
	before, after, ok := strings.Cut(rest, "\n---")
	if !ok {
		return sk
	}
	sc := bufio.NewScanner(strings.NewReader(before))
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		value = unquoteYAML(strings.TrimSpace(value))
		switch strings.TrimSpace(key) {
		case "name":
			sk.Name = value
		case "description":
			sk.Description = value
		}
	}
	sk.Body = strings.TrimLeft(after, "\n")
	return sk
}

func unquoteYAML(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		var out string
		if json.Unmarshal([]byte(v), &out) == nil {
			return out
		}
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}
