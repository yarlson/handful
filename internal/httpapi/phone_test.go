package httpapi

import (
	"bufio"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"handful/internal/access"
	"handful/internal/agent"
	"handful/internal/config"
	"handful/internal/core"
	"handful/internal/db"
)

const (
	phoneHost = "phone.example.com"
	teamHost  = "you.cloudflareaccess.com"
	appAUD    = "abcdef0123456789"
)

var (
	signKeyOnce sync.Once
	signKey     *rsa.PrivateKey
	otherKey    *rsa.PrivateKey
)

func phoneKeys() (*rsa.PrivateKey, *rsa.PrivateKey) {
	signKeyOnce.Do(func() {
		signKey, _ = rsa.GenerateKey(rand.Reader, 2048)
		otherKey, _ = rsa.GenerateKey(rand.Reader, 2048)
	})
	return signKey, otherKey
}

// phoneFixture is a server with phone access on, in tunnel mode (Cloudflare Access) or not
// (Tailscale), on a fixed clock, with a certs endpoint for the Access keys.
type phoneFixture struct {
	t        *testing.T
	svc      *core.Service
	main     http.Handler
	server   *server
	phone    *Phone
	tunnel   bool
	now      time.Time
	mu       sync.Mutex
	certsUp  atomic.Bool
	certHits atomic.Int32
	settings string // config.json
}

func newPhoneFixture(t *testing.T, tunnel bool) *phoneFixture {
	t.Helper()
	settings := config.PhoneSettings{Enabled: true, Addr: "127.0.0.1:0", Hosts: []string{phoneHost}, Proxy: config.ProxyTailscale}
	if tunnel {
		settings.Proxy, settings.TeamDomain, settings.AUD = config.ProxyCloudflare, teamHost, appAUD
	}
	return newPhoneFixtureWith(t, settings)
}

// newPhoneFixtureWith is the fixture with phone settings as config.json gives them.
func newPhoneFixtureWith(t *testing.T, settings config.PhoneSettings) *phoneFixture {
	t.Helper()
	tunnel := settings.Proxy == config.ProxyCloudflare
	dir := t.TempDir()
	conn, err := db.Open(context.Background(), filepath.Join(dir, "handful.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	f := &phoneFixture{t: t, tunnel: tunnel, now: time.UnixMilli(1_800_000_000_000), settings: filepath.Join(dir, "config.json")}
	f.certsUp.Store(true)
	clock := func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.now
	}
	svc, err := core.New(conn, dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := agent.NewRunner(svc, agent.Config{ClaudePath: "/nonexistent"}, log)
	opts := []PhoneOption{WithPhoneClock(clock, 20*time.Millisecond)}
	if tunnel {
		key, _ := phoneKeys()
		certs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			f.certHits.Add(1)
			if !f.certsUp.Load() {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kid": "k1", "kty": "RSA",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}}})
		}))
		t.Cleanup(certs.Close)
		opts = append(opts, WithAccessOptions(access.WithCertsURL(certs.URL), access.WithClock(clock)))
	}
	if err := config.Write(f.settings, config.File{}); err != nil {
		t.Fatal(err)
	}
	f.phone = NewPhone(settings, log, opts...)
	ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}, "assets/app.js": {Data: []byte("js")}}
	f.server, f.main = build(svc, runner, ui, log, WithPhone(f.phone), WithSettings(Settings{Path: f.settings}))
	return f
}

