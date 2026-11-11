package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"handful/internal/core"
)

const (
	oauthHTTPTimeout  = 30 * time.Second
	signInTimeout     = 10 * time.Minute
	refreshMargin     = 5 * time.Minute
	maxPendingSignIns = 64
	maxTokenFile      = 1 << 20
)

// Status values of a connector in GET /api/connectors.
const (
	StatusStatic      = "static" // headers in connectors.json
	StatusConnected   = "connected"
	StatusNeedsSignIn = "needs-sign-in"
	StatusExpired     = "expired"
)

// ConnectorStatus describes a connector without its secrets.
type ConnectorStatus struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Bots   []string `json:"bots"`
	Status string   `json:"status"`
}

// signIns holds the OAuth sign-ins in progress. Its lock also serializes
// token refreshes: an authorization server may rotate refresh tokens and
// revoke them all when one is used twice.
type signIns struct {
	client *http.Client

	mu      sync.Mutex
	pending map[string]pendingSignIn // state -> sign-in
	refresh sync.Mutex
}

type pendingSignIn struct {
	connector string
	file      tokenFile // registration and endpoints, without tokens
	config    *oauth2.Config
	verifier  string
	expires   time.Time
	// issRequired is true when the server sends iss in every redirect (RFC 9207).
	issRequired bool
	// stop closes the listener of an oauth.callbackPort sign-in; nil without one.
	stop func()
}

func newSignIns() *signIns {
	// A transport with its own DialContext opts out of oauthex's filter of
	// private addresses. Some connectors are on private networks.
	return &signIns{
		client:  &http.Client{Timeout: oauthHTTPTimeout, Transport: http.DefaultTransport.(*http.Transport).Clone()},
		pending: make(map[string]pendingSignIn),
	}
}

// tokenFile is <data>/oauth/<connector>.json. It holds secrets, so it has
// mode 0600 and never leaves the server.
type tokenFile struct {
	URL          string    `json:"url"` // the connector URL that the tokens are for
	Issuer       string    `json:"issuer"`
	Resource     string    `json:"resource"`
	TokenURL     string    `json:"tokenUrl"`
	RedirectURL  string    `json:"redirectUrl"`
	ClientID     string    `json:"clientId"`
	ClientSecret string    `json:"clientSecret,omitempty"`
	Registered   bool      `json:"registered"` // the client comes from dynamic registration
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
}

func (f *tokenFile) oauth2Config() *oauth2.Config {
	style := oauth2.AuthStyleAutoDetect
	if f.ClientSecret == "" {
		style = oauth2.AuthStyleInParams
	}
	return &oauth2.Config{ClientID: f.ClientID, ClientSecret: f.ClientSecret, RedirectURL: f.RedirectURL, Endpoint: oauth2.Endpoint{TokenURL: f.TokenURL, AuthStyle: style}}
}

func (f *tokenFile) status(connectorURL string, now time.Time) string {
	switch {
	case f == nil || f.URL != connectorURL || f.AccessToken == "":
		return StatusNeedsSignIn
	case !f.Expiry.IsZero() && now.After(f.Expiry) && f.RefreshToken == "":
		return StatusExpired
	}
	return StatusConnected
}

func (r *Runner) connectorsPath() string { return filepath.Join(r.svc.DataDir(), "connectors.json") }

// tokenPath returns the token file of connector name. The name check keeps
// the path inside <data>/oauth.
func (r *Runner) tokenPath(name string) (string, error) {
	if !connectorName.MatchString(name) {
		return "", fmt.Errorf("%w: connector name %q must match [a-z0-9-]+", core.ErrInvalid, name)
	}
	return filepath.Join(r.svc.DataDir(), "oauth", name+".json"), nil
}

func (r *Runner) readTokenFile(name string) (*tokenFile, error) {
	path, err := r.tokenPath(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open token file: %w", err)
	}
	defer f.Close()
	var tf tokenFile
	if err := json.NewDecoder(io.LimitReader(f, maxTokenFile)).Decode(&tf); err != nil {
		return nil, fmt.Errorf("read token file %s: %w", path, err)
	}
	return &tf, nil
}

