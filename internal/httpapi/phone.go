package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"handful/internal/access"
	"handful/internal/config"
	"handful/internal/core"
)

// The phone's session cookie. The __Host- prefix makes the browser refuse it unless it is
// Secure, set by this exact host, with Path=/ and no Domain.
const phoneCookie = "__Host-handful_phone"

// phoneRoutes are the routes a paired phone may use, with the scope each needs (spec 6a748f8a
// section 6.2). Every other route of the API is closed to a phone.
var phoneRoutes = []struct{ pattern, scope string }{
	{"GET /api/session", core.ScopeView},
	{"GET /api/bots", core.ScopeView},
	{"GET /api/bots/{id}", core.ScopeView},
	{"GET /api/bots/{id}/entries", core.ScopeView},
	{"GET /api/media/{name}", core.ScopeView},
	{"GET /api/teams/{id}/board", core.ScopeView},
	{"GET /api/tasks/{id}/events", core.ScopeView},
	{"GET /api/items/{id}/events", core.ScopeView},
	{"GET /api/needs-you", core.ScopeView},
	{"GET /api/questions", core.ScopeView},
	{"GET /api/bots/{id}/questions", core.ScopeView},
	{"GET /api/artifacts", core.ScopeView},
	{"GET /api/artifacts/{id}", core.ScopeView},
	{"GET /api/skills", core.ScopeView},
	{"GET /api/events", core.ScopeView},
	{"GET /api/version", core.ScopeView},
	{"GET /api/defaults", core.ScopeView},
	{"GET /api/teams/{id}/off-task", core.ScopeView},
	{"POST /api/bots/{id}/messages", core.ScopeChat},
	{"POST /api/bots/{id}/interrupt", core.ScopeChat},
	{"POST /api/entries/{id}/reactions", core.ScopeChat},
	{"POST /api/entries/{id}/dismiss", core.ScopeChat},
	{"POST /api/entries/{id}/reopen", core.ScopeChat},
	{"POST /api/tasks/{id}/answer", core.ScopeChat},
	{"POST /api/tasks/{id}/approve", core.ScopeChat},
	{"POST /api/tasks/{id}/changes", core.ScopeChat},
	{"POST /api/tasks/{id}/dismiss", core.ScopeChat},
	{"POST /api/teams/{id}/off-task/dismiss", core.ScopeChat},
	{"POST /api/teams/{id}/budget/wait", core.ScopeChat},
	{"POST /api/approvals/{id}", core.ScopeApprove},
}

// sendPattern is the route whose requests have their own, lower rate limit.
const sendPattern = "POST /api/bots/{id}/messages"

// Phone is the second listener, for paired phones. It opens only when phone access is on, on
// a loopback address that a proxy on this computer forwards to, and every request to it needs
// a session.
type Phone struct {
	settings   config.PhoneSettings
	verifier   *access.Verifier // nil unless the proxy is a Cloudflare Tunnel
	accessOpts []access.Option
	now        func() time.Time
	recheck    time.Duration // how often an open event stream checks its session
	log        *slog.Logger
	limits     *phoneLimits

	mu      sync.Mutex
	handler http.Handler
	srv     *http.Server
	addr    string        // where it listens; "" while it doesn't
	closed  chan struct{} // closed by Stop
	served  chan struct{} // closed when the server stops serving
}

// PhoneOption changes the Phone that NewPhone returns.
type PhoneOption func(*Phone)

// WithAccessOptions builds the Access token check with opts, for example a test's own keys.
func WithAccessOptions(opts ...access.Option) PhoneOption {
	return func(p *Phone) { p.accessOpts = opts }
}

// WithPhoneClock reads the time from now, and has open streams check their session every
// recheck.
func WithPhoneClock(now func() time.Time, recheck time.Duration) PhoneOption {
	return func(p *Phone) { p.now, p.recheck = now, recheck }
}

// NewPhone returns the phone listener of settings, which must pass settings.Check. It opens
// when Start runs, after New has given it its handler.
func NewPhone(settings config.PhoneSettings, log *slog.Logger, opts ...PhoneOption) *Phone {
	p := &Phone{settings: settings, now: time.Now, recheck: 15 * time.Second, log: log}
	for _, o := range opts {
		o(p)
	}
	if settings.Proxy == config.ProxyCloudflare {
		p.verifier = access.New(settings.TeamDomain, settings.AUD, p.accessOpts...)
	}
	p.limits = newPhoneLimits(p.now)
	return p
}