func (f *phoneFixture) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// token is a valid Access token, or one changed by edit, signed by key.
func (f *phoneFixture) token(key *rsa.PrivateKey, edit func(head, claims map[string]any)) string {
	f.mu.Lock()
	now := f.now
	f.mu.Unlock()
	head := map[string]any{"alg": "RS256", "kid": "k1"}
	claims := map[string]any{"iss": "https://" + teamHost, "aud": []string{appAUD}, "exp": now.Add(time.Hour).Unix(), "email": "you@example.com"}
	if edit != nil {
		edit(head, claims)
	}
	enc := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signed := enc(head) + "." + enc(claims)
	d := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, d[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// pair stores a session with scope and returns its token.
func (f *phoneFixture) pair(scope string) (string, core.PhoneSession) {
	f.t.Helper()
	token, p, err := f.svc.CreatePhoneSession(context.Background(), "Pixel "+scope, scope, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return token, p
}

// req is a request to the phone listener as a phone sends it through the proxy: the public
// host, the session cookie, JSON for a write, and in tunnel mode the Access token.
type req struct {
	method, path, body string
	session            string
	header             map[string]string
	host               string
	noToken            bool
}

func (f *phoneFixture) do(r req) *httptest.ResponseRecorder {
	f.t.Helper()
	if r.method == "" {
		r.method = http.MethodGet
	}
	if r.host == "" {
		r.host = phoneHost
	}
	hr := httptest.NewRequestWithContext(context.Background(), r.method, "http://"+r.host+r.path, strings.NewReader(r.body))
	if r.method != http.MethodGet {
		hr.Header.Set("Content-Type", "application/json")
	}
	if f.tunnel && !r.noToken {
		key, _ := phoneKeys()
		hr.Header.Set(access.Header, f.token(key, nil))
	}
	if r.session != "" {
		hr.AddCookie(&http.Cookie{Name: phoneCookie, Value: r.session})
	}
	for k, v := range r.header {
		if v == "" {
			hr.Header.Del(k)
		} else {
			hr.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	f.phone.handler.ServeHTTP(rec, hr)
	return rec
}

// gated reports whether the phone listener's checks refused rec, rather than a route.
func gated(rec *httptest.ResponseRecorder) bool {
	return rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden || rec.Code == http.StatusTooManyRequests || rec.Code == http.StatusUnsupportedMediaType ||
		(rec.Code == http.StatusNotFound && !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json"))
}

// concrete fills a pattern's wildcards and returns its method and path.
func concrete(pattern string) (string, string) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		method, path = http.MethodGet, pattern
	}
	return method, regexp.MustCompile(`\{[a-z]+\}`).ReplaceAllString(path, "x")
}

func bothModes(t *testing.T, test func(t *testing.T, f *phoneFixture)) {
	for _, tunnel := range []bool{false, true} {
		name := "tailscale"
		if tunnel {
			name = "tunnel"
		}
		t.Run(name, func(t *testing.T) { test(t, newPhoneFixture(t, tunnel)) })
	}
}

// AC 5 and AC 26: every route a phone may use needs a session, and every other route of the
// main listener, the phone controls too, is closed to a phone even with a session.
func TestEveryPhoneRouteNeedsASessionAndNoOtherRouteAnswers(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		allowed := map[string]bool{}
		for _, r := range phoneRoutes {
			allowed[r.pattern] = true
			if !slices.Contains(f.phoneMainRoutes(), r.pattern) {
				t.Errorf("phone route %s is not a route of the main listener", r.pattern)
			}
			method, path := concrete(r.pattern)
			if rec := f.do(req{method: method, path: path, body: "{}"}); rec.Code != http.StatusUnauthorized {
				t.Errorf("%s without a session: %d; want 401", r.pattern, rec.Code)
			}
		}
		token, _ := f.pair(core.ScopeApprove)
		for _, pattern := range f.phoneMainRoutes() {
			if allowed[pattern] || pattern == "/" {
				continue
			}
			methods := []string{}
			if m, _, ok := strings.Cut(pattern, " "); ok {
				methods = append(methods, m)
			} else {
				methods = append(methods, http.MethodGet, http.MethodPost)
			}
			for _, m := range methods {
				_, path := concrete(pattern)
				rec := f.do(req{method: m, path: path, body: "{}", session: token})
				if rec.Code != http.StatusNotFound {
					t.Errorf("%s %s on the phone listener with a session: %d %q; want 404", m, path, rec.Code, rec.Body)
				}
			}
		}
	})
}

func (f *phoneFixture) phoneMainRoutes() []string { return f.server.routes.patterns }

func TestTheWebAppsFilesNeedNoSession(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		for _, path := range []string{"/", "/assets/app.js", "/chat/x", "/pair"} {
			if rec := f.do(req{path: path}); rec.Code != http.StatusOK {
				t.Errorf("GET %s: %d; want 200", path, rec.Code)
			}
		}
	})
}