// writeTokenFile replaces the token file of name in one rename, so a reader
// never sees a partial file.
func (r *Runner) writeTokenFile(name string, tf *tokenFile) error {
	path, err := r.tokenPath(name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(tf)
	if err != nil {
		return fmt.Errorf("encode token file: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create token file: %w", err)
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return fmt.Errorf("write token file %s: %w", path, err)
	}
	return nil
}

// Connectors lists the connectors of connectors.json with their sign-in status.
func (r *Runner) Connectors() ([]ConnectorStatus, error) {
	connectors, err := loadConnectors(r.connectorsPath())
	if err != nil {
		return nil, err
	}
	out := []ConnectorStatus{}
	for name, c := range connectors {
		s := ConnectorStatus{Name: name, URL: c.URL, Bots: c.Bots, Status: StatusStatic}
		if s.Bots == nil {
			s.Bots = []string{}
		}
		if c.Headers == nil {
			tf, err := r.readTokenFile(name)
			if err != nil {
				return nil, err
			}
			s.Status = tf.status(c.URL, time.Now())
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b ConnectorStatus) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Disconnect deletes the tokens and the client registration of connector name.
func (r *Runner) Disconnect(name string) error {
	path, err := r.tokenPath(name)
	if err != nil {
		return err
	}
	r.oauth.refresh.Lock()
	defer r.oauth.refresh.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove token file: %w", err)
	}
	return nil
}

// Connect starts the sign-in to connector name and returns the URL to open
// in the browser. The authorization server sends the browser back to
// Config.OAuthCallbackURL, which calls FinishSignIn.
func (r *Runner) Connect(ctx context.Context, name string) (string, error) {
	connectors, err := loadConnectors(r.connectorsPath())
	if err != nil {
		return "", err
	}
	c, ok := connectors[name]
	switch {
	case !ok:
		return "", fmt.Errorf("%w: no connector %q in %s", core.ErrNotFound, name, r.connectorsPath())
	case c.Headers != nil:
		return "", fmt.Errorf("%w: connector %q uses headers from %s, not sign-in", core.ErrInvalid, name, r.connectorsPath())
	}
	redirect, port := r.cfg.OAuthCallbackURL, 0
	if c.OAuth != nil && c.OAuth.CallbackPort != 0 {
		port = c.OAuth.CallbackPort
		redirect = fmt.Sprintf("http://localhost:%d/callback", port)
	}
	if redirect == "" {
		return "", errors.New("sign-in is not available: the server has no OAuth callback URL")
	}
	o := r.oauth
	resource, scopes, asm, err := o.discover(ctx, c.URL)
	if err != nil {
		return "", fmt.Errorf("connector %q: %w", name, err)
	}
	tf := tokenFile{URL: c.URL, Issuer: asm.Issuer, Resource: resource, TokenURL: asm.TokenEndpoint, RedirectURL: redirect}
	switch prev, err := r.readTokenFile(name); {
	case err != nil:
		return "", err
	case c.OAuth != nil && c.OAuth.ClientID != "":
		tf.ClientID, tf.ClientSecret = c.OAuth.ClientID, c.OAuth.ClientSecret
	case prev != nil && prev.Registered && prev.Issuer == tf.Issuer && prev.RedirectURL == tf.RedirectURL:
		tf.ClientID, tf.ClientSecret, tf.Registered = prev.ClientID, prev.ClientSecret, true
	case asm.RegistrationEndpoint == "":
		return "", fmt.Errorf("connector %q: %s does not support dynamic client registration; set oauth.clientId in %s", name, asm.Issuer, r.connectorsPath())
	default:
		reg, err := oauthex.RegisterClient(ctx, asm.RegistrationEndpoint, &oauthex.ClientRegistrationMetadata{
			RedirectURIs:            []string{tf.RedirectURL},
			TokenEndpointAuthMethod: "none",
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			ClientName:              "Handful",
		}, o.client)
		if err != nil {
			return "", fmt.Errorf("connector %q: register with %s: %w", name, asm.Issuer, err)
		}
		tf.ClientID, tf.ClientSecret, tf.Registered = reg.ClientID, reg.ClientSecret, true
	}
	if c.OAuth != nil && len(c.OAuth.Scopes) > 0 {
		scopes = c.OAuth.Scopes
	}
	// Without offline_access, some servers issue no refresh token.
	if slices.Contains(asm.ScopesSupported, "offline_access") && !slices.Contains(scopes, "offline_access") {
		scopes = append(slices.Clone(scopes), "offline_access")
	}
	config := tf.oauth2Config()
	config.Endpoint.AuthURL = asm.AuthorizationEndpoint
	config.Scopes = scopes
	state, verifier := rand.Text(), oauth2.GenerateVerifier()

	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	for s, p := range o.pending {
		// A new sign-in to the connector replaces one that holds its callback port.
		if now.After(p.expires) || p.stop != nil && p.connector == name {
			p.stopListener()
			delete(o.pending, s)
		}
	}
	if len(o.pending) >= maxPendingSignIns {
		return "", fmt.Errorf("too many sign-ins in progress; finish one or wait %s", signInTimeout)
	}
	p := pendingSignIn{
		connector: name, file: tf, config: config, verifier: verifier, expires: now.Add(signInTimeout),
		issRequired: asm.AuthorizationResponseIssParameterSupported,
	}
	if port != 0 {
		if p.stop, err = r.listenForCallback(ctx, port); err != nil {
			return "", fmt.Errorf("connector %q: %w", name, err)
		}
	}
	o.pending[state] = p
	return config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("resource", resource)), nil
}

// FinishSignIn completes a sign-in with the query of the authorization
// server's redirect: it checks the state, exchanges the code and stores the
// tokens. It returns the connector name.
func (r *Runner) FinishSignIn(ctx context.Context, query url.Values) (string, error) {
	o := r.oauth
	o.mu.Lock()
	p, ok := o.pending[query.Get("state")]
	delete(o.pending, query.Get("state"))
	o.mu.Unlock()
	p.stopListener()
	if !ok || time.Now().After(p.expires) {
		return "", fmt.Errorf("%w: unknown or expired sign-in; start it again from Connectors", core.ErrInvalid)
	}
	if iss := query.Get("iss"); (iss != "" || p.issRequired) && iss != p.file.Issuer {
		return "", fmt.Errorf("%w: sign-in to %s came back from issuer %q, not %q", core.ErrInvalid, p.connector, iss, p.file.Issuer)
	}
	if e := query.Get("error"); e != "" {
		return "", fmt.Errorf("%w: sign-in to %s failed: %s %s", core.ErrInvalid, p.connector, e, query.Get("error_description"))
	}
	tok, err := p.config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, o.client), query.Get("code"),
		oauth2.VerifierOption(p.verifier), oauth2.SetAuthURLParam("resource", p.file.Resource))
	if err != nil {
		return "", fmt.Errorf("sign-in to %s: exchange the code: %w", p.connector, err)
	}
	tf := p.file
	tf.AccessToken, tf.RefreshToken, tf.Expiry = tok.AccessToken, tok.RefreshToken, tok.Expiry
	o.refresh.Lock()
	defer o.refresh.Unlock()
	if err := r.writeTokenFile(p.connector, &tf); err != nil {
		return "", err
	}
	return p.connector, nil
}

