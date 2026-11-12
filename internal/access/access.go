// Package access checks the Cloudflare Access token that a Cloudflare Tunnel sends with every
// request that passed its login: the Cf-Access-Jwt-Assertion header, an RS256 JSON Web Token
// signed by the Access team's keys.
package access

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Header is the request header that carries the token.
const Header = "Cf-Access-Jwt-Assertion"

const (
	maxTokenBytes = 16 << 10
	maxCertsBytes = 64 << 10
	fetchSeconds  = 5 // how long a fetch of the keys may take
	// refreshSeconds is the least time between two fetches of the keys, so tokens with made-up
	// key IDs can't make the server fetch them on every request.
	refreshSeconds = 10
)

// ErrToken is a token that proves nothing: missing, malformed, wrongly signed, for another
// application or issuer, or expired. ErrKeys is a check that could not run, because the keys
// could not be fetched. Both mean the request is refused.
var (
	ErrToken = errors.New("no valid Cloudflare Access token")
	ErrKeys  = errors.New("the Cloudflare Access keys can't be fetched")
)

// Identity is what a valid token says about who signed in.
type Identity struct {
	Email string
}

// Verifier checks tokens for one Access application. It fetches the team's keys when it
// first needs them, keeps them, and fetches them again, at most every refreshSeconds, when a token
// names a key it doesn't have.
type Verifier struct {
	issuer   string // https://<team domain>
	aud      string
	certsURL string
	client   *http.Client
	now      func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

// Option changes a Verifier that New returns.
type Option func(*Verifier)

// WithCertsURL fetches the keys from url, for a test's own certs endpoint.
func WithCertsURL(url string) Option { return func(v *Verifier) { v.certsURL = url } }

// WithClock reads the time from now.
func WithClock(now func() time.Time) Option { return func(v *Verifier) { v.now = now } }

// New returns the Verifier for the Access application aud of team domain teamDomain (a host
// name, such as you.cloudflareaccess.com).
func New(teamDomain, aud string, opts ...Option) *Verifier {
	issuer := "https://" + teamDomain
	v := &Verifier{issuer: issuer, aud: aud, certsURL: issuer + "/cdn-cgi/access/certs", client: &http.Client{Timeout: fetchSeconds * time.Second}, now: time.Now}
	for _, o := range opts {
		o(v)
	}
	return v
}

// Verify checks token and returns who signed in. The error is ErrToken or ErrKeys.
func (v *Verifier) Verify(ctx context.Context, token string) (Identity, error) {
	parts, kid, err := split(token)
	if err != nil {
		return Identity{}, err
	}
	key, err := v.key(ctx, kid)
	if err != nil {
		return Identity{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, ErrToken
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) != nil {
		return Identity{}, ErrToken
	}
	return v.identity(parts[1])
}

// split returns a token's three parts and its key ID, for an RS256 token.
func split(token string) ([]string, string, error) {
	if token == "" || len(token) > maxTokenBytes {
		return nil, "", ErrToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, "", ErrToken
	}
	var head struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if decodePart(parts[0], &head) != nil || head.Alg != "RS256" || head.Kid == "" {
		return nil, "", ErrToken
	}
	return parts, head.Kid, nil
}

// identity checks a signed token's claims: for this application, from its team, and in time.
func (v *Verifier) identity(payload string) (Identity, error) {
	var claims struct {
		Iss   string   `json:"iss"`
		Aud   audience `json:"aud"`
		Exp   float64  `json:"exp"`
		Nbf   float64  `json:"nbf"`
		Email string   `json:"email"`
	}
	if decodePart(payload, &claims) != nil {
		return Identity{}, ErrToken
	}
	now := float64(v.now().Unix())
	if claims.Iss != v.issuer || !claims.Aud.has(v.aud) || claims.Exp == 0 || now >= claims.Exp || now < claims.Nbf {
		return Identity{}, ErrToken
	}
	return Identity{Email: claims.Email}, nil
}

func decodePart(part string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// audience is a token's aud claim: one string or a list of them.
type audience []string

func (a *audience) UnmarshalJSON(raw []byte) error {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a audience) has(aud string) bool { return slices.Contains(a, aud) }

// key returns the public key kid, fetching the keys when it is new and the last fetch is older
// than refreshSeconds. Keys already fetched keep working while a fetch fails.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	if !v.fetched.IsZero() && v.now().Sub(v.fetched) < refreshSeconds*time.Second {
		return nil, v.missing()
	}
	v.fetched = v.now()
	keys, err := v.fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKeys, err)
	}
	v.keys = keys
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, ErrToken
}

// missing is the error for a key that isn't known and can't be fetched again yet.
func (v *Verifier) missing() error {
	if v.keys == nil {
		return ErrKeys
	}
	return ErrToken
}

// fetch reads the team's RSA keys from its certs endpoint.
func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchSeconds*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", v.certsURL, resp.Status)
	}
	var body struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCertsBytes)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode %s: %w", v.certsURL, err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range body.Keys {
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if k.Kty != "RSA" || k.Kid == "" || errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s has no RSA keys", v.certsURL)
	}
	return keys, nil
}