// AC 6 and AC 8: a Host not in phone.hosts is refused, with a session too; AC 7: forwarded
// headers change nothing.
func TestOnlyThePhoneHostsAreAnsweredAndForwardedHeadersChangeNothing(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		token, _ := f.pair(core.ScopeApprove)
		for _, host := range []string{"127.0.0.1", "127.0.0.1:7778", "localhost", "evil.test", "phone.example.com.evil.test", "localhost.evil.test"} {
			if rec := f.do(req{path: "/api/bots", session: token, host: host}); rec.Code != http.StatusForbidden {
				t.Errorf("Host %s: %d; want 403", host, rec.Code)
			}
		}
		if rec := f.do(req{path: "/api/bots", session: token, host: "PHONE.example.com:443"}); rec.Code != http.StatusOK {
			t.Errorf("the host in capitals, with a port: %d; want 200", rec.Code)
		}
		forwarded := map[string]string{"X-Forwarded-Host": phoneHost, "X-Forwarded-For": "127.0.0.1", "Forwarded": "host=" + phoneHost, "Tailscale-User-Login": "you@example.com", "Tailscale-User-Name": "You"}
		if rec := f.do(req{path: "/api/bots", session: token, host: "evil.test", header: forwarded}); rec.Code != http.StatusForbidden {
			t.Errorf("a forwarded phone host: %d; want 403", rec.Code)
		}
		if rec := f.do(req{path: "/api/bots", header: forwarded}); rec.Code != http.StatusUnauthorized {
			t.Errorf("forwarded identity headers without a session: %d; want 401", rec.Code)
		}
	})
	// AC 8 on the main listener.
	h, _ := newTestHandler(t)
	for _, host := range []string{"evil.test", phoneHost} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+host+"/api/bots", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("main listener, Host %s: %d; want 403", host, rec.Code)
		}
	}
}

// AC 10 and AC 12.
func TestAForeignOriginAndANonJSONWriteAreRefused(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		token, _ := f.pair(core.ScopeApprove)
		for _, origin := range []string{"null", "https://evil.test", "http://" + phoneHost, "https://" + phoneHost + ".evil.test", "%zz"} {
			if rec := f.do(req{path: "/api/bots", session: token, header: map[string]string{"Origin": origin}}); rec.Code != http.StatusForbidden {
				t.Errorf("Origin %s: %d; want 403", origin, rec.Code)
			}
		}
		if rec := f.do(req{path: "/api/bots", session: token, header: map[string]string{"Origin": "https://" + phoneHost}}); rec.Code != http.StatusOK {
			t.Errorf("the phone's own origin: %d; want 200", rec.Code)
		}
		for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x", ""} {
			for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
				if rec := f.do(req{method: m, path: "/api/entries/x/reactions", body: "{}", session: token, header: map[string]string{"Content-Type": ct}}); rec.Code != http.StatusUnsupportedMediaType {
					t.Errorf("%s with %q: %d; want 415", m, ct, rec.Code)
				}
			}
		}
	})
}

// AC 11: the cookie's attributes; that a browser doesn't send it cross-site is the browser check.
func TestTheSessionCookieIsHostOnlySecureHTTPOnlyAndStrict(t *testing.T) {
	c := phoneSessionCookie("hp_x").String()
	for _, want := range []string{"__Host-handful_phone=hp_x", "Path=/", "Secure", "HttpOnly", "SameSite=Strict", "Max-Age=7776000"} {
		if !strings.Contains(c, want) {
			t.Errorf("cookie %q lacks %q", c, want)
		}
	}
	if strings.Contains(c, "Domain") {
		t.Errorf("cookie %q has a Domain", c)
	}
}

