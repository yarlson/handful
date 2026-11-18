package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// syncedSkills is the folder in a bot's skills folder where Claude Code keeps the skills it syncs
// from the user's account.
const syncedSkills = "synced"

// SharedSkills is the owner of the skills that every bot loads. The skill functions take an
// owner: SharedSkills, or a bot's ID for the skills that only that bot loads.
const SharedSkills = ""

// The layers a skill is in: the shared skills folder, or one bot's own.
const (
	LayerShared = "shared"
	LayerBot    = "bot"
)

// Skill is a SKILL.md file in the shared skills folder or in a bot's Claude Code folder. Claude
// Code loads both: the shared ones through each workspace's .claude/skills link, and a bot's own
// as personal skills, which win over a shared skill with the same name.
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body,omitempty"`
	Layer       string `json:"layer"`
	// Replaced marks a shared skill that a bot's own skill with the same id hides from that bot.
	Replaced bool `json:"replaced,omitempty"`
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

// skillsDir returns the folder of an owner's skills and their layer. A bot owner must be an
// agent: a room has no Claude Code folder.
func (s *Service) skillsDir(ctx context.Context, owner string) (dir, layer string, err error) {
	if owner == SharedSkills {
		return s.SkillsDir(), LayerShared, nil
	}
	bot, err := s.GetBot(ctx, owner)
	if err != nil {
		return "", "", err
	}
	if bot.IsRoom() {
		return "", "", invalid("room %s has no skills of its own; only a bot has", owner)
	}
	return filepath.Join(s.BotConfigDir(owner), "skills"), LayerBot, nil
}

func skillPath(dir, layer, id string) (string, error) {
	if !validSlug.MatchString(id) {
		return "", invalid("skill id %q is not a valid folder name", id)
	}
	if layer == LayerBot && id == syncedSkills {
		return "", invalid("a bot's own skill cannot be named %q: Claude Code keeps the skills synced from the user's account in that folder; choose another name", id)
	}
	return filepath.Join(dir, id, skillFile), nil
}

// WriteSkill creates or replaces an owner's skill and returns it.
func (s *Service) WriteSkill(ctx context.Context, owner, name, description, body string) (Skill, error) {
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
	dir, layer, err := s.skillsDir(ctx, owner)
	if err != nil {
		return Skill{}, err
	}
	path, err := skillPath(dir, layer, id)
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
	return Skill{ID: id, Name: id, Description: description, Body: body, Layer: layer}, nil
}

// DeleteSkill deletes an owner's skill; a skill with the same id in the other layer stays.
func (s *Service) DeleteSkill(ctx context.Context, owner, id string) error {
	dir, layer, err := s.skillsDir(ctx, owner)
	if err != nil {
		return err
	}
	path, err := skillPath(dir, layer, id)
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

// GetSkill returns an owner's skill.
func (s *Service) GetSkill(ctx context.Context, owner, id string) (Skill, error) {
	dir, layer, err := s.skillsDir(ctx, owner)
	if err != nil {
		return Skill{}, err
	}
	return readSkill(dir, layer, id)
}

func readSkill(dir, layer, id string) (Skill, error) {
	path, err := skillPath(dir, layer, id)
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
	sk := parseSkill(id, string(raw))
	sk.Layer = layer
	return sk, nil
}

// ListSkills returns the skills that a chat loads, without their bodies: for a bot, its own
// skills and then the shared ones, with each shared skill that an own skill hides marked
// Replaced; for a room or SharedSkills, the shared skills only. Each layer is sorted by id.
func (s *Service) ListSkills(ctx context.Context, chat string) ([]Skill, error) {
	shared, err := listSkills(s.SkillsDir(), LayerShared)
	if err != nil {
		return nil, err
	}
	if chat == SharedSkills {
		return shared, nil
	}
	dir, _, err := s.skillsDir(ctx, chat)
	if errors.Is(err, ErrInvalid) {
		return shared, nil
	}
	if err != nil {
		return nil, err
	}
	own, err := listSkills(dir, LayerBot)
	if err != nil {
		return nil, err
	}
	for i, sk := range shared {
		shared[i].Replaced = slices.ContainsFunc(own, func(o Skill) bool { return o.ID == sk.ID })
	}
	return append(own, shared...), nil
}

// listSkills reads the skills of one folder; a missing folder has none. A skill's folder may be a
// link to a folder elsewhere, as Claude Code allows.
func listSkills(dir, layer string) ([]Skill, error) {
	dirs, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read skills folder %s: %w", dir, err)
	}
	var out []Skill
	for _, d := range dirs {
		folder, err := isFolder(dir, d)
		if err != nil {
			return nil, err
		}
		if !folder {
			continue
		}
		sk, err := readSkill(dir, layer, d.Name())
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
