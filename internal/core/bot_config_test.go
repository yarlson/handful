package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"handful/internal/db"
)

// grantedFileBot returns a service whose approval cards expire at once, and a
// bot that the user always allows Write and Edit.
func grantedFileBot(t *testing.T) (*Service, Bot) {
	t.Helper()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	for _, tool := range []string{"Write", "Edit"} {
		if err := svc.q.AddToolGrant(context.Background(), db.AddToolGrantParams{BotID: bot.ID, ToolName: tool, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	svc.approvals.timeout = time.Millisecond
	return svc, bot
}

// fileCall is a Write or Edit call by the bot in its own chat or in room.
func fileCall(bot Bot, room, tool, input string) ApprovalRequest {
	transcript := bot.ID
	if room != "" {
		transcript = room
	}
	return ApprovalRequest{BotID: bot.ID, TranscriptID: transcript, TurnID: "t", Tool: tool, Input: json.RawMessage(input)}
}

func filePathInput(path string) string {
	raw, _ := json.Marshal(map[string]string{"file_path": path})
	return string(raw)
}

// showsCard reports whether the call waited on an approval card; the card expires at once.
func showsCard(t *testing.T, svc *Service, req ApprovalRequest) bool {
	t.Helper()
	ok, reason, err := svc.RequestApproval(context.Background(), req)
	if err != nil {
		t.Fatalf("RequestApproval(%s %s): %v", req.Tool, req.Input, err)
	}
	if ok {
		return false
	}
	if reason != "The user did not answer the approval request in time." {
		t.Fatalf("RequestApproval(%s %s) = denied with %q; want a card that expired", req.Tool, req.Input, reason)
	}
	return true
}

func TestAGrantedWriteOrEditToABotsClaudeCodeFolderShowsACard(t *testing.T) {
	svc, ana := grantedFileBot(t)
	bo, err := svc.CreateBot(context.Background(), "Bo", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		svc.BotConfigDir(ana.ID),
		filepath.Join(svc.BotConfigDir(ana.ID), "settings.json"),
		filepath.Join(svc.BotConfigDir(ana.ID), "CLAUDE.md"),
		filepath.Join(svc.BotConfigDir(ana.ID), "rules", "x.md"),
		filepath.Join(svc.BotConfigDir(ana.ID), "skills", "x", "SKILL.md"),
		filepath.Join(svc.BotConfigDir(bo.ID), "CLAUDE.md"),
	} {
		for _, tool := range []string{"Write", "Edit"} {
			if !showsCard(t, svc, fileCall(ana, "", tool, filePathInput(path))) {
				t.Errorf("granted %s to %s ran with no card; want a card", tool, path)
			}
		}
	}
}

func TestAGrantedWriteOrEditToTheSharedSkillsOrInstructionsShowsACard(t *testing.T) {
	svc, bot := grantedFileBot(t)
	for _, path := range []string{
		filepath.Join(svc.Workspace(bot.ID), ".claude", "skills", "x", "SKILL.md"),
		filepath.Join(svc.SkillsDir(), "x", "SKILL.md"),
		filepath.Join(svc.DataDir(), "AGENTS.md"),
		filepath.Join(svc.DataDir(), "CLAUDE.md"),
	} {
		for _, tool := range []string{"Write", "Edit"} {
			if !showsCard(t, svc, fileCall(bot, "", tool, filePathInput(path))) {
				t.Errorf("granted %s to %s ran with no card; want a card", tool, path)
			}
		}
	}
}

func TestAGrantedWriteOrEditElsewhereNeedsNoCard(t *testing.T) {
	svc, bot := grantedFileBot(t)
	svc.home = svc.DataDir()
	for _, path := range []string{
		filepath.Join(svc.Workspace(bot.ID), "notes.md"),
		filepath.Join(svc.Workspace(bot.ID), "claude", "CLAUDE.md"),
		filepath.Join(svc.Workspace(bot.ID), "AGENTS.md"),
		filepath.Join(svc.DataDir(), "bots", bot.ID, "claude.md"),
		filepath.Join(svc.DataDir(), "bots", "CLAUDE.md"),
		filepath.Join(svc.DataDir(), "notes", "skills", "x.md"),
		filepath.Join(svc.DataDir(), "skillsx", "x.md"),
		svc.DataDir() + "x/AGENTS.md",
		svc.DataDir(),
		"notes.md",
		"~/media/x.png",
	} {
		for _, tool := range []string{"Write", "Edit"} {
			if showsCard(t, svc, fileCall(bot, "", tool, filePathInput(path))) {
				t.Errorf("granted %s to %s showed a card; want none", tool, path)
			}
		}
	}
}

func TestAGrantedFileCallResolvesItsPathAsTheOSDoes(t *testing.T) {
	svc, bot := grantedFileBot(t)
	svc.home = svc.DataDir()
	ws := svc.Workspace(bot.ID)
	if err := os.Mkdir(filepath.Join(svc.SkillsDir(), "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"data":      svc.DataDir(),
		"dangles":   filepath.Join("..", "..", "..", "skills", "new"),
		"loop":      "loop",
		"in-skills": filepath.Join(svc.SkillsDir(), "sub"),
		"elsewhere": t.TempDir(),
	} {
		if err := os.Symlink(target, filepath.Join(ws, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, "file.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(svc.DataDir(), "AGENTS.md")
	if err := os.WriteFile(agents, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(agents, filepath.Join(ws, "agents.md")); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"a link to the data folder":                   filepath.Join(ws, "data", "AGENTS.md"),
		"a link to a skill folder not yet made":       filepath.Join(ws, "dangles"),
		"a relative path in the working folder":       ".claude/skills/x/SKILL.md",
		"a .. out of the working folder":              ws + "/../claude/CLAUDE.md",
		"a .. after a missing folder":                 ws + "/missing/../.claude/skills/x",
		"a .. after a link, as the OS reads it":       filepath.Join(ws, "in-skills") + "/../x/SKILL.md",
		"a .. after a link, cleaned before use":       filepath.Join(ws, "elsewhere") + "/../.claude/skills/x/SKILL.md",
		"other letter case":                           filepath.Join(svc.DataDir(), "Skills", "x", "SKILL.md"),
		"a folder in Unicode case folding":            filepath.Join(svc.DataDir(), "\u017fkills", "x", "SKILL.md"),
		"a bots folder in Unicode case folding":       filepath.Join(svc.DataDir(), "bot\u017f", bot.ID, "claude", "CLAUDE.md"),
		"a file not yet made in Unicode case folding": filepath.Join(svc.DataDir(), "AGENT\u017f.md"),
		"a hard link to a protected file":             filepath.Join(ws, "agents.md"),
		"a path that /proc resolves in the bot's run": "/proc/self/cwd/../claude/CLAUDE.md",
		"a ~ for the home folder":                     "~/AGENTS.md",
		"a link loop":                                 filepath.Join(ws, "loop", "x"),
		"a file used as a folder":                     filepath.Join(ws, "file.txt", "x"),
	} {
		if !showsCard(t, svc, fileCall(bot, "", "Write", filePathInput(path))) {
			t.Errorf("granted Write through %s (%s) ran with no card; want a card", name, path)
		}
	}
}

func TestAGrantedWriteToAHomePathWithNoKnownHomeFolderShowsACard(t *testing.T) {
	svc, bot := grantedFileBot(t)
	svc.home = ""
	if !showsCard(t, svc, fileCall(bot, "", "Write", filePathInput("~"))) {
		t.Error("granted Write of ~ with no known home folder ran with no card; want a card")
	}
}

func TestAGrantedFileCallInATeamResolvesARelativePathInTheTeamFolder(t *testing.T) {
	svc, bot := grantedFileBot(t)
	team, err := svc.CreateTeam(context.Background(), "Ship", "", []string{bot.ID}, TeamSettings{LeadID: bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !showsCard(t, svc, fileCall(bot, team.ID, "Edit", filePathInput(".claude/skills/x/SKILL.md"))) {
		t.Error("granted Edit of .claude/skills/x/SKILL.md in a team turn ran with no card; want a card")
	}
	if err := os.Symlink(svc.DataDir(), filepath.Join(svc.TeamWorkspace(team.ID), "data")); err != nil {
		t.Fatal(err)
	}
	if !showsCard(t, svc, fileCall(bot, team.ID, "Write", filePathInput("data/AGENTS.md"))) {
		t.Error("granted Write of data/AGENTS.md, a link in the team folder, ran with no card; want a card")
	}
	if showsCard(t, svc, fileCall(bot, team.ID, "Write", filePathInput("notes.md"))) {
		t.Error("granted Write of notes.md in a team turn showed a card; want none")
	}
}

func TestAGrantedFileCallWithNoFilePathShowsACard(t *testing.T) {
	svc, bot := grantedFileBot(t)
	for _, input := range []string{`{}`, `{"file_path":""}`, `{"file_path":7}`, `"a.md"`, `null`} {
		if !showsCard(t, svc, fileCall(bot, "", "Write", input)) {
			t.Errorf("granted Write with input %s ran with no card; want a card", input)
		}
	}
}

func TestAGrantOfAnotherToolIgnoresItsFilePath(t *testing.T) {
	svc, bot := grantedFileBot(t)
	if err := svc.q.AddToolGrant(context.Background(), db.AddToolGrantParams{BotID: bot.ID, ToolName: "Bash", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	input := filePathInput(filepath.Join(svc.DataDir(), "AGENTS.md"))
	if showsCard(t, svc, fileCall(bot, "", "Bash", input)) {
		t.Error("granted Bash with a file_path of AGENTS.md showed a card; want none")
	}
}

func TestTheUserCanAllowAWriteToABotsClaudeCodeFolder(t *testing.T) {
	svc, bot := grantedFileBot(t)
	svc.approvals.timeout = time.Minute
	events, stop := svc.Hub().Subscribe()
	defer stop()
	done := make(chan bool, 1)
	go func() {
		ok, _, _ := svc.RequestApproval(context.Background(), fileCall(bot, "", "Write", filePathInput(filepath.Join(svc.BotConfigDir(bot.ID), "CLAUDE.md"))))
		done <- ok
	}()
	var cardID string
	for cardID == "" {
		select {
		case e := <-events:
			if e.Type == "entry" && e.Entry.Kind == KindApproval {
				cardID = e.Entry.ID
			}
		case ok := <-done:
			t.Fatalf("Write to the bot's CLAUDE.md returned allowed=%v with no card; want a card", ok)
		}
	}
	if err := svc.ResolveApproval(context.Background(), cardID, DecisionOnce); err != nil {
		t.Fatal(err)
	}
	if !<-done {
		t.Fatal("Write to the bot's CLAUDE.md after Allow once = denied; want allowed")
	}
}

func TestARelativePathResolvesFromTheServersWorkingFolder(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := realPath("missing/x.md"); err != nil || got != filepath.Join(real, "missing", "x.md") {
		t.Fatalf("realPath(missing/x.md) = %q, %v; want %q", got, err, filepath.Join(real, "missing", "x.md"))
	}
}

func TestAPathFollowsAtMost32Symlinks(t *testing.T) {
	// The temp folder itself may be under a link, such as /var on macOS.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "end"), 0o755); err != nil {
		t.Fatal(err)
	}
	// link1 leads to end, and each link n+1 leads to link n.
	for n := 1; n <= 33; n++ {
		target := "end"
		if n > 1 {
			target = fmt.Sprintf("link%d", n-1)
		}
		if err := os.Symlink(target, filepath.Join(dir, fmt.Sprintf("link%d", n))); err != nil {
			t.Fatal(err)
		}
	}
	end := filepath.Join(dir, "end")
	if got, err := realPath(filepath.Join(dir, "link32", "x.md")); err != nil || got != filepath.Join(end, "x.md") {
		t.Errorf("realPath through 32 links = %q, %v; want %q", got, err, filepath.Join(end, "x.md"))
	}
	if got, err := realPath(filepath.Join(dir, "link33", "x.md")); err == nil {
		t.Errorf("realPath through 33 links = %q; want an error", got)
	}
}

func TestAGrantedWriteToAPathInTheHomeFolderNeedsNoCard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc, bot := grantedFileBot(t)
	if showsCard(t, svc, fileCall(bot, "", "Write", filePathInput("~/notes.md"))) {
		t.Error("granted Write of ~/notes.md showed a card; want none")
	}
}