// AC 21–25: what each scope may do.
func TestEachScopeMayDoOnlyItsPart(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		view, _ := f.pair(core.ScopeView)
		chat, _ := f.pair(core.ScopeChat)
		approve, _ := f.pair(core.ScopeApprove)
		for _, r := range phoneRoutes {
			method, path := concrete(r.pattern)
			if method == http.MethodGet {
				continue
			}
			rec := f.do(req{method: method, path: path, body: "{}", session: view})
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Your phone can only view.") {
				t.Errorf("view: %s: %d %q; want 403", r.pattern, rec.Code, rec.Body)
			}
			want := r.scope != core.ScopeApprove
			if rec := f.do(req{method: method, path: path, body: "{}", session: chat}); gated(rec) == want {
				t.Errorf("chat: %s: %d %q; want it %s", r.pattern, rec.Code, rec.Body, map[bool]string{true: "through", false: "refused"}[want])
			}
			if rec := f.do(req{method: method, path: path, body: "{}", session: approve}); gated(rec) {
				t.Errorf("chat+approve: %s: %d %q; want it through", r.pattern, rec.Code, rec.Body)
			}
		}
		for _, r := range phoneRoutes {
			method, path := concrete(r.pattern)
			if method != http.MethodGet || path == "/api/events" {
				continue
			}
			if rec := f.do(req{path: path, session: view}); gated(rec) {
				t.Errorf("view: %s: %d; want it through", r.pattern, rec.Code)
			}
		}
		// A chat phone sends a message, a second after the one the loop sent.
		f.advance(time.Second)
		bot, err := f.svc.CreateBot(context.Background(), "Ana", "")
		if err != nil {
			t.Fatal(err)
		}
		if rec := f.do(req{method: http.MethodPost, path: "/api/bots/" + bot.ID + "/messages", body: `{"text":"hi","clientNonce":"n1"}`, session: chat}); rec.Code >= 300 {
			t.Fatalf("a chat phone sends a message: %d %q", rec.Code, rec.Body)
		}
	})
}

// pendingCard starts an approval request for tool and returns its card and the request's result.
func (f *phoneFixture) pendingCard(tool string) (string, chan bool) {
	f.t.Helper()
	bot, err := f.svc.CreateBot(context.Background(), "Bot "+tool, "")
	if err != nil {
		f.t.Fatal(err)
	}
	events, stop := f.svc.Hub().Subscribe()
	defer stop()
	done := make(chan bool, 1)
	go func() {
		ok, _, _ := f.svc.RequestApproval(context.Background(), core.ApprovalRequest{BotID: bot.ID, TranscriptID: bot.ID, TurnID: "t-" + tool, Tool: tool, Input: json.RawMessage(`{}`)})
		done <- ok
	}()
	for e := range events {
		if e.Type == "entry" && e.Entry.Kind == core.KindApproval {
			return e.Entry.ID, done
		}
	}
	return "", done
}

// AC 23–25 and AC 28: approval decisions from a phone.
func TestAPhoneAllowsOnceOrDeniesNeverAlwaysAndTheCardNamesIt(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		view, _ := f.pair(core.ScopeView)
		chat, _ := f.pair(core.ScopeChat)
		approve, dev := f.pair(core.ScopeApprove)
		card, done := f.pendingCard("Bash")
		for _, tok := range []string{view, chat} {
			for _, d := range []string{"once", "deny", "always"} {
				if rec := f.do(req{method: http.MethodPost, path: "/api/approvals/" + card, body: `{"decision":"` + d + `"}`, session: tok}); rec.Code != http.StatusForbidden {
					t.Errorf("%s from a view or chat phone: %d; want 403", d, rec.Code)
				}
			}
		}
		rec := f.do(req{method: http.MethodPost, path: "/api/approvals/" + card, body: `{"decision":"always"}`, session: approve})
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Always allow can only be chosen on the computer.") {
			t.Fatalf("always from a phone: %d %q; want 403 with the reason", rec.Code, rec.Body)
		}
		if rec := f.do(req{method: http.MethodPost, path: "/api/approvals/" + card, body: `{"decision":"once"}`, session: approve}); rec.Code != http.StatusNoContent || !<-done {
			t.Fatalf("once from a phone: %d %q", rec.Code, rec.Body)
		}
		e, err := f.svc.GetEntry(context.Background(), card)
		if err != nil || !strings.Contains(string(e.Body), `"decidedOn":"`+dev.Name+`"`) {
			t.Fatalf("card = %s, %v; want it to name %s", e.Body, err, dev.Name)
		}
		grants, _ := f.svc.ToolGrants(context.Background(), e.BotID)
		if len(grants) != 0 {
			t.Fatalf("grants = %v; want none", grants)
		}
		setup, done := f.pendingCard(core.ToolCreateSetup)
		if rec := f.do(req{method: http.MethodPost, path: "/api/approvals/" + setup, body: `{"decision":"once"}`, session: approve}); rec.Code != http.StatusForbidden {
			t.Fatalf("allow a setup from a phone: %d; want 403", rec.Code)
		}
		if rec := f.do(req{method: http.MethodPost, path: "/api/approvals/" + setup, body: `{"decision":"deny"}`, session: approve}); rec.Code != http.StatusNoContent || <-done {
			t.Fatalf("deny a setup from a phone: %d", rec.Code)
		}
	})
}

