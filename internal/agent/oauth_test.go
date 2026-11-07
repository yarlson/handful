package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"yargpt/internal/core"
	"yargpt/internal/oauthtest"
)

const callbackURL = "http://127.0.0.1:7777/oauth/callback"

func newOAuthHarness(t *testing.T) *harness {
	h := newHarness(t, "mcp-config")
	h.runner.cfg.OAuthCallbackURL = callbackURL
	return h
}

// signIn runs the sign-in flow of connector name as the user would in the browser.
func signIn(t *testing.T, h *harness, as *oauthtest.Server, name string) {
	t.Helper()
	authURL, err := h.runner.Connect(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	back := as.Authorize(t, authURL)
	if got := back.Scheme + "://" + back.Host + back.Path; got != callbackURL {
		t.Fatalf("redirect to %s; want %s", got, callbackURL)
	}
	if _, err := h.runner.FinishSignIn(context.Background(), back.Query()); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) mcpConfigServers(t *testing.T) map[string]mcpServer {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.dir, "mcp-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("MCP config %s is not JSON: %v", raw, err)
	}
	return config.MCPServers
}

func (h *harness) trackerStatus(t *testing.T) string {
	t.Helper()
	list, err := h.runner.Connectors()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Name == "tracker" {
			return c.Status
		}
	}
	t.Fatalf("connector tracker not listed in %+v", list)
	return ""
}

func TestSignInRegistersOnceChecksStateAndStoresTokensPrivately(t *testing.T) {
	as := oauthtest.New(t)
	h := newOAuthHarness(t)
	h.writeConnectors(t, `{"servers": {"tracker": {"url": "`+as.MCPURL()+`", "bots": []}}}`, 0o600)
	if got := h.trackerStatus(t); got != "needs-sign-in" {
		t.Fatalf("status before sign-in = %q; want needs-sign-in", got)
	}

	authURL, err := h.runner.Connect(context.Background(), "tracker")
	if err != nil {
		t.Fatal(err)
	}
	back := as.Authorize(t, authURL)
	forged := back.Query()
	forged.Set("state", "forged")
	if _, err := h.runner.FinishSignIn(context.Background(), forged); err == nil {
		t.Fatal("sign-in with a forged state succeeded")
	}
	tokenFile := filepath.Join(h.dir, "oauth", "tracker.json")
	if fileExists(tokenFile) {
		t.Fatal("a forged state stored tokens")
	}
	name, err := h.runner.FinishSignIn(context.Background(), back.Query())
	if err != nil || name != "tracker" {
		t.Fatalf("FinishSignIn = %q, %v; want tracker, nil", name, err)
	}
	if _, err := h.runner.FinishSignIn(context.Background(), back.Query()); err == nil {
		t.Fatal("a state was accepted twice")
	}

	for path, want := range map[string]os.FileMode{filepath.Dir(tokenFile): 0o700, tokenFile: 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s has mode %#o; want %#o", path, info.Mode().Perm(), want)
		}
	}
	raw, _ := os.ReadFile(tokenFile)
	if !strings.Contains(string(raw), "access-1") || !strings.Contains(string(raw), "refresh-1") {
		t.Errorf("token file %s does not hold the tokens", tokenFile)
	}
	if got := h.trackerStatus(t); got != "connected" {
		t.Errorf("status after sign-in = %q; want connected", got)
	}

	signIn(t, h, as, "tracker")
	if n := as.Registrations(); n != 1 {
		t.Errorf("client registrations = %d after two sign-ins; want 1 (the registration is reused)", n)
	}

	if err := h.runner.Disconnect("tracker"); err != nil {
		t.Fatal(err)
	}
	if fileExists(tokenFile) || h.trackerStatus(t) != "needs-sign-in" {
		t.Error("Disconnect left the tokens")
	}
}

func TestSessionGetsARefreshedTokenOnlyForTheConnectorsBots(t *testing.T) {
	as := oauthtest.New(t)
	as.ExpiresIn = 60 // within the 5-minute margin, so the next session refreshes
	h := newOAuthHarness(t)
	listed, other := h.newNamedBot(t, "Ana"), h.newNamedBot(t, "Bo")
	h.writeConnectors(t, `{"servers": {"tracker": {"url": "`+as.MCPURL()+`", "bots": ["`+listed.ID+`"]}}}`, 0o600)
	signIn(t, h, as, "tracker")
	h.start(t)

	h.runConnectorTurn(t, listed)
	tracker := h.mcpConfigServers(t)["tracker"]
	if tracker.URL != as.MCPURL() || tracker.Headers["Authorization"] != "Bearer access-2" {
		t.Fatalf("tracker server = %+v; want %s with the refreshed token access-2", tracker, as.MCPURL())
	}
	if n := as.Refreshes(); n != 1 {
		t.Errorf("refreshes = %d; want 1", n)
	}
	raw, _ := os.ReadFile(filepath.Join(h.dir, "oauth", "tracker.json"))
	if !strings.Contains(string(raw), "refresh-2") {
		t.Error("the rotated refresh token was not stored")
	}

	h.runConnectorTurn(t, other)
	if _, ok := h.mcpConfigServers(t)["tracker"]; ok {
		t.Error("a bot that the connector does not list got its server")
	}
}