func (p pendingSignIn) stopListener() {
	if p.stop != nil {
		p.stop()
	}
}

// listenForCallback serves SignInCallback at /callback on port of both
// loopback addresses, because the browser may resolve localhost to either.
// The returned stop closes the listeners; it runs on its own after
// signInTimeout.
func (r *Runner) listenForCallback(ctx context.Context, port int) (func(), error) {
	busy := func(err error) error {
		return fmt.Errorf("listen on callback port %d: %w; another sign-in, such as Claude Code's, may hold it", port, err)
	}
	var lc net.ListenConfig
	ln4, err := lc.Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, busy(err)
	}
	listeners := []net.Listener{ln4}
	ln6, err := lc.Listen(ctx, "tcp", net.JoinHostPort("::1", strconv.Itoa(port)))
	switch {
	case err == nil:
		listeners = append(listeners, ln6)
	case errors.Is(err, syscall.EADDRINUSE):
		ln4.Close()
		return nil, busy(err)
	}
	// Without IPv6 loopback, localhost resolves only to 127.0.0.1.

	mux := http.NewServeMux()
	mux.Handle("GET /callback", r.SignInCallback())
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	// The callback closes the listeners; no idle connection may outlive them.
	srv.SetKeepAlivesEnabled(false)
	for _, ln := range listeners {
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, net.ErrClosed) {
				r.log.Warn("sign-in callback listener", "port", port, "err", err)
			}
		}()
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			for _, ln := range listeners {
				ln.Close()
			}
		})
	}
	time.AfterFunc(signInTimeout, stop)
	return stop, nil
}

// SignInCallback is where the authorization server sends the browser back
// after a connector sign-in. It is a top-level navigation, so it answers
// with a plain page, not JSON.
func (r *Runner) SignInCallback() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		name, err := r.FinishSignIn(req.Context(), req.URL.Query())
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, core.ErrInvalid) {
				status = http.StatusBadRequest
			}
			r.log.Warn("connector sign-in failed", "err", err)
			w.WriteHeader(status)
			fmt.Fprintf(w, "Sign-in failed: %v\n", err)
			return
		}
		fmt.Fprintf(w, "Connected %s. You can close this tab.\n", name)
	})
}

var errNotSignedIn = errors.New("not signed in")