// AC 27: the MCP endpoint and the sign-in callback are not on the phone listener.
func TestTheBotsEndpointsAreNotOnThePhoneListener(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		token, _ := f.pair(core.ScopeApprove)
		for _, r := range []req{{path: "/mcp"}, {method: http.MethodPost, path: "/mcp", body: "{}"}, {path: "/mcp/x"}, {path: "/oauth/callback?state=x&code=y"}} {
			r.session = token
			if rec := f.do(r); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: %d; want 404", r.method, r.path, rec.Code)
			}
		}
		// The main listener still has them.
		for _, target := range []string{"/mcp", "/oauth/callback?state=x"} {
			rec := do(f.main, http.MethodGet, target, "", nil)
			if rec.Code == http.StatusNotFound {
				t.Errorf("main listener %s: 404; want it served", target)
			}
		}
	})
}

// AC 44: the session answer on both listeners.
func TestTheSessionSaysWhetherThisIsAPhone(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		if rec := do(f.main, http.MethodGet, "/api/session", "", nil); rec.Body.String() != "{\"phone\":false}\n" {
			t.Fatalf("main: %q", rec.Body)
		}
		for _, scope := range []string{core.ScopeView, core.ScopeChat, core.ScopeApprove} {
			token, p := f.pair(scope)
			rec := f.do(req{path: "/api/session", session: token})
			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got["phone"] != true || got["scope"] != scope || got["device"] != p.Name {
				t.Errorf("phone %s: %d %q", scope, rec.Code, rec.Body)
			}
		}
		if rec := f.do(req{path: "/api/session"}); rec.Code != http.StatusUnauthorized {
			t.Errorf("no session: %d; want 401", rec.Code)
		}
	})
}

// AC 18 and AC 20: revoke on the computer; the phone controls aren't on the phone listener.
func TestRevokingAPhoneEndsItsRequestsAndOnlyThem(t *testing.T) {
	f := newPhoneFixture(t, false)
	a, pa := f.pair(core.ScopeChat)
	b, _ := f.pair(core.ScopeChat)
	for _, path := range []string{"/api/phone/sessions", "/api/phone/sessions/" + pa.ID, "/api/phone/off"} {
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			if rec := f.do(req{method: m, path: path, body: "{}", session: a}); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s on the phone listener: %d; want 404", m, path, rec.Code)
			}
		}
	}
	rec := do(f.main, http.MethodGet, "/api/phone/sessions", "", nil)
	var list []core.PhoneSession
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("list: %d %q", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), a) {
		t.Fatal("the list shows a token")
	}
	if rec := do(f.main, http.MethodDelete, "/api/phone/sessions/"+pa.ID, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %q", rec.Code, rec.Body)
	}
	if rec := f.do(req{path: "/api/bots", session: a}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked phone: %d; want 401", rec.Code)
	}
	if rec := f.do(req{path: "/api/bots", session: b}); rec.Code != http.StatusOK {
		t.Fatalf("the other phone: %d; want 200", rec.Code)
	}
	if rec := do(f.main, http.MethodDelete, "/api/phone/sessions/missing", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("revoke a missing phone: %d", rec.Code)
	}
	if rec := do(f.main, http.MethodDelete, "/api/phone/sessions", "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke all: %d", rec.Code)
	}
	if rec := f.do(req{path: "/api/bots", session: b}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after revoke all: %d; want 401", rec.Code)
	}
}

