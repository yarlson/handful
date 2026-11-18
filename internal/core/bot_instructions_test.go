package core

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// column returns what the database holds in the instructions column of bot or room id.
func column(t *testing.T, svc *Service, id string) string {
	t.Helper()
	var text string
	if err := svc.sqlDB.QueryRowContext(context.Background(), "SELECT instructions FROM bots WHERE id = ?", id).Scan(&text); err != nil {
		t.Fatal(err)
	}
	return text
}

// A bot's instructions are its CLAUDE.md: a save writes the file, and the bot, the list and
// the save's answer show what the file holds, also text written to it by hand. The database
// gets none of it.
func TestABotsInstructionsAreItsFile(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot, err := svc.CreateBot(ctx, "Scout", "Research things.")
	if err != nil {
		t.Fatal(err)
	}
	file := svc.InstructionsFile(bot.ID)
	if got := readFile(t, file); got != "Research things." {
		t.Fatalf("file after create = %q; want %q", got, "Research things.")
	}

	text := "Answer in French.\nCite sources."
	saved, err := svc.UpdateBot(ctx, bot.ID, nil, &text)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, file); got != text || saved.Instructions != text {
		t.Fatalf("after a save: file %q, answer %q; want both %q", got, saved.Instructions, text)
	}

	byHand := "# Rules\n\nAnswer in German.\n"
	writeFile(t, file, byHand)
	got, err := svc.GetBot(ctx, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Instructions != byHand {
		t.Fatalf("GetBot after a hand edit = %q; want %q", got.Instructions, byHand)
	}
	list, err := svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Instructions != byHand {
		t.Fatalf("ListBots after a hand edit = %+v; want the bot with %q", list, byHand)
	}

	name := "Scout Two"
	if _, err := svc.UpdateBot(ctx, bot.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, file); got != byHand {
		t.Fatalf("file after a rename = %q; want the hand-written %q", got, byHand)
	}
	if got := column(t, svc, bot.ID); got != "" {
		t.Fatalf("instructions column = %q; want \"\"", got)
	}
}