// accessToken returns a token for connector name that is valid for at least
// refreshMargin, refreshing it when needed.
func (r *Runner) accessToken(ctx context.Context, name string, c connector) (string, error) {
	r.oauth.refresh.Lock()
	defer r.oauth.refresh.Unlock()
	tf, err := r.readTokenFile(name)
	if err != nil {
		return "", err
	}
	if tf.status(c.URL, time.Now()) != StatusConnected {
		return "", errNotSignedIn
	}
	if tf.Expiry.IsZero() || time.Until(tf.Expiry) > refreshMargin {
		return tf.AccessToken, nil
	}
	if tf.RefreshToken == "" {
		return "", errors.New("the token expires soon and there is no refresh token")
	}
	tok, err := tf.oauth2Config().TokenSource(context.WithValue(ctx, oauth2.HTTPClient, r.oauth.client), &oauth2.Token{RefreshToken: tf.RefreshToken}).Token()
	var rerr *oauth2.RetrieveError
	if errors.As(err, &rerr) && rerr.ErrorCode == "invalid_grant" {
		// The grant is gone for good; drop it so the status says to sign in again.
		tf.AccessToken, tf.RefreshToken = "", ""
		if werr := r.writeTokenFile(name, tf); werr != nil {
			return "", errors.Join(err, werr)
		}
	}
	if err != nil {
		return "", fmt.Errorf("refresh the token: %w", err)
	}
	tf.AccessToken, tf.RefreshToken, tf.Expiry = tok.AccessToken, tok.RefreshToken, tok.Expiry
	if err := r.writeTokenFile(name, tf); err != nil {
		return "", err
	}
	return tf.AccessToken, nil
}

// discover finds the resource, the scopes to ask for and the authorization
// server of the MCP server at serverURL (MCP authorization spec, 2025-06-18).
func (o *signIns) discover(ctx context.Context, serverURL string) (string, []string, *oauthex.AuthServerMeta, error) {
	challenges, err := o.challenge(ctx, serverURL)
	if err != nil {
		return "", nil, nil, err
	}
	var scopes []string
	var candidates [][2]string // metadata URL, resource it must name
	for _, ch := range challenges {
		if ch.Scheme != "bearer" {
			continue
		}
		scopes = strings.Fields(ch.Params["scope"])
		if u := ch.Params["resource_metadata"]; u != "" {
			candidates = append(candidates, [2]string{u, serverURL})
		}
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", nil, nil, err
	}
	origin := u.Scheme + "://" + u.Host
	candidates = append(candidates,
		[2]string{origin + "/.well-known/oauth-protected-resource/" + strings.TrimLeft(u.Path, "/"), serverURL},
		[2]string{origin + "/.well-known/oauth-protected-resource", origin})
	var prm *oauthex.ProtectedResourceMetadata
	var errs []error
	for _, c := range candidates {
		prm, err = oauthex.GetProtectedResourceMetadata(ctx, c[0], c[1], o.client)
		if err == nil && prm != nil {
			break
		}
		errs = append(errs, err)
	}
	if prm == nil {
		return "", nil, nil, fmt.Errorf("find OAuth protected resource metadata for %s: %w", serverURL, errors.Join(errs...))
	}
	if len(prm.AuthorizationServers) == 0 {
		return "", nil, nil, fmt.Errorf("protected resource metadata of %s names no authorization server", serverURL)
	}
	if len(scopes) == 0 {
		scopes = prm.ScopesSupported
	}
	issuer := prm.AuthorizationServers[0]
	asm, err := auth.GetAuthServerMetadata(ctx, issuer, o.client)
	if err != nil {
		return "", nil, nil, fmt.Errorf("authorization server metadata of %s: %w", issuer, err)
	}
	if asm == nil {
		return "", nil, nil, fmt.Errorf("authorization server %s publishes no metadata", issuer)
	}
	if !slices.Contains(asm.CodeChallengeMethodsSupported, "S256") {
		return "", nil, nil, fmt.Errorf("authorization server %s does not support PKCE with S256", issuer)
	}
	for _, endpoint := range []string{asm.AuthorizationEndpoint, asm.TokenEndpoint} {
		e, err := url.Parse(endpoint)
		if err != nil || (e.Scheme != "https" && (e.Scheme != "http" || !isLoopbackHost(e.Hostname()))) {
			return "", nil, nil, fmt.Errorf("authorization server %s: endpoint %q must use https", issuer, endpoint)
		}
	}
	return prm.Resource, scopes, asm, nil
}

// challenge sends an unauthenticated MCP initialize request and returns the
// WWW-Authenticate challenges of a 401 answer.
func (o *signIns) challenge(ctx context.Context, serverURL string) ([]oauthex.Challenge, error) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"handful","version":"1"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", serverURL, err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		return nil, nil
	}
	return oauthex.ParseWWWAuthenticate(res.Header.Values("WWW-Authenticate"))
}