// AC 29: the rate limits.
func TestAPhonesRequestsAreRateLimited(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		a, _ := f.pair(core.ScopeChat)
		b, _ := f.pair(core.ScopeChat)
		get := func(tok string) int { return f.do(req{path: "/api/version", session: tok}).Code }
		for i := range phoneBurst {
			if code := get(a); code != http.StatusOK {
				t.Fatalf("request %d of a burst: %d; want 200", i+1, code)
			}
		}
		if code := get(a); code != http.StatusTooManyRequests {
			t.Fatalf("request %d at once: %d; want 429", phoneBurst+1, code)
		}
		if code := get(b); code != http.StatusOK {
			t.Fatalf("another phone: %d; want 200", code)
		}
		// Then 20 a second.
		f.advance(time.Second)
		for i := range phoneRate {
			if code := get(a); code != http.StatusOK {
				t.Fatalf("request %d in the next second: %d; want 200", i+1, code)
			}
		}
		if code := get(a); code != http.StatusTooManyRequests {
			t.Fatalf("request %d in one second: %d; want 429", phoneRate+1, code)
		}
		// One message a second.
		bot, _ := f.svc.CreateBot(context.Background(), "Ana", "")
		send := func(n string) int {
			return f.do(req{method: http.MethodPost, path: "/api/bots/" + bot.ID + "/messages", body: `{"text":"hi","clientNonce":"` + n + `"}`, session: b}).Code
		}
		if code := send("1"); code >= 300 {
			t.Fatalf("a message: %d", code)
		}
		if code := send("2"); code != http.StatusTooManyRequests {
			t.Fatalf("a second message at once: %d; want 429", code)
		}
		f.advance(time.Second)
		if code := send("3"); code >= 300 {
			t.Fatalf("a message a second later: %d", code)
		}
		// 30 requests a minute without a session, from everyone.
		for i := range unpairedPerMin {
			if code := f.do(req{path: "/api/bots"}).Code; code != http.StatusUnauthorized {
				t.Fatalf("request %d without a session: %d; want 401", i+1, code)
			}
		}
		if code := f.do(req{path: "/api/bots"}).Code; code != http.StatusTooManyRequests {
			t.Fatalf("request %d without a session in a minute: %d; want 429", unpairedPerMin+1, code)
		}
		// They come back at one every 2 s.
		f.advance(2 * time.Second)
		if code := f.do(req{path: "/api/bots"}).Code; code != http.StatusUnauthorized {
			t.Fatalf("2 s later: %d; want 401", code)
		}
		if code := f.do(req{path: "/api/bots"}).Code; code != http.StatusTooManyRequests {
			t.Fatalf("a second one 2 s later: %d; want 429", code)
		}
	})
}

// AC 36–38: tunnel mode needs a valid Access token on every request.
func TestTunnelModeNeedsAValidAccessTokenOnEveryRequest(t *testing.T) {
	f := newPhoneFixture(t, true)
	token, _ := f.pair(core.ScopeApprove)
	key, other := phoneKeys()
	refused := func(name string, r req) {
		t.Helper()
		rec := f.do(r)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "This address needs a Cloudflare login.") {
			t.Errorf("%s: %d %q; want 403 and the login text", name, rec.Code, rec.Body)
		}
	}
	for _, path := range []string{"/", "/assets/app.js", "/pair", "/api/session", "/api/bots"} {
		refused("no token: "+path, req{path: path, session: token, noToken: true})
	}
	refused("no token: a write", req{method: http.MethodPost, path: "/api/entries/x/reactions", body: "{}", session: token, noToken: true})
	bad := map[string]string{
		"forged":         "forged-jwt-test",
		"other key":      f.token(other, nil),
		"wrong issuer":   f.token(key, func(_, c map[string]any) { c["iss"] = "https://other.cloudflareaccess.com" }),
		"wrong audience": f.token(key, func(_, c map[string]any) { c["aud"] = []string{"0000000000000000"} }),
		"expired":        f.token(key, func(_, c map[string]any) { c["exp"] = time.UnixMilli(1_800_000_000_000).Unix() }),
		"unknown key":    f.token(key, func(h, _ map[string]any) { h["kid"] = "nope" }),
	}
	for name, tok := range bad {
		refused(name, req{path: "/api/bots", session: token, noToken: true, header: map[string]string{access.Header: tok}})
	}
	if rec := f.do(req{path: "/api/bots", session: token}); rec.Code != http.StatusOK {
		t.Fatalf("a valid token: %d", rec.Code)
	}
	// A valid token without a session is still no session.
	if rec := f.do(req{path: "/api/bots"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a valid token without a session: %d; want 401", rec.Code)
	}
	// Cached keys keep working while the endpoint is down.
	f.certsUp.Store(false)
	f.advance(time.Minute)
	if rec := f.do(req{path: "/api/bots", session: token}); rec.Code != http.StatusOK {
		t.Fatalf("cached key, endpoint down: %d; want 200", rec.Code)
	}
}

func TestTunnelModeFailsClosedWhenTheKeysCantBeFetched(t *testing.T) {
	f := newPhoneFixture(t, true)
	token, _ := f.pair(core.ScopeApprove)
	f.certsUp.Store(false)
	if rec := f.do(req{path: "/api/bots", session: token}); rec.Code != http.StatusForbidden {
		t.Fatalf("keys unreachable: %d; want 403", rec.Code)
	}
}