// WithPhone serves p's listener with the API's phone routes, and offers the main listener's
// controls of phone access.
func WithPhone(p *Phone) Option { return func(s *server) { s.phone = p } }

// Start opens the phone listener and serves it until ctx ends or Stop runs.
func (p *Phone) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handler == nil {
		return errors.New("the phone listener has no handler; pass WithPhone to httpapi.New first")
	}
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", p.settings.Addr)
	if err != nil {
		return fmt.Errorf("listen for phones on %s (phone.addr): %w", p.settings.Addr, err)
	}
	p.srv = &http.Server{Handler: p.handler, ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	p.addr = ln.Addr().String()
	p.closed, p.served = make(chan struct{}), make(chan struct{})
	srv, closed, served := p.srv, p.closed, p.served
	go func() {
		defer close(served)
		// Serve always returns an error; ErrServerClosed is Stop's.
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			p.log.Error("phone listener stopped", "err", err)
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			p.Stop()
		case <-closed:
		}
	}()
	p.log.Info("phone access is on", "addr", p.addr, "hosts", strings.Join(p.settings.Hosts, ","), "proxy", p.settings.Proxy)
	return nil
}

// Addr is where the listener listens, or "" while it is closed.
func (p *Phone) Addr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addr
}

// Stop closes the listener and every open connection, event streams too, and returns once it
// has stopped serving. Paired phones stay paired. Stopping it again does nothing.
func (p *Phone) Stop() {
	p.mu.Lock()
	if p.srv == nil {
		p.mu.Unlock()
		return
	}
	if err := p.srv.Close(); err != nil {
		p.log.Error("close the phone listener", "err", err)
	}
	served := p.served
	p.srv, p.addr = nil, ""
	close(p.closed)
	p.mu.Unlock()
	<-served
	p.log.Info("phone access is off")
}

// phoneDevice is the paired phone a request on the phone listener comes from.
type phoneDeviceKey struct{}

func phoneDeviceOf(ctx context.Context) (core.PhoneSession, bool) {
	d, ok := ctx.Value(phoneDeviceKey{}).(core.PhoneSession)
	return d, ok
}

// phoneHandler is the phone listener's handler: the checks of every request, then the routes
// a phone may use. api serves them; ui serves the web app's files.
func (s *server) phoneHandler(api, ui http.Handler) http.Handler {
	p := s.phone
	routes := http.NewServeMux()
	for _, r := range phoneRoutes {
		routes.Handle(r.pattern, p.route(s, r.scope, r.pattern == sendPattern, api))
	}
	routes.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The bots' MCP endpoint and the sign-in callbacks are the computer's only.
		if r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/mcp/") || strings.HasPrefix(r.URL.Path, "/oauth/") {
			http.NotFound(w, r)
			return
		}
		ui.ServeHTTP(w, r)
	}))
	routes.Handle("/", http.NotFoundHandler())
	return p.gate(s.svc, routes)
}

// gate runs the checks that every request on the phone listener passes, in this order, before
// next: the Cloudflare Access token (tunnel mode), the Host, the Origin, a JSON body for a
// write, and a session for anything but the web app's files. Forwarded headers are never read.
func (p *Phone) gate(svc *core.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status, msg := p.refusal(r); status != 0 {
			http.Error(w, msg, status)
			return
		}
		if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		dev, err := p.session(r.Context(), svc, r)
		if err != nil {
			status, msg := p.noSession(err)
			http.Error(w, msg, status)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), phoneDeviceKey{}, dev)))
	})
}

