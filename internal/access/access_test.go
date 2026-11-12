package access

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	team = "you.cloudflareaccess.com"
	aud  = "abcdef0123456789"
)

var (
	keysOnce sync.Once
	keyA     *rsa.PrivateKey
	keyB     *rsa.PrivateKey
)

// testKeys returns two RSA keys, made once for the package.
func testKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keysOnce.Do(func() {
		keyA, _ = rsa.GenerateKey(rand.Reader, 2048)
		keyB, _ = rsa.GenerateKey(rand.Reader, 2048)
	})
	return keyA, keyB
}

// certs is a fake certs endpoint: it serves the public keys it holds, by kid, counts the
// requests, and fails while down is set.
type certs struct {
	mu    sync.Mutex
	keys  map[string]*rsa.PublicKey
	hits  atomic.Int32
	down  atomic.Bool
	extra []map[string]string
}

func (c *certs) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	c.hits.Add(1)
	if c.down.Load() {
		http.Error(w, "down", http.StatusBadGateway)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := append([]map[string]string{}, c.extra...)
	for kid, k := range c.keys {
		keys = append(keys, map[string]string{
			"kid": kid, "kty": "RSA", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys, "public_cert": map[string]string{"kid": "x", "cert": "y"}})
}

type fixture struct {
	v     *Verifier
	certs *certs
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	a, _ := testKeys(t)
	c := &certs{keys: map[string]*rsa.PublicKey{"ka": &a.PublicKey}}
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	f := &fixture{certs: c, now: time.Unix(1_800_000_000, 0)}
	f.v = New(team, aud, WithCertsURL(srv.URL), WithClock(func() time.Time { return f.now }))
	return f
}

// sign makes a token with header fields head and claims, signed by key.
func sign(t *testing.T, key *rsa.PrivateKey, head, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signed := enc(head) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fixture) claims() map[string]any {
	return map[string]any{"iss": "https://" + team, "aud": []string{aud}, "exp": f.now.Add(time.Hour).Unix(), "iat": f.now.Unix(), "nbf": f.now.Unix(), "email": "you@example.com"}
}

func head(kid string) map[string]any { return map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"} }

func TestAValidTokenNamesWhoSignedIn(t *testing.T) {
	f := newFixture(t)
	a, _ := testKeys(t)
	id, err := f.v.Verify(context.Background(), sign(t, a, head("ka"), f.claims()))
	if err != nil || id.Email != "you@example.com" {
		t.Fatalf("Verify = %+v, %v; want you@example.com", id, err)
	}
	// aud may be one string; the keys are fetched once.
	c := f.claims()
	c["aud"] = aud
	if _, err := f.v.Verify(context.Background(), sign(t, a, head("ka"), c)); err != nil {
		t.Fatalf("aud as a string: %v", err)
	}
	if hits := f.certs.hits.Load(); hits != 1 {
		t.Fatalf("certs fetched %d times; want 1", hits)
	}
}

func TestATokenThatProvesNothingIsRefused(t *testing.T) {
	a, b := testKeys(t)
	f := newFixture(t)
	if _, err := f.v.Verify(context.Background(), sign(t, a, head("ka"), f.claims())); err != nil {
		t.Fatal(err)
	}
	with := func(k string, v any) map[string]any {
		c := f.claims()
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
		return c
	}
	good := sign(t, a, head("ka"), f.claims())
	cases := map[string]string{
		"missing":          "",
		"not a token":      "abc",
		"forged":           "forged-jwt-test",
		"too long":         string(make([]byte, maxTokenBytes+1)),
		"other key":        sign(t, b, head("ka"), f.claims()),
		"wrong audience":   sign(t, a, head("ka"), with("aud", []string{"0000000000000000"})),
		"no audience":      sign(t, a, head("ka"), with("aud", nil)),
		"wrong issuer":     sign(t, a, head("ka"), with("iss", "https://other.cloudflareaccess.com")),
		"expired":          sign(t, a, head("ka"), with("exp", f.now.Unix())),
		"no expiry":        sign(t, a, head("ka"), with("exp", nil)),
		"not yet valid":    sign(t, a, head("ka"), with("nbf", f.now.Add(time.Minute).Unix())),
		"HS256":            sign(t, a, map[string]any{"alg": "HS256", "kid": "ka"}, f.claims()),
		"none":             sign(t, a, map[string]any{"alg": "none", "kid": "ka"}, f.claims()),
		"no key id":        sign(t, a, map[string]any{"alg": "RS256"}, f.claims()),
		"bad signature":    good[:len(good)-4] + "AAAA",
		"bad base64":       good + "!",
		"claims not JSON":  good[:len(good)/3] + "." + base64.RawURLEncoding.EncodeToString([]byte("{")) + "." + "AA",
		"bad header":       "!!!." + good[len(good)/3:],
		"header not JSON":  base64.RawURLEncoding.EncodeToString([]byte("[")) + ".e30.AA",
		"aud not a string": sign(t, a, head("ka"), with("aud", 7)),
	}
	for name, token := range cases {
		if _, err := f.v.Verify(context.Background(), token); !errors.Is(err, ErrToken) {
			t.Errorf("%s: err = %v; want ErrToken", name, err)
		}
	}
}

func TestAnUnknownKeyIDFetchesTheKeysOnceThenWaits(t *testing.T) {
	a, b := testKeys(t)
	f := newFixture(t)
	if _, err := f.v.Verify(context.Background(), sign(t, a, head("ka"), f.claims())); err != nil {
		t.Fatal(err)
	}
	// Cloudflare rotates its keys: kb is new.
	f.certs.mu.Lock()
	f.certs.keys["kb"] = &b.PublicKey
	f.certs.mu.Unlock()
	f.now = f.now.Add(10 * time.Second)
	token := sign(t, b, head("kb"), f.claims())
	if _, err := f.v.Verify(context.Background(), token); err != nil {
		t.Fatalf("a new key after the gap: %v", err)
	}
	if hits := f.certs.hits.Load(); hits != 2 {
		t.Fatalf("certs fetched %d times; want 2", hits)
	}
	// Made-up key IDs fetch once, then nothing within the gap.
	for range 5 {
		if _, err := f.v.Verify(context.Background(), sign(t, a, head("made-up"), f.claims())); !errors.Is(err, ErrToken) {
			t.Fatalf("an unknown key: %v; want ErrToken", err)
		}
	}
	if hits := f.certs.hits.Load(); hits != 2 {
		t.Fatalf("certs fetched %d times after unknown keys within the gap; want 2", hits)
	}
	f.now = f.now.Add(10*time.Second - time.Millisecond)
	if _, err := f.v.Verify(context.Background(), sign(t, a, head("made-up"), f.claims())); !errors.Is(err, ErrToken) || f.certs.hits.Load() != 2 {
		t.Fatalf("an unknown key just within the gap: %v, %d fetches", err, f.certs.hits.Load())
	}
	f.now = f.now.Add(time.Millisecond)
	if _, err := f.v.Verify(context.Background(), sign(t, a, head("made-up"), f.claims())); !errors.Is(err, ErrToken) || f.certs.hits.Load() != 3 {
		t.Fatalf("an unknown key after the gap: %v, %d fetches; want ErrToken and a third fetch", err, f.certs.hits.Load())
	}
}

func TestUnreachableKeysFailClosedAndCachedKeysKeepWorking(t *testing.T) {
	a, _ := testKeys(t)
	f := newFixture(t)
	f.certs.down.Store(true)
	token := sign(t, a, head("ka"), f.claims())
	if _, err := f.v.Verify(context.Background(), token); !errors.Is(err, ErrKeys) {
		t.Fatalf("keys down, none cached: %v; want ErrKeys", err)
	}
	// Within the gap no new fetch, and still no keys.
	if _, err := f.v.Verify(context.Background(), token); !errors.Is(err, ErrKeys) || f.certs.hits.Load() != 1 {
		t.Fatalf("again within the gap: %v, %d fetches; want ErrKeys and 1 fetch", err, f.certs.hits.Load())
	}
	f.certs.down.Store(false)
	f.now = f.now.Add(10 * time.Second)
	token = sign(t, a, head("ka"), f.claims())
	if _, err := f.v.Verify(context.Background(), token); err != nil {
		t.Fatalf("keys back: %v", err)
	}
	f.certs.down.Store(true)
	f.now = f.now.Add(time.Hour - time.Second)
	if _, err := f.v.Verify(context.Background(), token); err != nil {
		t.Fatalf("keys down again, key cached: %v; want it to keep working", err)
	}
	f.now = f.now.Add(10 * time.Second)
	if _, err := f.v.Verify(context.Background(), sign(t, a, head("kz"), f.claims())); !errors.Is(err, ErrKeys) {
		t.Fatalf("an unknown key while the endpoint is down: %v; want ErrKeys", err)
	}
}

func TestKeysThatArentRSAAreSkipped(t *testing.T) {
	a, _ := testKeys(t)
	f := newFixture(t)
	f.certs.extra = []map[string]string{{"kid": "ec", "kty": "EC"}, {"kid": "", "kty": "RSA", "n": "AQAB", "e": "AQAB"}, {"kid": "bad", "kty": "RSA", "n": "!", "e": "AQAB"}, {"kid": "e5", "kty": "RSA", "n": "AQAB", "e": "AQABAQAB"}}
	if _, err := f.v.Verify(context.Background(), sign(t, a, head("ka"), f.claims())); err != nil {
		t.Fatalf("the RSA key among others: %v", err)
	}
	if len(f.v.keys) != 1 {
		t.Fatalf("keys = %v; want only ka", f.v.keys)
	}
	empty := newFixture(t)
	empty.certs.keys = nil
	if _, err := empty.v.Verify(context.Background(), sign(t, a, head("ka"), empty.claims())); !errors.Is(err, ErrKeys) {
		t.Fatalf("an endpoint with no RSA keys: %v; want ErrKeys", err)
	}
}

func TestTheKeysComeFromTheTeamDomain(t *testing.T) {
	v := New(team, aud)
	if v.certsURL != "https://you.cloudflareaccess.com/cdn-cgi/access/certs" || v.issuer != "https://you.cloudflareaccess.com" || v.client.Timeout != 5*time.Second {
		t.Fatalf("verifier = %q, %q, %v", v.certsURL, v.issuer, v.client.Timeout)
	}
}

func TestATokenOfTheLargestSizeIsRead(t *testing.T) {
	a, _ := testKeys(t)
	f := newFixture(t)
	// Padding the header and the claims by one character each moves the length in steps of one
	// or two, so together they reach the limit exactly.
	sized := func(hp, cp int) string {
		h := head("ka")
		h["x"] = strings.Repeat("x", hp)
		c := f.claims()
		c["email"] = strings.Repeat("x", cp)
		return sign(t, a, h, c)
	}
	base := len(sized(0, 0))
	var token string
	for hp := 0; hp < 4 && token == ""; hp++ {
		for cp := (maxTokenBytes-base)*3/4 - 8; cp < (maxTokenBytes-base)*3/4+8; cp++ {
			if tok := sized(hp, cp); len(tok) == maxTokenBytes {
				token = tok
				break
			}
		}
	}
	if token == "" {
		t.Fatalf("no token of exactly %d bytes", maxTokenBytes)
	}
	if _, err := f.v.Verify(context.Background(), token); err != nil {
		t.Fatalf("a valid token of %d bytes: %v", len(token), err)
	}
}

func TestAKeyWhoseExponentHasFourBytesIsRead(t *testing.T) {
	a, _ := testKeys(t)
	f := newFixture(t)
	f.certs.keys = nil
	n := base64.RawURLEncoding.EncodeToString(a.N.Bytes())
	f.certs.extra = []map[string]string{
		{"kid": "ka", "kty": "RSA", "n": n, "e": base64.RawURLEncoding.EncodeToString([]byte{0, 1, 0, 1})},
		{"kid": "k5", "kty": "RSA", "n": n, "e": base64.RawURLEncoding.EncodeToString([]byte{0, 0, 1, 0, 1})},
	}
	if _, err := f.v.Verify(context.Background(), sign(t, a, head("ka"), f.claims())); err != nil {
		t.Fatalf("an exponent of 4 bytes: %v", err)
	}
	if _, ok := f.v.keys["k5"]; ok {
		t.Fatal("an exponent of 5 bytes was read")
	}
}
