package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Handful's key: every write to the API on the main listener carries it, in KeyHeader from a
// program or in keyCookie from a browser. The key lives in the data folder's ui-key file; the
// phone listener has its own sessions and never asks for it.
const (
	KeyHeader = "X-Handful-Key"
	keyCookie = "handful_key"
	// keyCookieSeconds is 400 days, the longest a browser keeps a cookie.
	keyCookieSeconds = 34_560_000
	keyPath          = "/api/key"
)

// apiKey is the key the main listener checks, and the file that holds it, which the key page
// shows. An empty key refuses every write.
type apiKey struct {
	value []byte
	file  string
}

// WithKey makes key, kept in file, the key that writes to the API need. Without it, every
// write is refused.
func WithKey(key, file string) Option {
	return func(s *server) { s.key = apiKey{value: []byte(key), file: file} }
}

// matches reports whether got is the key, in constant time.
func (k apiKey) matches(got string) bool {
	return len(k.value) > 0 && subtle.ConstantTimeCompare(k.value, []byte(got)) == 1
}

// carried reports whether r carries the key, in the header or the cookie.
func (k apiKey) carried(r *http.Request) bool {
	if k.matches(r.Header.Get(KeyHeader)) {
		return true
	}
	c, err := r.Cookie(keyCookie)
	return err == nil && k.matches(c.Value)
}

// needsKey reports whether r is a write to the API, which needs the key. POST /api/key, which
// hands a browser the key's cookie, is the one write without it.
func needsKey(r *http.Request) bool {
	write := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
	return write && strings.HasPrefix(r.URL.Path, "/api/") && (r.Method != http.MethodPost || r.URL.Path != keyPath)
}

// saveKey checks a key that the user pasted on the key page and, when it is Handful's key, gives
// this browser the key's cookie.
func (s *server) saveKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
	}
	if err := decodeLimited(r, &in, 1<<10); err != nil {
		s.fail(w, r, err)
		return
	}
	if !s.key.matches(strings.TrimSpace(in.Key)) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "That isn't Handful's key. Copy the whole line from the file."})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: keyCookie, Value: string(s.key.value), Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: keyCookieSeconds})
	w.WriteHeader(http.StatusNoContent)
}
