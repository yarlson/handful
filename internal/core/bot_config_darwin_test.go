package core

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAGrantedFileCallThroughAnotherMacOSPathToTheFileShowsACard(t *testing.T) {
	svc, bot := grantedFileBot(t)
	skills, err := filepath.EvalSymlinks(svc.SkillsDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(skills)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	for name, path := range map[string]string{
		"the Data volume's firmlink": filepath.Join("/System/Volumes/Data", skills, "x", "SKILL.md"),
		"volfs":                      fmt.Sprintf("/.vol/%d/%d/x/SKILL.md", stat.Dev, stat.Ino),
	} {
		if _, err := os.Stat(filepath.Dir(filepath.Dir(path))); err != nil {
			t.Logf("skip %s: %v", name, err)
			continue
		}
		if !showsCard(t, svc, fileCall(bot, "", "Write", filePathInput(path))) {
			t.Errorf("granted Write through %s (%s) ran with no card; want a card", name, path)
		}
	}
}