// refusal returns the status and text that refuse r before its session is read, or 0: no
// valid Access token in tunnel mode, a Host not in phone.hosts, a foreign Origin, or a write
// to the API that isn't JSON.
func (p *Phone) refusal(r *http.Request) (int, string) {
	if p.verifier != nil {
		if _, err := p.verifier.Verify(r.Context(), r.Header.Get(access.Header)); err != nil {
			if errors.Is(err, access.ErrKeys) {
				p.log.Warn("phone request refused: the Access keys can't be fetched", "err", err)
			}
			return http.StatusForbidden, "This address needs a Cloudflare login."
		}
	}
	if !slices.Contains(p.settings.Hosts, strings.ToLower(hostOnly(r.Host))) {
		return http.StatusForbidden, "This address isn't one of phone.hosts."
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, r.Host) {
			return http.StatusForbidden, "cross-origin request refused"
		}
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
	if write && strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return http.StatusUnsupportedMediaType, "Content-Type must be application/json"
	}
	return 0, ""
}

// noSession returns the answer to a request whose session doesn't work: 401, or 429 once too
// many such requests came in a minute.
func (p *Phone) noSession(err error) (int, string) {
	if !errors.Is(err, core.ErrNotFound) {
		p.log.Error("check a phone session", "err", err)
		return http.StatusInternalServerError, "the session can't be checked"
	}
	if !p.limits.allowUnpaired() {
		return http.StatusTooManyRequests, "too many requests without a session"
	}
	return http.StatusUnauthorized, "Pair this phone again."
}

// session returns the active session of the request's cookie, or ErrNotFound.
func (p *Phone) session(ctx context.Context, svc *core.Service, r *http.Request) (core.PhoneSession, error) {
	c, err := r.Cookie(phoneCookie)
	if err != nil {
		return core.PhoneSession{}, core.ErrNotFound
	}
	return svc.PhoneSessionByToken(ctx, c.Value)
}

// phoneSessionCookie is the cookie that carries a phone's session token.
func phoneSessionCookie(token string) *http.Cookie {
	return &http.Cookie{Name: phoneCookie, Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: core.PhoneMaxDays * 24 * 60 * 60}
}

// route lets a request through to next when its phone's scope includes scope and its rate
// limits allow it. An event stream ends once its session no longer works.
func (p *Phone) route(s *server, scope string, send bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dev, _ := phoneDeviceOf(r.Context())
		if !dev.Allows(scope) {
			msg := "Your phone can't do this; change what it may do on the computer."
			if dev.Scope == core.ScopeView {
				msg = "Your phone can only view."
			}
			http.Error(w, msg, http.StatusForbidden)
			return
		}
		if !p.limits.allow(dev.ID, send) {
			http.Error(w, "too many requests from this phone", http.StatusTooManyRequests)
			return
		}
		if r.URL.Path == "/api/events" {
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			go p.watch(ctx, cancel, s.svc, dev.ID)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// watch ends a phone's event stream, through cancel, once its session no longer works. It
// checks every p.recheck until ctx ends.
func (p *Phone) watch(ctx context.Context, cancel context.CancelFunc, svc *core.Service, id string) {
	tick := time.NewTicker(p.recheck)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if ok, err := svc.PhoneSessionActive(ctx, id); !ok || err != nil {
				cancel()
				return
			}
		}
	}
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// session tells the UI what this browser's session is: on the computer's listener never a
// phone; on the phone listener the paired phone, its scope and its name.
func (s *server) session(w http.ResponseWriter, r *http.Request) {
	dev, ok := phoneDeviceOf(r.Context())
	if !ok {
		writeJSON(w, http.StatusOK, map[string]bool{"phone": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"phone": true, "scope": dev.Scope, "device": dev.Name})
}

// phoneOff is the kill switch: it closes the phone listener and its streams at once, and turns
// phone access off in the settings file, so it stays off. Paired phones stay paired.
func (s *server) phoneOff(w http.ResponseWriter, r *http.Request) {
	s.phone.Stop()
	if s.settings.Path != "" {
		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
		f, err := config.Load(s.settings.Path)
		if err == nil {
			err = f.Set("phone.enabled", []byte("false"))
		}
		if err == nil {
			err = config.Write(s.settings.Path, f)
		}
		if err != nil {
			s.fail(w, r, fmt.Errorf("phone access is off until the server restarts, but it can't be turned off in config.json: %w", err))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listPhoneSessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.PhoneSessions(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) revokePhoneSession(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.RevokePhoneSession(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) revokeAllPhoneSessions(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.RevokeAllPhoneSessions(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