func TestSessionWithoutSignInLeavesTheServerOutRunsAndSaysSo(t *testing.T) {
	as := oauthtest.New(t)
	h := newOAuthHarness(t)
	bot := h.newBot(t)
	h.writeConnectors(t, `{"servers": {
		"tracker": {"url": "`+as.MCPURL()+`", "bots": ["`+bot.ID+`"]},
		"open": {"url": "https://open.example/mcp", "headers": {}, "bots": ["`+bot.ID+`"]}
	}}`, 0o600)
	h.start(t)

	h.runConnectorTurn(t, bot)
	servers := h.mcpConfigServers(t)
	if _, ok := servers["tracker"]; ok {
		t.Error("the MCP config has a connector that is not signed in")
	}
	if _, ok := servers["open"]; !ok {
		t.Error("a connector with empty headers needs no sign-in, but the MCP config lacks it")
	}
	event := h.waitEntry(t, bot.ID, func(e core.Entry) bool {
		return e.Kind == core.KindEvent && bodyField(e, "type") == "connector-sign-in"
	})
	if text, _ := bodyField(event, "text").(string); !strings.Contains(text, "tracker needs sign-in") {
		t.Errorf("event text = %q; want it to say that tracker needs sign-in", text)
	}
}

func TestTokenIsNotSentToANewURLOfTheConnector(t *testing.T) {
	as, other := oauthtest.New(t), oauthtest.New(t)
	h := newOAuthHarness(t)
	bot := h.newBot(t)
	h.writeConnectors(t, `{"servers": {"tracker": {"url": "`+as.MCPURL()+`", "bots": ["`+bot.ID+`"]}}}`, 0o600)
	signIn(t, h, as, "tracker")
	h.writeConnectors(t, `{"servers": {"tracker": {"url": "`+other.MCPURL()+`", "bots": ["`+bot.ID+`"]}}}`, 0o600)
	h.start(t)

	h.runConnectorTurn(t, bot)
	if tracker, ok := h.mcpConfigServers(t)["tracker"]; ok {
		t.Errorf("tracker server = %+v after its url changed to %s; want it left out until a new sign-in", tracker, other.MCPURL())
	}
	raw, err := os.ReadFile(filepath.Join(h.dir, "mcp-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "access-1") {
		t.Errorf("MCP config holds the token issued for %s after the url changed: %s", as.MCPURL(), raw)
	}
	if got := h.trackerStatus(t); got != "needs-sign-in" {
		t.Errorf("status after the url changed = %q; want needs-sign-in", got)
	}
}

func TestSignInNeedsTheIssuerWhenTheServerPromisesIt(t *testing.T) {
	as := oauthtest.New(t)
	as.IssSupported = true
	h := newOAuthHarness(t)
	h.writeConnectors(t, `{"servers": {"tracker": {"url": "`+as.MCPURL()+`", "bots": []}}}`, 0o600)

	authURL, err := h.runner.Connect(context.Background(), "tracker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.runner.FinishSignIn(context.Background(), as.Authorize(t, authURL).Query()); err == nil {
		t.Fatal("a redirect without iss was accepted, but the metadata says that redirects carry it")
	}

	authURL, err = h.runner.Connect(context.Background(), "tracker")
	if err != nil {
		t.Fatal(err)
	}
	back := as.Authorize(t, authURL).Query()
	back.Set("iss", as.URL)
	if _, err := h.runner.FinishSignIn(context.Background(), back); err != nil {
		t.Fatalf("a redirect with iss %s failed: %v", as.URL, err)
	}
}

func TestSignInStartsWithAnHTTPSTokenEndpointOnAnotherHost(t *testing.T) {
	as := oauthtest.New(t)
	as.TokenEndpoint = "https://auth.example/token"
	h := newOAuthHarness(t)
	h.writeConnectors(t, `{"servers": {"tracker": {"url": "`+as.MCPURL()+`", "bots": []}}}`, 0o600)

	if _, err := h.runner.Connect(context.Background(), "tracker"); err != nil {
		t.Fatalf("Connect with token endpoint https://auth.example/token = %v; want the sign-in to start", err)
	}
}

func TestConnectorWithHeadersAndOAuthIsRejected(t *testing.T) {
	h := newOAuthHarness(t)
	h.writeConnectors(t, `{"servers": {"tracker": {"url": "https://tracker.example/mcp", "headers": {}, "oauth": {"clientId": "c"}}}}`, 0o600)
	if _, err := h.runner.Connectors(); err == nil || !strings.Contains(err.Error(), "oauth") {
		t.Fatalf("Connectors() error = %v; want an error about headers with oauth", err)
	}
}

func TestSignInRefusesAConnectorWithHeaders(t *testing.T) {
	h := newOAuthHarness(t)
	h.writeConnectors(t, `{"servers": {"wiki": {"url": "https://wiki.example/mcp", "headers": {"Authorization": "Bearer x"}}}}`, 0o600)
	if _, err := h.runner.Connect(context.Background(), "wiki"); err == nil {
		t.Fatal("Connect on a connector with static headers succeeded")
	}
	if _, err := h.runner.Connect(context.Background(), "../etc"); err == nil {
		t.Fatal("Connect on an unknown connector succeeded")
	}
	if err := h.runner.Disconnect("../connectors"); err == nil {
		t.Fatal("Disconnect accepted a name outside [a-z0-9-]+")
	}
}

// freePort returns a loopback port that nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func callbackPortConnectors(as *oauthtest.Server, port int) string {
	return fmt.Sprintf(`{"servers": {"slack": {"url": %q, "oauth": {"clientId": %q, "callbackPort": %d}, "bots": []}}}`, as.MCPURL(), oauthtest.ClientID, port)
}

