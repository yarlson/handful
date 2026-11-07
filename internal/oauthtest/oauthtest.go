// Package oauthtest is a fake MCP server with its own OAuth 2.1 authorization
// server (protected resource metadata, DCR, PKCE S256, refresh) for tests.
package oauthtest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Server issues access-1 and refresh-1 at sign-in, then access-N and
// refresh-N for the N-th token. Each token expires after ExpiresIn seconds.
// With IssSupported, its metadata says that redirects carry iss (RFC 9207),
// but Authorize leaves iss out. With TokenEndpoint, its metadata names that
// URL as the token endpoint instead of its own.
type Server struct {
	*httptest.Server
	ExpiresIn     int
	IssSupported  bool
	TokenEndpoint string

	mu            sync.Mutex
	registrations int
	redirectURI   string
	challenge     string
	issued        int
	refreshes     int
}

const ClientID = "client-1"

// Preregister makes ClientID a client registered out of band with
// redirectURI, as for a server without dynamic client registration.
func (s *Server) Preregister(redirectURI string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redirectURI = redirectURI
}

// MCPURL is the URL of the protected MCP endpoint.
func (s *Server) MCPURL() string { return s.URL + "/mcp" }

func (s *Server) Registrations() int { s.mu.Lock(); defer s.mu.Unlock(); return s.registrations }
func (s *Server) Refreshes() int     { s.mu.Lock(); defer s.mu.Unlock(); return s.refreshes }

func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{ExpiresIn: 3600}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp", scope="mcp:read"`, s.URL))
		http.Error(w, "sign in", http.StatusUnauthorized)
	})
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"resource": s.MCPURL(), "authorization_servers": []string{s.URL}, "scopes_supported": []string{"mcp:read"}})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		tokenEndpoint := s.URL + "/token"
		if s.TokenEndpoint != "" {
			tokenEndpoint = s.TokenEndpoint
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":                           s.URL,
			"authorization_endpoint":           s.URL + "/authorize",
			"token_endpoint":                   tokenEndpoint,
			"registration_endpoint":            s.URL + "/register",
			"code_challenge_methods_supported": []string{"S256"},
			"scopes_supported":                 []string{"mcp:read", "offline_access"},
			"authorization_response_iss_parameter_supported": s.IssSupported,
		})
	})
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			RedirectURIs []string `json:"redirect_uris"`
			AuthMethod   string   `json:"token_endpoint_auth_method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.RedirectURIs) != 1 || in.AuthMethod != "none" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
			return
		}
		s.mu.Lock()
		s.registrations++
		s.redirectURI = in.RedirectURIs[0]
		s.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{"client_id": ClientID, "redirect_uris": in.RedirectURIs, "token_endpoint_auth_method": "none"})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case q.Get("client_id") != ClientID, q.Get("redirect_uri") != s.redirectURI:
			http.Error(w, "unknown client or redirect_uri", http.StatusBadRequest)
		case q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "":
			http.Error(w, "S256 PKCE is required", http.StatusBadRequest)
		case q.Get("resource") != s.MCPURL():
			http.Error(w, "unsupported resource", http.StatusBadRequest)
		case !slices.Contains(strings.Fields(q.Get("scope")), "offline_access"):
			http.Error(w, "no offline_access, so no refresh token", http.StatusBadRequest)
		default:
			s.challenge = q.Get("code_challenge")
			http.Redirect(w, r, s.redirectURI+"?"+url.Values{"code": {"code-1"}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
		}
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		f := func(k string) string { return r.PostFormValue(k) }
		ok := false
		switch f("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(f("code_verifier")))
			ok = f("code") == "code-1" && f("client_id") == ClientID && f("redirect_uri") == s.redirectURI &&
				f("resource") == s.MCPURL() && base64.RawURLEncoding.EncodeToString(sum[:]) == s.challenge
		case "refresh_token":
			ok = f("client_id") == ClientID && f("refresh_token") == fmt.Sprintf("refresh-%d", s.issued)
			if ok {
				s.refreshes++
			}
		}
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		s.issued++
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  fmt.Sprintf("access-%d", s.issued),
			"refresh_token": fmt.Sprintf("refresh-%d", s.issued),
			"token_type":    "Bearer",
			"expires_in":    s.ExpiresIn,
		})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// Authorize plays the browser: it opens authURL, signs in and returns the
// URL that the authorization server redirects to.
func (s *Server) Authorize(t *testing.T, authURL string) *url.URL {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, authURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("authorize %s: status %d; want a redirect", authURL, res.StatusCode)
	}
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
