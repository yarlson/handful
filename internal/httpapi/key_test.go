package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"handful/internal/agent"
	"handful/internal/core"
)

const keyRefusal = "{\"error\":\"This needs Handful's key.\"}\n"

// writePatterns are the main listener's routes that write under /api/, but POST /api/key.
func writePatterns(s *server) []string {
	var out []string
	for _, p := range s.routes.patterns {
		method, path, ok := strings.Cut(p, " ")
		write := ok && method != http.MethodGet && method != http.MethodHead
		if write && strings.HasPrefix(path, "/api/") && p != "POST "+keyPath {
			out = append(out, p)
		}
	}
	return out
}

// Note test 1: every write on the main listener needs the key, in the header or the cookie.
func TestEveryWriteOnTheComputerNeedsHandfulsKey(t *testing.T) {
	f := newPhoneFixture(t, false)
	patterns := writePatterns(f.server)
	if len(patterns) < 50 {
		t.Fatalf("found %d write routes; want every one of the main listener's (over 50)", len(patterns))
	}
	without := []map[string]string{
		{KeyHeader: ""},
		{KeyHeader: "wrong-key"},
		{KeyHeader: "", "X-Handful-Token": testKey},
		{KeyHeader: "", "Cookie": "handful=" + testKey},
		{KeyHeader: "", "Cookie": keyCookie + "=wrong-key"},
		{KeyHeader: testKey + "x"},
	}
	with := []map[string]string{
		{KeyHeader: testKey},
		{KeyHeader: "", "Cookie": keyCookie + "=" + testKey},
	}
	for _, p := range patterns {
		method, path := concrete(p)
		for _, h := range without {
			h["Content-Type"] = "application/json"
			if rec := do(f.main, method, path, "{}", h); rec.Code != http.StatusUnauthorized || rec.Body.String() != keyRefusal {
				t.Errorf("%s without the key (%v): %d %q; want 401 from the key check", p, h, rec.Code, rec.Body)
			}
		}
		for _, h := range with {
			h["Content-Type"] = "application/json"
			if rec := do(f.main, method, path, "{}", h); rec.Code == http.StatusUnauthorized {
				t.Errorf("%s with the key (%v): 401 %q; want it past the key check", p, h, rec.Body)
			}
		}
	}
}

// Note test 2: a bot's own command can't allow its cards always without the key.
func TestAlwaysAllowWithoutTheKeyChangesNothing(t *testing.T) {
	f := newPhoneFixture(t, false)
	card, done := f.pendingCard("Bash")
	bots, err := f.svc.ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var bot string
	for _, b := range bots {
		if b.Name == "Bot Bash" {
			bot = b.ID
		}
	}
	rec := do(f.main, http.MethodPost, "/api/approvals/"+card, `{"decision":"always"}`, map[string]string{"Content-Type": "application/json", KeyHeader: ""})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("always without the key: %d %q; want 401", rec.Code, rec.Body)
	}
	if grants, err := f.svc.ToolGrants(context.Background(), bot); err != nil || len(grants) != 0 {
		t.Fatalf("grants after the refused call = %v, %v; want none", grants, err)
	}
	select {
	case ok := <-done:
		t.Fatalf("the card was decided (%v); want it still pending", ok)
	default:
	}
	if rec := do(f.main, http.MethodPost, "/api/approvals/"+card, `{"decision":"deny"}`, jsonHeader); rec.Code != http.StatusNoContent || <-done {
		t.Fatalf("deny with the key: %d %q", rec.Code, rec.Body)
	}
}

// Note test 3: reads need no key, the event stream too.
func TestReadsNeedNoKey(t *testing.T) {
	h, _ := newTestHandler(t)
	noKey := map[string]string{KeyHeader: ""}
	for _, path := range []string{"/api/bots", "/api/version", "/api/session", "/"} {
		if rec := do(h, http.MethodGet, path, "", noKey); rec.Code != http.StatusOK {
			t.Errorf("GET %s without the key: %d; want 200", path, rec.Code)
		}
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the event stream without the key: %d; want 200", resp.StatusCode)
	}
}