func TestSignInWithACallbackPortComesBackOnThatPortAndClosesIt(t *testing.T) {
	as := oauthtest.New(t)
	port := freePort(t)
	redirect := fmt.Sprintf("http://localhost:%d/callback", port)
	as.Preregister(redirect)
	h := newOAuthHarness(t)
	h.writeConnectors(t, callbackPortConnectors(as, port), 0o600)

	authURL, err := h.runner.Connect(context.Background(), "slack")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("redirect_uri"); got != redirect {
		t.Fatalf("authorize redirect_uri = %q; want %q", got, redirect)
	}
	back := as.Authorize(t, authURL)

	forged := fmt.Sprintf("http://127.0.0.1:%d/callback?state=forged&code=code-1", port)
	if status, body := get(t, forged); status != http.StatusBadRequest {
		t.Fatalf("GET %s = %d %q; want 400", forged, status, body)
	}
	tokenFile := filepath.Join(h.dir, "oauth", "slack.json")
	if fileExists(tokenFile) {
		t.Fatal("a forged state stored tokens")
	}

	if status, body := get(t, back.String()); status != http.StatusOK || !strings.Contains(body, "Connected slack") {
		t.Fatalf("GET %s = %d %q; want 200 Connected slack", back, status, body)
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "access-1") || !strings.Contains(string(raw), redirect) {
		t.Errorf("token file %s = %s; want access-1 and redirect %s", tokenFile, raw, redirect)
	}
	if conn, err := new(net.Dialer).DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		conn.Close()
		t.Errorf("port %d still accepts connections after the sign-in finished", port)
	}
}

func TestSignInAgainWithACallbackPortReplacesTheEarlierListener(t *testing.T) {
	as := oauthtest.New(t)
	port := freePort(t)
	as.Preregister(fmt.Sprintf("http://localhost:%d/callback", port))
	h := newOAuthHarness(t)
	h.writeConnectors(t, callbackPortConnectors(as, port), 0o600)

	if _, err := h.runner.Connect(context.Background(), "slack"); err != nil {
		t.Fatal(err)
	}
	authURL, err := h.runner.Connect(context.Background(), "slack")
	if err != nil {
		t.Fatalf("second Connect: %v; want it to replace the first sign-in", err)
	}
	if status, body := get(t, as.Authorize(t, authURL).String()); status != http.StatusOK {
		t.Fatalf("callback of the second sign-in = %d %q; want 200", status, body)
	}
}

func TestSignInWithABusyCallbackPortFails(t *testing.T) {
	as := oauthtest.New(t)
	busy, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	h := newOAuthHarness(t)
	h.writeConnectors(t, callbackPortConnectors(as, port), 0o600)

	_, err = h.runner.Connect(context.Background(), "slack")
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatalf("Connect with port %d in use = %v; want an error that names the port", port, err)
	}
}

func get(t *testing.T, u string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}