// openPhone starts f's phone listener and returns a client that sends the phone's Host.
func (f *phoneFixture) openPhone() func(path, token string) (*http.Response, error) {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := f.phone.Start(ctx); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { cancel(); f.phone.Stop() })
	addr := f.phone.Addr()
	get := func(path, token string) (*http.Response, error) {
		r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+path, nil)
		if err != nil {
			return nil, err
		}
		r.Host = phoneHost
		r.AddCookie(&http.Cookie{Name: phoneCookie, Value: token})
		return http.DefaultClient.Do(r)
	}
	return get
}

// streamEnds reads resp's event stream and reports whether it ends within limit.
func streamEnds(resp *http.Response, limit time.Duration) bool {
	ended := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, bufio.NewReader(resp.Body))
		close(ended)
	}()
	select {
	case <-ended:
		return true
	case <-time.After(limit):
		resp.Body.Close()
		return false
	}
}

// AC 18: a revoked phone's event stream ends at its next check.
func TestARevokedPhonesStreamEndsAndAnothersStaysOpen(t *testing.T) {
	f := newPhoneFixture(t, false)
	a, pa := f.pair(core.ScopeView)
	b, _ := f.pair(core.ScopeView)
	get := f.openPhone() //nolint:bodyclose // closed by the defer below
	sa, err := get("/api/events", a)
	if err != nil {
		t.Fatal(err)
	}
	defer sa.Body.Close()
	sb, err := get("/api/events", b)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Body.Close()
	if sa.StatusCode != http.StatusOK || sb.StatusCode != http.StatusOK {
		t.Fatalf("streams: %d, %d; want 200", sa.StatusCode, sb.StatusCode)
	}
	if err := f.svc.RevokePhoneSession(context.Background(), pa.ID); err != nil {
		t.Fatal(err)
	}
	if !streamEnds(sa, 5*time.Second) {
		t.Fatal("the revoked phone's stream stayed open")
	}
	if streamEnds(sb, 200*time.Millisecond) {
		t.Fatal("the other phone's stream ended")
	}
}

// AC 4: the kill switch closes the port and the streams at once, without a restart, turns phone
// access off in config.json, and keeps the paired phones.
func TestTheKillSwitchClosesThePhoneListenerAtOnce(t *testing.T) {
	f := newPhoneFixture(t, false)
	token, _ := f.pair(core.ScopeView)
	get := f.openPhone() //nolint:bodyclose // closed by the defer below
	addr := f.phone.Addr()
	stream, err := get("/api/events", token)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("stream: %d; want 200", stream.StatusCode)
	}
	start := time.Now()
	if rec := do(f.main, http.MethodPost, "/api/phone/off", "{}", jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("off: %d %q", rec.Code, rec.Body)
	}
	if !streamEnds(stream, time.Second) {
		t.Fatal("the stream stayed open after the kill switch")
	}
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(context.Background(), "tcp", addr)
	if err == nil {
		conn.Close()
		t.Fatal("the phone port still accepts connections")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the kill switch took %v; want under 1 s", elapsed)
	}
	raw, _ := os.ReadFile(f.settings)
	file, err := config.Load(f.settings)
	if err != nil || file.Phone == nil || file.Phone.Enabled == nil || *file.Phone.Enabled {
		t.Fatalf("config.json = %s, %v; want phone.enabled false", raw, err)
	}
	if list, _ := f.svc.PhoneSessions(context.Background()); len(list) != 1 || list[0].RevokedAt != 0 {
		t.Fatalf("phones after the kill switch = %+v; want the phone kept", list)
	}
	if rec := do(f.main, http.MethodPost, "/api/phone/off", "{}", jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("off again: %d", rec.Code)
	}
}

