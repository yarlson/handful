package move

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sessionA = "0b6a3c1e-7f43-4c55-9a43-3e2f8c1d0a77"
	sessionB = "5d0f9e2a-1c3b-4e8f-a7d6-2b9c4e1f8a30"
)

// claudeFolders returns a user's Claude Code folder with sessions A and B of
// the work folder /work/ana (key -work-ana), A with a subagent transcript,
// and an empty Claude Code folder for a bot.
func claudeFolders(t *testing.T) (from, to string) {
	t.Helper()
	from, to = filepath.Join(t.TempDir(), "user"), filepath.Join(t.TempDir(), "bot")
	files := map[string]string{
		sessionA + ".jsonl":                         "transcript A\n",
		sessionA + "/subagents/agent-1.jsonl":       "subagent A\n",
		sessionB + ".jsonl":                         "transcript B\n",
		sessionA + "/tool-results/toolu_01.txt":     "tool output A\n",
		"../-work-bo/" + sessionA + ".jsonl":        "Bo's transcript\n",
		"../-work-ana-old/" + sessionA + ".jsonl":   "other folder\n",
		"../../plans/plan.md":                       "plan\n",
		"../../settings.json":                       "{}\n",
		"../-work-ana/" + sessionB + "/notes.txt":   "B notes\n",
		"../-work-ana/" + sessionA + "/sentinel.md": "A sentinel\n",
	}
	for name, text := range files {
		path := filepath.Join(from, "projects", "-work-ana", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return from, to
}

// readTree returns every file under dir by its path relative to dir.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		out[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

func TestCopyTranscriptCopiesOneSessionOfOneFolderAndLeavesTheOriginals(t *testing.T) {
	from, to := claudeFolders(t)
	before := readTree(t, from)
	if err := CopyTranscript(from, to, "/work/ana", sessionA); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"projects/-work-ana/" + sessionA + ".jsonl":                     "transcript A\n",
		"projects/-work-ana/" + sessionA + "/subagents/agent-1.jsonl":   "subagent A\n",
		"projects/-work-ana/" + sessionA + "/tool-results/toolu_01.txt": "tool output A\n",
		"projects/-work-ana/" + sessionA + "/sentinel.md":               "A sentinel\n",
	}
	if got := readTree(t, to); !equalMaps(got, want) {
		t.Fatalf("bot folder = %v; want %v", got, want)
	}
	if after := readTree(t, from); !equalMaps(after, before) {
		t.Fatalf("user folder after the copy = %v; want it unchanged %v", after, before)
	}
	for _, path := range []string{"projects/-work-ana", "projects/-work-ana/" + sessionA + ".jsonl", "projects/-work-ana/" + sessionA + "/subagents/agent-1.jsonl"} {
		info, err := os.Stat(filepath.Join(to, path))
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: %v, %v; want it private", path, info, err)
		}
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func TestCopyTranscriptKeepsACopyThatExists(t *testing.T) {
	from, to := claudeFolders(t)
	if err := CopyTranscript(from, to, "/work/ana", sessionA); err != nil {
		t.Fatal(err)
	}
	// The bot's run goes on in its own folder; the user's original stays behind.
	copied := filepath.Join(to, "projects", "-work-ana", sessionA+".jsonl")
	if err := os.WriteFile(copied, []byte("transcript A\nturn 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CopyTranscript(from, to, "/work/ana", sessionA); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(copied); string(data) != "transcript A\nturn 2\n" {
		t.Fatalf("copy after a second call = %q; want the bot's own transcript kept", data)
	}
}

func TestCopyTranscriptFinishesACopyThatStoppedPartway(t *testing.T) {
	for _, tc := range []struct {
		name string
		left map[string]string // what the stopped copy left in the bot's projects/-work-ana
	}{
		{"in the session folder", map[string]string{".handful-copy-" + sessionA + "/subagents/agent-1.jsonl": "subag"}},
		{"before the transcript", map[string]string{sessionA + "/subagents/agent-1.jsonl": "subagent A\n", sessionA + "/tool-results/toolu_01.txt": "tool output A\n", sessionA + "/sentinel.md": "A sentinel\n", sessionA + ".jsonl.tmp": "trans"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, to := claudeFolders(t)
			for name, text := range tc.left {
				path := filepath.Join(to, "projects", "-work-ana", name)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := CopyTranscript(from, to, "/work/ana", sessionA); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"projects/-work-ana/" + sessionA + ".jsonl":                     "transcript A\n",
				"projects/-work-ana/" + sessionA + "/subagents/agent-1.jsonl":   "subagent A\n",
				"projects/-work-ana/" + sessionA + "/tool-results/toolu_01.txt": "tool output A\n",
				"projects/-work-ana/" + sessionA + "/sentinel.md":               "A sentinel\n",
			}
			if got := readTree(t, to); !equalMaps(got, want) {
				t.Fatalf("bot folder = %v; want %v", got, want)
			}
		})
	}
}

func TestCopyTranscriptUsesTheRealPathOfTheWorkFolder(t *testing.T) {
	from, to := claudeFolders(t)
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	key, _ := sessionKey(real)
	if err := os.Rename(filepath.Join(from, "projects", "-work-ana"), filepath.Join(from, "projects", key)); err != nil {
		t.Fatal(err)
	}
	if err := CopyTranscript(from, to, link, sessionA); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(to, "projects", key, sessionA+".jsonl")); err != nil || string(data) != "transcript A\n" {
		t.Fatalf("copy through a link to the work folder = %q, %v; want transcript A under the real path's key %s", data, err, key)
	}
}

func TestCopyTranscriptCopiesNothingItCannotName(t *testing.T) {
	from, to := claudeFolders(t)
	for _, tc := range []struct{ name, dir, session string }{
		{"a session that is not a UUID", "/work/ana", "../-work-bo/" + sessionA},
		{"a UUID in capitals", "/work/ana", strings.ToUpper(sessionA)},
		{"no session", "/work/ana", ""},
		{"a session with no transcript", "/work/ana", "9e7d1c2b-3a4f-4b5c-8d6e-7f8091a2b3c4"},
		{"a folder whose key Claude Code hashes", "/" + strings.Repeat("w", 200), sessionA},
		{"a folder with no sessions", "/work/cy", sessionA},
	} {
		if err := CopyTranscript(from, to, tc.dir, tc.session); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if got := readTree(t, to); len(got) != 0 {
			t.Fatalf("%s: bot folder = %v; want nothing", tc.name, got)
		}
	}
}
