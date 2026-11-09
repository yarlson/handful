package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// The first bytes of an MP4 file, a WebM file and a QuickTime movie: enough
// for the type check, which reads only the start of a file.
var (
	testMP4  = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")
	testWebM = []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01")
	testMOV  = []byte("\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00qt  ")
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func userEntryBody(t *testing.T, svc *Service, botID string) (body struct {
	Text  string
	Media []string
}, found bool,
) {
	t.Helper()
	entries, err := svc.Entries(context.Background(), botID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Kind == KindUser {
			if err := json.Unmarshal(e.Body, &body); err != nil {
				t.Fatal(err)
			}
			found = true
		}
	}
	return body, found
}

func TestMessageWithOnlyAnImageStoresTheImage(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newAgents(t, svc, "Ana")["Ana"]
	pic := testPNG(t)

	turnID, _, err := svc.SubmitUserMessage(ctx, bot.ID, "  ", "n1", pic)
	if err != nil {
		t.Fatalf("submit image-only message: %v", err)
	}

	body, _ := userEntryBody(t, svc, bot.ID)
	if body.Text != "" || len(body.Media) != 1 || !strings.HasSuffix(body.Media[0], ".png") {
		t.Fatalf("user entry = %+v; want no text and one .png image", body)
	}
	names, err := svc.TurnMedia(ctx, bot.ID, turnID)
	if err != nil || len(names) != 1 || names[0] != body.Media[0] {
		t.Fatalf("TurnMedia = %v, %v; want %v", names, err, body.Media)
	}
	f, typ, err := svc.OpenMedia(names[0])
	if err != nil || typ != "image/png" {
		t.Fatalf("open stored image: type %q, %v; want image/png", typ, err)
	}
	defer f.Close()
	stored, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(stored, pic) {
		t.Fatalf("stored image = %d bytes, %v; want the %d bytes sent", len(stored), err, len(pic))
	}
}

func TestMessageWithBadMediaIsRejectedAndStoresNothing(t *testing.T) {
	pic := testPNG(t)
	tooLarge := append(append([]byte{}, pic...), make([]byte, MaxImageBytes)...)
	videoAtLimit := append(append([]byte{}, testMP4...), make([]byte, MaxVideoBytes-len(testMP4))...)
	videoTooLarge := append(append([]byte{}, testMP4...), make([]byte, MaxVideoBytes)...)
	cases := []struct {
		name    string
		text    string
		images  [][]byte
		wantErr string
	}{
		{"five files", "look", [][]byte{pic, pic, pic, pic, pic}, "at most 4 files"},
		{"image over 10 MB", "look", [][]byte{tooLarge}, "file 1 is larger than 10 MB"},
		{"video over 50 MB", "look", [][]byte{videoTooLarge}, "file 1 is larger than 50 MB"},
		{"files over 100 MB together", "look", [][]byte{videoAtLimit, videoAtLimit, pic}, "larger than 100 MB together"},
		{"text file", "look", [][]byte{pic, []byte("hello, world")}, "file 2 is not a PNG, JPEG, GIF or WebP image, or an MP4, WebM or QuickTime video"},
		{"no text and no file", " ", nil, "message text is empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := newTestService(t)
			bot := newAgents(t, svc, "Ana")["Ana"]

			_, _, err := svc.SubmitUserMessage(context.Background(), bot.ID, c.text, "n1", c.images...)

			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error = %v; want invalid input with %q", err, c.wantErr)
			}
			if body, found := userEntryBody(t, svc, bot.ID); found {
				t.Fatalf("user entry %+v was stored; want none", body)
			}
			files, err := os.ReadDir(svc.MediaDir())
			if err != nil || len(files) != 0 {
				t.Fatalf("media dir has %d files, %v; want none", len(files), err)
			}
		})
	}
}

func TestOpenMediaFindsNothingOutsideTheStoredMedia(t *testing.T) {
	svc := newTestService(t)
	for _, name := range []string{"../yargpt.db", "yargpt.db", uuid.NewString() + ".txt", uuid.NewString() + ".png", ""} {
		if f, _, err := svc.OpenMedia(name); !errors.Is(err, ErrNotFound) {
			if f != nil {
				f.Close()
			}
			t.Errorf("OpenMedia(%q) error = %v; want not found", name, err)
		}
	}
}