// Note test 4: the key page's POST /api/key gives a browser the cookie for the right key only,
// and only on the computer.
func TestTheKeyPageSetsTheCookieForTheRightKeyOnly(t *testing.T) {
	f := newPhoneFixture(t, false)
	noKey := map[string]string{"Content-Type": "application/json", KeyHeader: ""}
	rec := do(f.main, http.MethodPost, keyPath, `{"key":" `+testKey+`\n"}`, noKey)
	c := cookie(rec, keyCookie)
	if rec.Code != http.StatusNoContent || c == nil || c.Value != testKey || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 400*24*60*60 || c.Secure {
		t.Fatalf("the right key: %d, cookie %+v; want 204 and an HttpOnly, Strict cookie for / that lasts 400 days", rec.Code, c)
	}
	for _, body := range []string{`{"key":"wrong"}`, `{"key":""}`, `{}`, `{"key":"` + testKey + `x"}`} {
		rec := do(f.main, http.MethodPost, keyPath, body, noKey)
		if rec.Code != http.StatusForbidden || cookie(rec, keyCookie) != nil || !strings.Contains(rec.Body.String(), "That isn't Handful's key.") {
			t.Errorf("POST %s %s: %d, cookie %v; want 403 and no cookie", keyPath, body, rec.Code, cookie(rec, keyCookie))
		}
	}
	foreign := map[string]string{"Content-Type": "application/json", KeyHeader: "", "Origin": "http://evil.test"}
	if rec := do(f.main, http.MethodPost, keyPath, `{"key":"`+testKey+`"}`, foreign); rec.Code != http.StatusForbidden || cookie(rec, keyCookie) != nil {
		t.Fatalf("from a foreign origin: %d; want 403 and no cookie", rec.Code)
	}
	if rec := do(f.main, http.MethodPost, keyPath, `{"key":`, noKey); rec.Code != http.StatusBadRequest {
		t.Fatalf("a broken body: %d; want 400", rec.Code)
	}
	token, _ := f.pair(core.ScopeApprove)
	if rec := f.do(req{method: http.MethodPost, path: keyPath, body: `{"key":"` + testKey + `"}`, session: token}); rec.Code != http.StatusNotFound {
		t.Fatalf("on the phone listener: %d; want 404", rec.Code)
	}
}

// Note test 5: the session says whether this browser has the key, and where the key's file is.
func TestTheSessionSaysWhetherTheBrowserHasTheKey(t *testing.T) {
	f := newPhoneFixture(t, false)
	if rec := do(f.main, http.MethodGet, "/api/session", "", map[string]string{KeyHeader: ""}); rec.Body.String() != "{\"key\":false,\"keyFile\":\"/data/ui-key\",\"phone\":false}\n" {
		t.Fatalf("without the key: %q", rec.Body)
	}
	if rec := do(f.main, http.MethodGet, "/api/session", "", map[string]string{KeyHeader: "", "Cookie": keyCookie + "=" + testKey}); rec.Body.String() != "{\"key\":true,\"phone\":false}\n" {
		t.Fatalf("with the cookie: %q", rec.Body)
	}
	token, _ := f.pair(core.ScopeView)
	if rec := f.do(req{path: "/api/session", session: token}); strings.Contains(rec.Body.String(), "key") {
		t.Fatalf("on the phone listener: %q; want no key fields", rec.Body)
	}
}

// Note test 6: a paired phone writes without the key; its session is its lock.
func TestAPhoneWritesWithoutTheKey(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		bot, err := f.svc.CreateBot(context.Background(), "Ana", "")
		if err != nil {
			t.Fatal(err)
		}
		token, _ := f.pair(core.ScopeChat)
		if rec := f.do(req{method: http.MethodPost, path: "/api/bots/" + bot.ID + "/messages", body: `{"text":"hi","clientNonce":"n1"}`, session: token}); rec.Code != http.StatusAccepted {
			t.Fatalf("a phone's message: %d %q; want 202", rec.Code, rec.Body)
		}
	})
}

// Note test 7: a server given no key refuses every write.
func TestAServerWithoutAKeyRefusesEveryWrite(t *testing.T) {
	_, svc := newTestHandler(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(svc, agent.NewRunner(svc, agent.Config{ClaudePath: "/nonexistent"}, log), nil, log)
	for _, key := range []string{"", testKey, "anything"} {
		if rec := do(h, http.MethodPost, "/api/bots", `{"name":"A"}`, map[string]string{"Content-Type": "application/json", KeyHeader: key}); rec.Code != http.StatusUnauthorized {
			t.Errorf("a write with key %q: %d; want 401", key, rec.Code)
		}
		if rec := do(h, http.MethodPost, keyPath, `{"key":"`+key+`"}`, jsonHeader); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s with %q: %d; want 403", keyPath, key, rec.Code)
		}
	}
}