// AC 1: without phone access the main listener has no phone controls.
func TestWithoutPhoneAccessThereAreNoPhoneControls(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, target := range []string{"/api/phone/sessions", "/api/phone/off"} {
		if rec := do(h, http.MethodPost, target, "{}", jsonHeader); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d; want no route", target, rec.Code)
		}
	}
	p := NewPhone(config.PhoneSettings{Enabled: true, Addr: "127.0.0.1:0", Hosts: []string{phoneHost}, Proxy: config.ProxyTailscale}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := p.Start(context.Background()); err == nil || p.Addr() != "" {
		t.Fatalf("Start without a handler = %v; want an error and no port", err)
	}
	p.Stop()
	if p.verifier != nil {
		t.Fatal("a Tailscale phone listener checks Access tokens")
	}
	cf := NewPhone(config.PhoneSettings{Proxy: config.ProxyCloudflare, TeamDomain: teamHost, AUD: appAUD}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if cf.verifier == nil {
		t.Fatal("a tunnel phone listener doesn't check Access tokens")
	}
}

// AC 3: the main listener answers as before (spec 6a748f8a section 2.1).
func TestTheMainListenerAnswersAsBefore(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := []struct {
		host, origin, ct, method, path string
		want                           int
	}{
		{host: "127.0.0.1:47831", method: "GET", path: "/api/bots", want: 200},
		{host: "localhost", method: "GET", path: "/api/bots", want: 200},
		{host: "mac.tail1234.ts.net", method: "GET", path: "/api/bots", want: 403},
		{host: "100.101.102.103", method: "GET", path: "/api/bots", want: 403},
		{host: "127.0.0.1.evil.test", method: "GET", path: "/api/bots", want: 403},
		{host: "localhost.evil.test", method: "GET", path: "/api/bots", want: 403},
		{host: "evil.test", method: "GET", path: "/api/bots", want: 403},
		{host: "127.0.0.1", origin: "https://evil.test", method: "GET", path: "/api/bots", want: 403},
		{host: "127.0.0.1", origin: "null", method: "GET", path: "/api/bots", want: 403},
		{host: "127.0.0.1", ct: "text/plain", method: "POST", path: "/api/bots", want: 415},
		{host: "127.0.0.1", ct: "application/x-www-form-urlencoded", method: "POST", path: "/api/bots", want: 415},
		{host: "127.0.0.1", ct: "application/json", method: "POST", path: "/api/bots", want: 400},
		{host: "127.0.0.1", method: "POST", path: "/mcp", ct: "application/json", want: 401},
	}
	for _, c := range cases {
		r := httptest.NewRequestWithContext(context.Background(), c.method, "http://"+c.host+c.path, strings.NewReader("{}"))
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.ct != "" {
			r.Header.Set("Content-Type", c.ct)
		}
		r.Header.Set("X-Forwarded-Host", "localhost")
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%s %s Host %s Origin %q Content-Type %q: %d; want %d", c.method, c.path, c.host, c.origin, c.ct, rec.Code, c.want)
		}
	}
}

// The listener's timeouts, and a clean start and stop log no error.
func TestThePhoneListenerStartsAndStopsCleanly(t *testing.T) {
	var logs strings.Builder
	var mu sync.Mutex
	log := slog.New(slog.NewTextHandler(writerFunc(func(b []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return logs.Write(b)
	}), nil))
	settings := config.PhoneSettings{Enabled: true, Addr: "127.0.0.1:0", Hosts: []string{phoneHost}, Proxy: config.ProxyTailscale}
	p := NewPhone(settings, log)
	if p.recheck != 15*time.Second {
		t.Fatalf("streams check their session every %v; want 15 s", p.recheck)
	}
	p.handler = http.NotFoundHandler()
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.srv.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("read header timeout = %v; want 10 s", p.srv.ReadHeaderTimeout)
	}
	p.Stop()
	p.Stop()
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "phone access is off") {
		t.Fatalf("log = %q; want no error and the stop", logs.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }

// A config.json written by hand, with names in capitals and the team domain as a URL, gives a
// phone listener that works.
func TestAHandWrittenConfigFileGivesAWorkingPhoneListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"phone":{"enabled":true,"hosts":[" PHONE.Example.com "],"proxy":"cloudflare",` +
		`"cloudflare":{"teamDomain":"https://YOU.cloudflareaccess.com/","aud":" ABCDEF0123456789 "}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	f := newPhoneFixtureWith(t, file.PhoneSettings())
	token, _ := f.pair(core.ScopeView)
	if rec := f.do(req{path: "/api/session", session: token}); rec.Code != http.StatusOK {
		t.Fatalf("a request to the hand-written settings: %d %q; want 200", rec.Code, rec.Body)
	}
}