// A save changes only CLAUDE.md in the bot's folder, and a save that fails leaves no file behind.
func TestASaveOfInstructionsChangesOnlyTheFile(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	dir := svc.BotConfigDir(bot.ID)
	other := dir + "/CLAUDE.md.tmp"
	writeFile(t, other, "kept")
	text := "Answer in French."
	if _, err := svc.UpdateBot(ctx, bot.ID, nil, &text); err != nil {
		t.Fatal(err)
	}
	if got, kept := readFile(t, svc.InstructionsFile(bot.ID)), readFile(t, other); got != text || kept != "kept" {
		t.Fatalf("after a save: CLAUDE.md %q, CLAUDE.md.tmp %q; want %q and \"kept\"", got, kept, text)
	}

	file := svc.InstructionsFile(bot.ID)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(file+"/x", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := svc.writeInstructions(bot.ID, "Lost."); err == nil {
		t.Fatal("a save onto a folder named CLAUDE.md succeeded; want an error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "CLAUDE.md,CLAUDE.md.tmp" {
		t.Fatalf("folder after a failed save = %v; want only CLAUDE.md and CLAUDE.md.tmp", names)
	}
}

// A bot without a file has no instructions.
func TestABotWithoutAnInstructionsFileHasNone(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	if err := os.Remove(svc.InstructionsFile(bot.ID)); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetBot(ctx, bot.ID)
	if err != nil || got.Instructions != "" {
		t.Fatalf("GetBot = %q, %v; want no instructions", got.Instructions, err)
	}
}

// A save of a bot's instructions has no 8000-character limit, only a bound of 256 KiB, and a
// rename works on a larger file written by hand. A room's purpose keeps the 8000 limit.
func TestOnlyASaveOfBotInstructionsIsBounded(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	long := strings.Repeat("x", 256<<10)
	if _, err := svc.UpdateBot(ctx, bot.ID, nil, &long); err != nil {
		t.Fatalf("save 256 KiB: %v; want it saved", err)
	}
	tooLong := long + "x"
	if _, err := svc.UpdateBot(ctx, bot.ID, nil, &tooLong); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "instructions are larger than 256 KiB") {
		t.Fatalf("save 256 KiB and 1 byte: %v; want ErrInvalid", err)
	}
	if _, err := svc.CreateBot(ctx, "Big", tooLong); !errors.Is(err, ErrInvalid) {
		t.Fatalf("create with 256 KiB and 1 byte: %v; want ErrInvalid", err)
	}
	writeFile(t, svc.InstructionsFile(bot.ID), tooLong)
	name := "Renamed"
	if _, err := svc.UpdateBot(ctx, bot.ID, &name, nil); err != nil {
		t.Fatalf("rename a bot with a large hand-written file: %v", err)
	}

	purpose := strings.Repeat("x", 8000)
	if _, err := svc.CreateRoom(ctx, "Lounge", purpose, []string{bot.ID}); err != nil {
		t.Fatalf("room with a purpose of 8000 characters: %v; want it created", err)
	}
	purpose += "x"
	if _, err := svc.CreateRoom(ctx, "Lounge", purpose, []string{bot.ID}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "longer than 8000") {
		t.Fatalf("room with a purpose of 8001 characters: %v; want ErrInvalid", err)
	}
}

// A room's purpose stays in the database; a room has no instructions file.
func TestARoomsPurposeStaysInTheDatabase(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	room, err := svc.CreateRoom(ctx, "Lounge", "Plan trips.", []string{bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	purpose := "Plan holidays."
	if _, err := svc.UpdateBot(ctx, room.ID, nil, &purpose); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetBot(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Instructions != purpose || column(t, svc, room.ID) != purpose {
		t.Fatalf("room purpose = %q, column %q; want %q in both", got.Instructions, column(t, svc, room.ID), purpose)
	}
	list, err := svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range list {
		if b.ID == room.ID && b.Instructions != purpose {
			t.Fatalf("ListBots: room purpose = %q; want %q", b.Instructions, purpose)
		}
	}
	if _, err := os.Stat(svc.InstructionsFile(room.ID)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a room's instructions file: %v; want none", err)
	}
}

// Builder and the bots of a setup get their instructions as files.
func TestBuilderAndSetupBotsGetInstructionsFiles(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if err := svc.SeedBuilder(ctx); err != nil {
		t.Fatal(err)
	}
	bots, err := svc.ListBots(ctx)
	if err != nil || len(bots) != 1 {
		t.Fatalf("bots after the seed = %v, %v; want Builder", bots, err)
	}
	if got := readFile(t, svc.InstructionsFile(bots[0].ID)); got != builderInstructions {
		t.Fatalf("Builder's file = %q; want %q", got, builderInstructions)
	}
	created, err := svc.CreateSetup(ctx, Setup{Bots: []SetupBot{{Name: "Ada", Instructions: "Write tests."}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, svc.InstructionsFile(created[0].ID)); got != "Write tests." || created[0].Instructions != "Write tests." {
		t.Fatalf("setup bot: file %q, answer %q; want %q", got, created[0].Instructions, "Write tests.")
	}
}

// The move at start copies each bot's instructions column to its file once. A file that
// exists is kept, a rerun changes nothing, and a file the user deleted after the move stays
// deleted. Rooms and bots without instructions get no file. The column keeps its text.
func TestMoveInstructionsToFilesCopiesEachBotsInstructionsOnce(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.sqlDB.ExecContext(ctx, `INSERT INTO bots (id, name, kind, instructions, created_at, updated_at) VALUES
		('old', 'Old', 'agent', 'Answer in French.', 1, 1),
		('hand', 'Hand', 'agent', 'Answer in German.', 2, 2),
		('empty', 'Empty', 'agent', '', 3, 3),
		('room', 'Room', 'room', 'Plan trips.', 4, 4)`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(svc.BotConfigDir("hand"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, svc.InstructionsFile("hand"), "Written by hand.")

	if err := svc.MoveInstructionsToFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, svc.InstructionsFile("old")); got != "Answer in French." {
		t.Fatalf("moved file = %q; want the column's text", got)
	}
	if got := readFile(t, svc.InstructionsFile("hand")); got != "Written by hand." {
		t.Fatalf("hand-written file after the move = %q; want it kept", got)
	}
	for _, id := range []string{"empty", "room"} {
		if _, err := os.Stat(svc.InstructionsFile(id)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("file of %s: %v; want none", id, err)
		}
	}
	edit := "Answer in Spanish."
	if _, err := svc.UpdateBot(ctx, "old", nil, &edit); err != nil {
		t.Fatal(err)
	}
	if got := column(t, svc, "old"); got != "Answer in French." {
		t.Fatalf("column after the move and a save = %q; want the text it had", got)
	}

	if err := os.Remove(svc.InstructionsFile("old")); err != nil {
		t.Fatal(err)
	}
	if err := svc.MoveInstructionsToFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(svc.InstructionsFile("old")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a file deleted after the move: %v; want it to stay deleted", err)
	}
}

// A move stopped after a bot's file was written and before the bot was marked moved is
// finished by the next start, which keeps the file as it is then.
func TestMoveInstructionsToFilesFinishesAPartialRun(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.sqlDB.ExecContext(ctx, `INSERT INTO bots (id, name, kind, instructions, created_at, updated_at) VALUES ('b', 'B', 'agent', 'Old text.', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := svc.writeInstructions("b", "Edited since."); err != nil {
		t.Fatal(err)
	}
	if err := svc.MoveInstructionsToFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, svc.InstructionsFile("b")); got != "Edited since." {
		t.Fatalf("file = %q; want it kept", got)
	}
	left, err := svc.q.ListInstructionsToMove(ctx)
	if err != nil || len(left) != 0 {
		t.Fatalf("bots left to move = %v, %v; want none", left, err)
	}
}
