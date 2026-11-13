package core

import (
	"os"
	"path/filepath"
	"testing"
)

// The skills folder holds a plain skill, a skill linked from another folder, and links that are
// no skill: a broken one, one to a file and one that points at itself. Only the two skills list.
func TestListSkillsFollowsLinksToSkillFolders(t *testing.T) {
	svc := newTestService(t)
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(svc.SkillsDir(), name)); err != nil {
			t.Fatal(err)
		}
	}
	elsewhere := t.TempDir()
	write(filepath.Join(svc.SkillsDir(), "zeta-plain", skillFile), "---\nname: zeta-plain\ndescription: A plain folder.\n---\nDo it.\n")
	write(filepath.Join(elsewhere, "alpha", skillFile), "---\nname: alpha-linked\ndescription: A linked folder.\n---\nDo it too.\n")
	write(filepath.Join(elsewhere, "notes.md"), "not a skill")
	link(filepath.Join(elsewhere, "alpha"), "alpha-linked")
	link(filepath.Join(elsewhere, "gone"), "broken-link")
	link(filepath.Join(elsewhere, "notes.md"), "file-link")
	link("loop-link", "loop-link")

	got, err := svc.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	want := []Skill{
		{ID: "alpha-linked", Name: "alpha-linked", Description: "A linked folder."},
		{ID: "zeta-plain", Name: "zeta-plain", Description: "A plain folder."},
	}
	if len(got) != len(want) {
		t.Fatalf("ListSkills = %+v; want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListSkills = %+v; want %+v", got, want)
		}
	}
}
