package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"handful/internal/db"
)

// Limits for the media files of one message. Claude Code shrinks a large
// image to fit the model, so these limits only bound storage and request size.
const (
	MaxMedia      = 4
	MaxImageBytes = 10 << 20
	MaxVideoBytes = 50 << 20
	MaxMediaBytes = 100 << 20 // all files of one message together
)

// mediaExts maps each accepted media type, sniffed from the content, to the
// extension of its stored file.
var mediaExts = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
	"video/mp4": ".mp4", "video/webm": ".webm", "video/quicktime": ".mov",
}

// mediaFile is the content of a file for a message. label names it in errors.
type mediaFile struct {
	label string
	data  []byte
}

// sniffMedia returns the content type of a media file.
func sniffMedia(data []byte) string {
	// net/http knows an MP4 file only by an "mp4" brand. A QuickTime movie,
	// such as a screen recording or a phone video, has the brand "qt  ".
	if len(data) >= 12 && string(data[4:8]) == "ftyp" && string(data[8:12]) == "qt  " {
		return "video/quicktime"
	}
	return http.DetectContentType(data)
}

// userBody is the body of a user entry. Media are file names in MediaDir.
type userBody struct {
	Text    string   `json:"text"`
	Media   []string `json:"media,omitempty"`
	Answers []string `json:"answers,omitempty"` // the questions (entry ids) it answers
}

// MediaDir holds the media files of messages.
func (s *Service) MediaDir() string { return filepath.Join(s.dataDir, "media") }

// saveMedia checks the files and writes them to MediaDir. It returns their
// file names. If it fails, it removes the files it wrote.
func (s *Service) saveMedia(files []mediaFile) ([]string, error) {
	if len(files) > MaxMedia {
		return nil, invalid("a message can carry at most %d files", MaxMedia)
	}
	exts := make([]string, len(files))
	total := 0
	for i, f := range files {
		typ := sniffMedia(f.data)
		ext, ok := mediaExts[typ]
		if !ok {
			return nil, invalid("%s is not a PNG, JPEG, GIF or WebP image, or an MP4, WebM or QuickTime video", f.label)
		}
		limit := MaxImageBytes
		if strings.HasPrefix(typ, "video/") {
			limit = MaxVideoBytes
		}
		if len(f.data) > limit {
			return nil, invalid("%s is larger than %d MB", f.label, limit>>20)
		}
		exts[i] = ext
		total += len(f.data)
	}
	if total > MaxMediaBytes {
		return nil, invalid("the files of a message are larger than %d MB together", MaxMediaBytes>>20)
	}
	names := make([]string, 0, len(files))
	for i, f := range files {
		names = append(names, uuid.NewString()+exts[i])
		if err := os.WriteFile(filepath.Join(s.MediaDir(), names[i]), f.data, 0o600); err != nil {
			return nil, errors.Join(fmt.Errorf("save %s: %w", f.label, err), s.removeMedia(names))
		}
	}
	return names, nil
}

// uploadedMedia labels the files that a user sends as "file 1", "file 2", ...
func uploadedMedia(data [][]byte) []mediaFile {
	files := make([]mediaFile, len(data))
	for i, d := range data {
		files[i] = mediaFile{label: fmt.Sprintf("file %d", i+1), data: d}
	}
	return files
}

// saveBotFiles stores files that a bot shares with a message. workspace is
// where the bot works in the turn: its own workspace or its team's folder. A
// relative path is in workspace. A file must be, after symlinks, in
// workspace, the media folder or a temp folder, so that a misled bot cannot
// post the user's own files; the content must be a media file within the
// limits.
func (s *Service) saveBotFiles(workspace string, paths []string) ([]string, error) {
	if len(paths) > MaxMedia {
		return nil, invalid("a message can carry at most %d files", MaxMedia)
	}
	files := make([]mediaFile, 0, len(paths))
	for _, p := range paths {
		path := p
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		path, err := s.allowedBotFile(workspace, path, p)
		if err != nil {
			return nil, err
		}
		data, err := readMediaFile(path, p)
		if err != nil {
			return nil, err
		}
		files = append(files, mediaFile{label: p, data: data})
	}
	return s.saveMedia(files)
}

// allowedBotFile resolves the symlinks of path and returns it when it lies in
// workspace, the media folder or a temp folder. label names the file in errors.
func (s *Service) allowedBotFile(workspace, path, label string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", invalid("%s does not exist", label)
	}
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	roots := append([]string{workspace, s.MediaDir()}, s.tempDirs...)
	for _, root := range roots {
		root, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(root, resolved); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", invalid("%s is outside your working directory, the media folder and the temp folders; copy it into your working directory first", label)
}

// readMediaFile reads a regular file of at most MaxVideoBytes. label names
// the file in errors.
func readMediaFile(path, label string) ([]byte, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, invalid("%s does not exist", label)
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", label, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return nil, invalid("%s is not a regular file", label)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxVideoBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if len(data) > MaxVideoBytes {
		return nil, invalid("%s is larger than %d MB", label, MaxVideoBytes>>20)
	}
	return data, nil
}

// removeMedia deletes stored media files. A file that is already gone is not an error.
func (s *Service) removeMedia(names []string) error {
	var errs []error
	for _, name := range names {
		if err := os.Remove(filepath.Join(s.MediaDir(), name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove media file %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// OpenMedia opens a stored media file by its name and returns its content
// type. A name that saveMedia cannot create is not found, so the name cannot
// reach outside MediaDir.
func (s *Service) OpenMedia(name string) (*os.File, string, error) {
	ext := filepath.Ext(name)
	typ := ""
	for t, e := range mediaExts {
		if e == ext {
			typ = t
		}
	}
	if _, err := uuid.Parse(strings.TrimSuffix(name, ext)); err != nil || typ == "" {
		return nil, "", notFound("media file", name)
	}
	f, err := os.Open(filepath.Join(s.MediaDir(), name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", notFound("media file", name)
	}
	if err != nil {
		return nil, "", fmt.Errorf("open media file %s: %w", name, err)
	}
	return f, typ, nil
}

// TurnMedia returns the file names, in MediaDir, of the media attached to the
// user message that started the turn.
func (s *Service) TurnMedia(ctx context.Context, botID, turnID string) ([]string, error) {
	raw, err := s.q.GetTurnUserEntryBody(ctx, db.GetTurnUserEntryBodyParams{BotID: botID, TurnID: turnID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the user message of turn %s: %w", turnID, err)
	}
	var body userBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return nil, fmt.Errorf("decode the user message of turn %s: %w", turnID, err)
	}
	return body.Media, nil
}
