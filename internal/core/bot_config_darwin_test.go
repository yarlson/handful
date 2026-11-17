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
	volfs := fmt.Sprintf("/.vol/%d/%d", stat.Dev, stat.Ino)
	firmlink := "/System/Volumes/Data" + skills
	firmlinkUp := "/System/Volumes/Data/private/../System/Volumes/Data" + skills
	// Each path is a folder that the OS resolves to the skills folder, and a file name.
	for _, tc := range []struct{ name, folder, file string }{
		{"the Data volume's firmlink", firmlink, "x/SKILL.md"},
		{"volfs", volfs, "x/SKILL.md"},
		{"a .. in the firmlink", firmlinkUp, "x/SKILL.md"},
		{"a .. in volfs", volfs, "../AGENTS.md"},
	} {
		path := tc.folder + "/" + tc.file
		if _, err := os.Stat(tc.folder); err != nil {
			t.Logf("skip %s: %v", tc.name, err)
			continue
		}
		if !showsCard(t, svc, fileCall(bot, "", "Write", filePathInput(path))) {
			t.Errorf("granted Write through %s (%s) ran with no card; want a card", tc.name, path)
		}
	}
}
