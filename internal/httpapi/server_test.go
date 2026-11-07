package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"yargpt/internal/agent"
	"yargpt/internal/core"
	"yargpt/internal/db"
	"yargpt/internal/oauthtest"
)

func newTestHandler(t *testing.T) (http.Handler, *core.Service) {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(context.Background(), filepath.Join(dir, "yargpt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	svc, err := core.New(conn, dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := agent.NewRunner(svc, agent.Config{ClaudePath: "/nonexistent"}, log)
	ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}}
	return New(svc, runner, ui, log), svc
}

func do(h http.Handler, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, "http://127.0.0.1:7777"+target, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var jsonHeader = map[string]string{"Content-Type": "application/json"}

func TestGuardRefusesForeignHostOriginAndNonJSONWrites(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := []struct {
		name   string
		method string
		host   string
		header map[string]string
		want   int
	}{
		{"loopback GET", "GET", "127.0.0.1:7777", nil, http.StatusOK},
		{"localhost GET", "GET", "localhost:7777", nil, http.StatusOK},
		{"rebinding host", "GET", "evil.example:7777", nil, http.StatusForbidden},
		{"foreign origin", "GET", "127.0.0.1:7777", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"same origin write", "POST", "127.0.0.1:7777", map[string]string{"Origin": "http://127.0.0.1:7777", "Content-Type": "application/json"}, http.StatusCreated},
		{"form-encoded write", "POST", "127.0.0.1:7777", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), c.method, "http://"+c.host+"/api/bots", strings.NewReader(`{"name":"A"}`))
			for k, v := range c.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("%s %s Host=%s headers=%v: status %d; want %d (body %q)", c.method, "/api/bots", c.host, c.header, rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// The UI sends DELETE requests without a body and so without a Content-Type.
func TestBodilessDeletesAreAccepted(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	bot, err := svc.CreateBot(ctx, "Scout", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteFact(ctx, bot.ID, core.ScopeAgent, core.TierLog, "A fact to forget."); err != nil {
		t.Fatal(err)
	}
	facts, err := svc.Memories(ctx, bot.ID)
	if err != nil || len(facts) != 1 {
		t.Fatalf("memories = %+v, %v", facts, err)
	}
	if rec := do(h, "DELETE", "/api/memory/"+facts[0].ID, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE memory without Content-Type: %d %s; want 204", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/api/bots/"+bot.ID, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE bot without Content-Type: %d %s; want 204", rec.Code, rec.Body)
	}
	if _, err := svc.GetBot(ctx, bot.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("bot after delete: err = %v; want ErrNotFound", err)
	}
}

func TestSendMessageIsIdempotentAndValidates(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := do(h, "POST", "/api/bots", `{"name":"Scout"}`, jsonHeader)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create bot: %d %s", rec.Code, rec.Body)
	}
	var bot struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &bot); err != nil {
		t.Fatal(err)
	}

	first := do(h, "POST", "/api/bots/"+bot.ID+"/messages", `{"text":"hi","clientNonce":"n1"}`, jsonHeader)
	second := do(h, "POST", "/api/bots/"+bot.ID+"/messages", `{"text":"hi","clientNonce":"n1"}`, jsonHeader)
	if first.Code != http.StatusAccepted || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d; want 202 then 200", first.Code, second.Code)
	}
	var a, b struct {
		TurnID    string
		Duplicate bool
	}
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(second.Body.Bytes(), &b)
	if a.TurnID == "" || a.TurnID != b.TurnID || !b.Duplicate {
		t.Fatalf("responses = %+v, %+v; want the same turn and duplicate=true", a, b)
	}

	if rec := do(h, "POST", "/api/bots/"+bot.ID+"/messages", `{"text":"  ","clientNonce":"n2"}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty text: %d; want 400", rec.Code)
	}
	if rec := do(h, "POST", "/api/bots/missing/messages", `{"text":"hi","clientNonce":"n3"}`, jsonHeader); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown bot: %d; want 404", rec.Code)
	}
}

func TestEntriesCatchUpByRevAndSPAFallback(t *testing.T) {
	h, svc := newTestHandler(t)
	bot, err := svc.CreateBot(context.Background(), "Scout", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SubmitUserMessage(context.Background(), bot.ID, "one", "n1"); err != nil {
		t.Fatal(err)
	}
	rec := do(h, "GET", "/api/bots/"+bot.ID+"/entries?after=0", "", nil)
	var page struct{ Entries []core.Entry }
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Entries) != 1 {
		t.Fatalf("entries = %s; want one", rec.Body)
	}
	rec = do(h, "GET", "/api/bots/"+bot.ID+"/entries?after="+itoa(page.Entries[0].Rev), "", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Entries) != 0 {
		t.Fatalf("entries after last rev = %s; want none", rec.Body)
	}
	if rec := do(h, "GET", "/bots/some/deep/link", "", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("SPA fallback: %d %q; want index.html", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/nope", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown API path: %d; want 404", rec.Code)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestRoomEndpointsCreateListAndValidate(t *testing.T) {
	h, svc := newTestHandler(t)
	ana, err := svc.CreateBot(context.Background(), "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	bo, err := svc.CreateBot(context.Background(), "Bo", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := do(h, "POST", "/api/rooms", `{"name":"Plan","memberIds":["`+ana.ID+`"]}`, jsonHeader)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create room: %d %s", rec.Code, rec.Body)
	}
	var room struct {
		ID      string
		Kind    string
		Members []core.Member
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &room); err != nil || room.Kind != "room" || len(room.Members) != 1 {
		t.Fatalf("created room = %s; want kind room with one member", rec.Body)
	}
	rec = do(h, "PUT", "/api/rooms/"+room.ID+"/members", `{"memberIds":["`+ana.ID+`","`+bo.ID+`"]}`, jsonHeader)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Bo"`) {
		t.Fatalf("set members: %d %s; want Bo added", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/bots", "", nil)
	var all []struct {
		Name    string
		Kind    string
		Members []core.Member
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil || len(all) != 3 || all[2].Kind != "room" || len(all[2].Members) != 2 {
		t.Fatalf("bot list = %s; want two agents and the room with two members", rec.Body)
	}
	if rec := do(h, "POST", "/api/rooms", `{"name":"Empty","memberIds":[]}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("room without members: %d; want 400", rec.Code)
	}
}

func TestClearAndCompactEndpoints(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	bot, err := svc.CreateBot(ctx, "Scout", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetSession(ctx, bot.ID, "old-session"); err != nil {
		t.Fatal(err)
	}
	if rec := do(h, "POST", "/api/bots/"+bot.ID+"/clear", `{}`, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("clear: %d %s; want 204", rec.Code, rec.Body)
	}
	if got, _ := svc.GetBot(ctx, bot.ID); got.ClaudeSessionID != "" {
		t.Fatalf("session after clear = %q; want empty", got.ClaudeSessionID)
	}
	rec := do(h, "POST", "/api/bots/"+bot.ID+"/compact", `{}`, jsonHeader)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"queued":1`) {
		t.Fatalf("compact: %d %s; want 202 with one compaction queued", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/bots/missing/clear", `{}`, jsonHeader); rec.Code != http.StatusNotFound {
		t.Fatalf("clear of unknown bot: %d; want 404", rec.Code)
	}
}

func TestCreateRoutineEndpoint(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	bot, err := svc.CreateBot(ctx, "Standup", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := do(h, "POST", "/api/bots/"+bot.ID+"/routines", `{"name":"Daily","prompt":"Summarize yesterday.","schedule":"17 9 * * 1-5"}`, jsonHeader)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create routine: %d %s; want 201", rec.Code, rec.Body)
	}
	var r core.Routine
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || r.Schedule != "17 9 * * 1-5" || !r.Enabled || r.NextRunAt == nil {
		t.Fatalf("routine = %s; want an enabled routine with a next run", rec.Body)
	}
	if rec := do(h, "POST", "/api/bots/"+bot.ID+"/routines", `{"name":"Spam","prompt":"x","schedule":"* * * * *"}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("too-frequent schedule: %d; want 400", rec.Code)
	}
	if rec := do(h, "POST", "/api/bots/missing/routines", `{"name":"x","prompt":"x","schedule":"@daily"}`, jsonHeader); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown bot: %d %s; want 404", rec.Code, rec.Body)
	}
	room, err := svc.CreateRoom(ctx, "Release", "", []string{bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(h, "POST", "/api/bots/"+room.ID+"/routines", `{"name":"x","prompt":"x","schedule":"@daily"}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("routine on a room: %d %s; want 400", rec.Code, rec.Body)
	}
}

func TestReorderEndpoint(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	a, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateBot(ctx, "Bo", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(h, "PUT", "/api/bots/order", `{"ids":["`+b.ID+`","`+a.ID+`"]}`, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("reorder: %d %s; want 204", rec.Code, rec.Body)
	}
	rec := do(h, "GET", "/api/bots", "", nil)
	if i, j := strings.Index(rec.Body.String(), `"name":"Bo"`), strings.Index(rec.Body.String(), `"name":"Ana"`); i < 0 || j < 0 || i > j {
		t.Fatalf("bot list = %s; want Bo before Ana", rec.Body)
	}
	if rec := do(h, "PUT", "/api/bots/order", `{"ids":["`+a.ID+`"]}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("partial order: %d; want 400", rec.Code)
	}
}

func TestMessageMediaAreStoredAndServed(t *testing.T) {
	h, svc := newTestHandler(t)
	rec := do(h, "POST", "/api/bots", `{"name":"Scout"}`, jsonHeader)
	var bot struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &bot); err != nil {
		t.Fatal(err)
	}
	// Random pixels do not compress, so this PNG is larger than the 1 MiB body limit of other requests.
	noise := image.NewRGBA(image.Rect(0, 0, 700, 700))
	_, _ = rand.NewChaCha8([32]byte{1}).Read(noise.Pix)
	var pic bytes.Buffer
	if err := png.Encode(&pic, noise); err != nil {
		t.Fatal(err)
	}
	if pic.Len() <= 1<<20 {
		t.Fatalf("test image is %d bytes; want more than 1 MiB", pic.Len())
	}
	body, _ := json.Marshal(map[string]any{"text": "look", "clientNonce": "n1", "media": []string{base64.StdEncoding.EncodeToString(pic.Bytes())}})

	if rec := do(h, "POST", "/api/bots/"+bot.ID+"/messages", string(body), jsonHeader); rec.Code != http.StatusAccepted {
		t.Fatalf("send message with image: %d %s; want 202", rec.Code, rec.Body)
	}

	entries, err := svc.Entries(context.Background(), bot.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var user struct{ Media []string }
	for _, e := range entries {
		if e.Kind == core.KindUser {
			_ = json.Unmarshal(e.Body, &user)
		}
	}
	if len(user.Media) != 1 {
		t.Fatalf("user entry media = %v; want one", user.Media)
	}
	got := do(h, "GET", "/api/media/"+user.Media[0], "", nil)
	if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/png" || got.Header().Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(got.Body.Bytes(), pic.Bytes()) {
		t.Fatalf("GET image: %d, type %q, nosniff %q, %d bytes; want 200, image/png, nosniff, the %d bytes sent",
			got.Code, got.Header().Get("Content-Type"), got.Header().Get("X-Content-Type-Options"), got.Body.Len(), pic.Len())
	}
	if rec := do(h, "GET", "/api/media/"+uuid.NewString()+".png", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("GET unknown image: %d; want 404", rec.Code)
	}
	if rec := do(h, "GET", "/api/media/..%2Fyargpt.db", "", nil); rec.Code == http.StatusOK {
		t.Fatalf("GET ../yargpt.db: 200 with %d bytes; want it refused", rec.Body.Len())
	}
	if rec := do(h, "POST", "/api/bots/"+bot.ID+"/messages", `{"text":"x","clientNonce":"n2","media":["not base64!"]}`, jsonHeader); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "file 1 is not valid base64") {
		t.Fatalf("bad base64: %d %s; want 400 naming file 1", rec.Code, rec.Body)
	}
}

// Browsers fetch a video in ranges, to seek and to read its length first.
func TestVideoIsServedWithItsTypeAndInRanges(t *testing.T) {
	h, svc := newTestHandler(t)
	rec := do(h, "POST", "/api/bots", `{"name":"Scout"}`, jsonHeader)
	var bot struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &bot); err != nil {
		t.Fatal(err)
	}
	clip := append([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), bytes.Repeat([]byte{7}, 1000)...)
	body, _ := json.Marshal(map[string]any{"text": "", "clientNonce": "n1", "media": []string{base64.StdEncoding.EncodeToString(clip)}})
	if rec := do(h, "POST", "/api/bots/"+bot.ID+"/messages", string(body), jsonHeader); rec.Code != http.StatusAccepted {
		t.Fatalf("send video: %d %s; want 202", rec.Code, rec.Body)
	}
	entries, err := svc.Entries(context.Background(), bot.ID, 0, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %v, %v; want the user message", entries, err)
	}
	var user struct{ Media []string }
	if err := json.Unmarshal(entries[0].Body, &user); err != nil || len(user.Media) != 1 {
		t.Fatalf("user entry %s; want one media file", entries[0].Body)
	}

	got := do(h, "GET", "/api/media/"+user.Media[0], "", map[string]string{"Range": "bytes=8-11"})
	if got.Code != http.StatusPartialContent || got.Header().Get("Content-Type") != "video/mp4" || got.Body.String() != "mp42" {
		t.Fatalf("GET bytes 8-11: %d, type %q, body %q; want 206, video/mp4, \"mp42\"", got.Code, got.Header().Get("Content-Type"), got.Body)
	}
}

func TestConnectorSignInThroughTheBrowserNeverShowsTokens(t *testing.T) {
	as := oauthtest.New(t)
	dir := t.TempDir()
	conn, err := db.Open(context.Background(), filepath.Join(dir, "yargpt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	svc, err := core.New(conn, dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	connectors := `{"servers": {
		"tracker": {"url": "` + as.MCPURL() + `", "bots": ["b1"]},
		"wiki": {"url": "https://wiki.example/mcp", "headers": {"Authorization": "Bearer static-secret"}}
	}}`
	if err := os.WriteFile(filepath.Join(dir, "connectors.json"), []byte(connectors), 0o600); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := agent.NewRunner(svc, agent.Config{ClaudePath: "/nonexistent", OAuthCallbackURL: "http://127.0.0.1:7777/oauth/callback"}, log)
	h := New(svc, runner, nil, log)

	rec := do(h, "POST", "/api/connectors/tracker/connect", `{}`, jsonHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body)
	}
	var started struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	back := as.Authorize(t, started.URL)

	// The browser comes back with a top-level GET: no Origin, no JSON.
	rec = do(h, "GET", "/oauth/callback?state=forged&code=code-1", "", nil)
	if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "access-") {
		t.Fatalf("callback with a forged state: %d %s; want 400 without token data", rec.Code, rec.Body)
	}
	rec = do(h, "GET", back.RequestURI(), "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Connected") {
		t.Fatalf("callback: %d %s; want the Connected page", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "access-1") {
		t.Fatalf("callback page shows the token: %s", rec.Body)
	}
	if rec := do(h, "GET", back.RequestURI(), "", map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
		t.Errorf("callback with a foreign Origin: %d; want 403", rec.Code)
	}

	rec = do(h, "GET", "/api/connectors", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list connectors: %d %s", rec.Code, rec.Body)
	}
	for _, secret := range []string{"access-1", "refresh-1", "static-secret", "Authorization"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("GET /api/connectors shows %q: %s", secret, rec.Body)
		}
	}
	var list []struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Bots   []string `json:"bots"`
		Status string   `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"tracker": "connected", "wiki": "static"}
	for _, c := range list {
		if want[c.Name] != c.Status {
			t.Errorf("connector %s status = %q; want %q", c.Name, c.Status, want[c.Name])
		}
		delete(want, c.Name)
	}
	if len(want) != 0 {
		t.Errorf("connectors missing from the list: %v (list %s)", want, rec.Body)
	}

	if rec := do(h, "DELETE", "/api/connectors/tracker/token", "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("disconnect: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/connectors", "", nil); !strings.Contains(rec.Body.String(), `"needs-sign-in"`) {
		t.Errorf("after disconnect: %s; want tracker to need sign-in", rec.Body)
	}
}

func TestUserAddsAFactThatIsMarkedAsTheUsers(t *testing.T) {
	h, svc := newTestHandler(t)
	bot, err := svc.CreateBot(context.Background(), "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := do(h, "POST", "/api/bots/"+bot.ID+"/memory", `{"scope":"user","tier":"profile","fact":"Ask before you merge."}`, jsonHeader)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("add fact: %d %q; want 204 with no body (the web client parses JSON from any other 2xx)", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/api/bots/"+bot.ID+"/memory", "", nil)
	if !strings.Contains(rec.Body.String(), `"fact":"Ask before you merge.","learnedBy":"user"`) {
		t.Fatalf("memory = %s; want the fact learned by the user", rec.Body)
	}
	if rec := do(h, "POST", "/api/bots/"+bot.ID+"/memory", `{"scope":"user","tier":"profile","fact":""}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty fact: %d; want 400", rec.Code)
	}
}

func TestPatchRoutineTellsTheBotWhatTheUserChanged(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	bot, err := svc.CreateBot(ctx, "Standup", "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.CreateRoutine(ctx, bot.ID, "Daily", "Summarize yesterday.", "17 9 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(h, "PATCH", "/api/routines/"+r.ID, `{"schedule":"17 10 * * 1-5","enabled":false}`, jsonHeader); rec.Code != http.StatusOK {
		t.Fatalf("patch routine: %d %s; want 200", rec.Code, rec.Body)
	}
	entries, err := svc.Entries(ctx, bot.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := string(entries[len(entries)-1].Body)
	if !strings.Contains(last, `"type":"routine-updated"`) || !strings.Contains(last, `The user changed routine \"Daily\": schedule, paused.`) {
		t.Fatalf("last chat entry = %s; want a routine-updated notice naming schedule and paused", last)
	}
}