// Files that users attached before videos were accepted are in "images".
func TestStartupMovesTheImagesDirToMedia(t *testing.T) {
	dir := t.TempDir()
	name := uuid.NewString() + ".png"
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "images", name), testPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(context.Background(), filepath.Join(dir, "yargpt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	svc, err := New(conn, dir, time.UTC)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	f, _, err := svc.OpenMedia(name)
	if err != nil {
		t.Fatalf("open the moved file: %v", err)
	}
	f.Close()
	if _, err := os.Stat(filepath.Join(dir, "images")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("images dir after startup: %v; want it gone", err)
	}
}

func TestVideosAreStoredAndOpenedWithTheirType(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newAgents(t, svc, "Ana")["Ana"]

	if _, _, err := svc.SubmitUserMessage(ctx, bot.ID, "clips", "n1", testMP4, testWebM, testMOV); err != nil {
		t.Fatalf("submit videos: %v", err)
	}

	body, _ := userEntryBody(t, svc, bot.ID)
	want := []struct{ ext, typ string }{{".mp4", "video/mp4"}, {".webm", "video/webm"}, {".mov", "video/quicktime"}}
	if len(body.Media) != len(want) {
		t.Fatalf("media = %v; want %d files", body.Media, len(want))
	}
	for i, w := range want {
		f, typ, err := svc.OpenMedia(body.Media[i])
		if err != nil || !strings.HasSuffix(body.Media[i], w.ext) || typ != w.typ {
			t.Errorf("file %d = %s, type %q, %v; want %s and %s", i+1, body.Media[i], typ, err, w.ext, w.typ)
			continue
		}
		f.Close()
	}
}

func TestBotFilesAreCopiedIntoTheMediaStore(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bots := newAgents(t, svc, "Ana", "Bo")
	ana := bots["Ana"]
	pic := testPNG(t)
	if err := os.WriteFile(filepath.Join(svc.Workspace(ana.ID), "chart.png"), pic, 0o600); err != nil {
		t.Fatal(err)
	}
	clip := filepath.Join(t.TempDir(), "demo.mov")
	if err := os.WriteFile(clip, testMOV, 0o600); err != nil {
		t.Fatal(err)
	}

	e, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "", []string{"chart.png", clip}, false, false, PostMeta{})
	if err != nil {
		t.Fatalf("bot message with files: %v", err)
	}
	var body struct{ Media []string }
	if err := json.Unmarshal(e.Body, &body); err != nil || len(body.Media) != 2 {
		t.Fatalf("bot entry = %s, %v; want two media files", e.Body, err)
	}
	for i, content := range [][]byte{pic, testMOV} {
		f, _, err := svc.OpenMedia(body.Media[i])
		if err != nil {
			t.Fatalf("open file %d: %v", i+1, err)
		}
		stored, _ := io.ReadAll(f)
		f.Close()
		if !bytes.Equal(stored, content) {
			t.Errorf("stored file %d has %d bytes; want the %d bytes of the bot's file", i+1, len(stored), len(content))
		}
	}

	room, err := svc.CreateRoom(ctx, "Plan", "", []string{ana.ID, bots["Bo"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostToRoom(ctx, room.ID, ana.ID, "t2", "the chart", []string{"chart.png"}, 0, false); err != nil {
		t.Fatalf("room post with a file: %v", err)
	}
	entries, err := svc.Entries(ctx, room.ID, 0, 0)
	if err != nil || len(entries) == 0 || !strings.Contains(string(entries[len(entries)-1].Body), `"media":["`) {
		t.Fatalf("room entries = %v, %v; want the post with its media", entries, err)
	}
}

func TestBadBotFilesAreRejectedAndStoreNothing(t *testing.T) {
	cases := []struct {
		name    string
		file    func(t *testing.T, workspace string) string
		wantErr string
	}{
		{"missing file", func(t *testing.T, _ string) string { return "nope.png" }, "nope.png does not exist"},
		{"directory", func(t *testing.T, ws string) string { return ws }, "is not a regular file"},
		{"text file", func(t *testing.T, ws string) string {
			if err := os.WriteFile(filepath.Join(ws, "notes.txt"), []byte("hello"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "notes.txt"
		}, "notes.txt is not a PNG, JPEG, GIF or WebP image, or an MP4, WebM or QuickTime video"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := newTestService(t)
			bot := newAgents(t, svc, "Ana")["Ana"]
			path := c.file(t, svc.Workspace(bot.ID))

			_, err := svc.AppendBotMessage(context.Background(), bot.ID, "t1", "see", []string{path}, false, false, PostMeta{})

			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error = %v; want invalid input with %q", err, c.wantErr)
			}
			entries, _ := svc.Entries(context.Background(), bot.ID, 0, 0)
			files, _ := os.ReadDir(svc.MediaDir())
			if len(entries) != 0 || len(files) != 0 {
				t.Fatalf("stored %d entries and %d files; want none", len(entries), len(files))
			}
		})
	}
}

func TestBotFilesComeOnlyFromTheWorkspaceTheMediaFolderAndTemp(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	tmp := t.TempDir()
	svc.tempDirs = []string{tmp}
	ana := newAgents(t, svc, "Ana")["Ana"]
	pic := testPNG(t)
	write := func(path string) string {
		t.Helper()
		if err := os.WriteFile(path, pic, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write(filepath.Join(svc.Workspace(ana.ID), "in-workspace.png"))
	inTemp := write(filepath.Join(tmp, "in-temp.png"))
	outside := write(filepath.Join(t.TempDir(), "photo.png"))
	if err := os.Symlink(outside, filepath.Join(svc.Workspace(ana.ID), "link.png")); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"in-workspace.png", inTemp} {
		if _, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "", []string{path}, false, false, PostMeta{}); err != nil {
			t.Errorf("file %s: %v; want it accepted", path, err)
		}
	}
	escape, err := filepath.Rel(svc.Workspace(ana.ID), outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, "link.png", escape} {
		_, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "", []string{path}, false, false, PostMeta{})
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("file %s outside the allowed folders: err = %v; want ErrInvalid", path, err)
		}
	}
}
