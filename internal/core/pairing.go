package core

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Pairing a phone takes a one-time code, shown on the computer and typed or scanned on the phone,
// and then the user's approval on the computer. Codes, requests and the lockout live in memory:
// a restart voids them, and the phone asks for a new code.
const (
	pairCodeLength = 12
	// pairCodeSymbols are 32 symbols that can't be misread for one another (no I, L, O or U), so
	// a code of 12 holds 60 bits.
	pairCodeSymbols = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	// PairCodeMinutes is how long a code works; PairRequestMinutes is how long a pair request
	// waits for the user's decision, and how long an approved phone has to collect its session.
	PairCodeMinutes    = 5
	PairRequestMinutes = 2
	// After pairFailLimit wrong codes in pairLockMinutes, pairing pauses for pairLockMinutes.
	pairFailLimit   = 5
	pairLockMinutes = 10
)

// The states of a pair request, as the phone sees them.
const (
	PairWaiting  = "waiting"  // the computer hasn't decided
	PairApproved = "approved" // the computer allowed it; the answer carries the session
	PairDenied   = "denied"   // the computer denied it, or it waited too long
)

// ErrTooMany is a request that comes too often; the caller waits and tries again.
var ErrTooMany = errors.New("too many requests")

// tooMany is an ErrTooMany whose text is a sentence for the user.
type tooMany string

func (e tooMany) Error() string        { return string(e) }
func (e tooMany) Is(target error) bool { return target == ErrTooMany }

// errPairingLocked is the answer while pairing is paused after too many wrong codes.
const errPairingLocked = tooMany("5 wrong pairing codes were tried. Pairing is paused for 10 minutes.")

// errBadCode is a code that doesn't work: wrong, used or expired. The text doesn't say which.
const errBadCode = badCode("This code doesn't work. Ask for a new code on your computer.")

// badCode is an ErrInvalid whose text is a sentence for the user.
type badCode string

func (e badCode) Error() string        { return string(e) }
func (e badCode) Is(target error) bool { return target == ErrInvalid }

// PairRequest is a phone that typed a working code and waits for the user's decision.
type PairRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	PairedBy  string `json:"pairedBy,omitempty"`
	ExpiresAt int64  `json:"expiresAt"` // ms
}

// Pairing is what the computer shows of pairing: the open code's end and scope, the request
// that waits for a decision, and the end of a pause after too many wrong codes.
type Pairing struct {
	CodeExpiresAt int64        `json:"codeExpiresAt,omitempty"` // ms; 0 without an open code
	Scope         string       `json:"scope,omitempty"`         // the open code's scope
	Request       *PairRequest `json:"request,omitempty"`
	LockedUntil   int64        `json:"lockedUntil,omitempty"` // ms
}

// pairing holds the one open code, the latest pair request and the wrong codes of the last
// pairLockMinutes.
type pairing struct {
	mu          sync.Mutex
	code        []byte // the open code's hash; nil when there is none
	scope       string
	codeExpires time.Time
	fails       []time.Time
	locked      time.Time // pairing is paused until then
	last        time.Time // the latest pair request, for the cap of one a second
	request     *pairRequest
}

// pairRequest is the latest pair request, with the hash of the claim that the phone polls with.
type pairRequest struct {
	PairRequest
	claim   []byte
	state   string
	decided time.Time
}

// normalPairCode is a typed code without case, spaces or dashes.
func normalPairCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(code))
}

// StartPairing makes a new one-time code for a phone with scope, and voids the previous one.
func (s *Service) StartPairing(scope string) (string, Pairing, error) {
	if !validScope(scope) {
		return "", Pairing{}, invalid("a phone's scope must be view, chat or chat+approve")
	}
	p := s.pairing
	p.mu.Lock()
	now := s.now()
	if now.Before(p.locked) {
		p.mu.Unlock()
		return "", Pairing{}, errPairingLocked
	}
	raw := make([]byte, pairCodeLength)
	if _, err := rand.Read(raw); err != nil {
		p.mu.Unlock()
		return "", Pairing{}, fmt.Errorf("make a pairing code: %w", err)
	}
	code := make([]byte, pairCodeLength)
	for i, b := range raw {
		code[i] = pairCodeSymbols[int(b)%len(pairCodeSymbols)]
	}
	p.code, p.scope, p.codeExpires = phoneTokenHash(string(code)), scope, now.Add(PairCodeMinutes*time.Minute)
	view := p.view(now)
	p.mu.Unlock()
	s.hub.Publish(Event{Type: "pair"})
	return string(code), view, nil
}

// EndPairing voids the open code and the waiting pair request, when phone access turns off. A
// pause after wrong codes stays.
func (s *Service) EndPairing() {
	p := s.pairing
	p.mu.Lock()
	p.code, p.request = nil, nil
	p.mu.Unlock()
	s.hub.Publish(Event{Type: "pair"})
}

// SetPairingScope changes what the phone that uses the open code may do.
func (s *Service) SetPairingScope(scope string) (Pairing, error) {
	if !validScope(scope) {
		return Pairing{}, invalid("a phone's scope must be view, chat or chat+approve")
	}
	p := s.pairing
	p.mu.Lock()
	now := s.now()
	if p.code == nil || !now.Before(p.codeExpires) {
		p.mu.Unlock()
		return Pairing{}, notFound("pairing code", "")
	}
	p.scope = scope
	view := p.view(now)
	p.mu.Unlock()
	s.hub.Publish(Event{Type: "pair"})
	return view, nil
}

// Pairing returns what the computer shows of pairing now.
func (s *Service) Pairing() Pairing {
	p := s.pairing
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.view(s.now())
}

func (p *pairing) view(now time.Time) Pairing {
	var v Pairing
	if p.code != nil && now.Before(p.codeExpires) {
		v.CodeExpiresAt, v.Scope = p.codeExpires.UnixMilli(), p.scope
	}
	if r := p.request; r != nil && r.state == PairWaiting && now.UnixMilli() < r.ExpiresAt {
		req := r.PairRequest
		v.Request = &req
	}
	if now.Before(p.locked) {
		v.LockedUntil = p.locked.UnixMilli()
	}
	return v
}

// RequestPairing takes a code typed on a phone named name. A working code is used up, whatever
// the user decides, and starts a pair request that waits for the user; the answer is the claim
// the phone asks PairingStatus with. Wrong codes count toward the pause of pairing.
func (s *Service) RequestPairing(code, name, pairedBy string) (string, error) {
	name, err := phoneName(name)
	if err != nil {
		return "", err
	}
	p := s.pairing
	p.mu.Lock()
	now := s.now()
	if now.Before(p.locked) {
		p.mu.Unlock()
		return "", errPairingLocked
	}
	if now.Sub(p.last) < time.Second {
		p.mu.Unlock()
		return "", tooMany("Wait a second, then try again.")
	}
	p.last = now
	if p.code == nil || !now.Before(p.codeExpires) || subtle.ConstantTimeCompare(p.code, phoneTokenHash(normalPairCode(code))) != 1 {
		locked := p.fail(now)
		p.mu.Unlock()
		if locked {
			s.hub.Publish(Event{Type: "pair"})
			return "", errPairingLocked
		}
		return "", errBadCode
	}
	raw := make([]byte, phoneTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		p.mu.Unlock()
		return "", fmt.Errorf("make a pairing claim: %w", err)
	}
	claim := base64.RawURLEncoding.EncodeToString(raw)
	p.request = &pairRequest{
		ID: uuid.NewString(), Name: name, Scope: p.scope, PairedBy: pairedBy, ExpiresAt: now.Add(PairRequestMinutes * time.Minute).UnixMilli(),
		claim: phoneTokenHash(claim),
		state: PairWaiting,
	}
	p.code = nil
	p.mu.Unlock()
	s.hub.Publish(Event{Type: "pair"})
	return claim, nil
}

// fail counts a wrong code at now, and pauses pairing, voiding the open code, once
// pairFailLimit came within pairLockMinutes. It reports whether pairing is paused.
func (p *pairing) fail(now time.Time) bool {
	window := now.Add(-pairLockMinutes * time.Minute)
	kept := p.fails[:0]
	for _, t := range p.fails {
		if t.After(window) {
			kept = append(kept, t)
		}
	}
	p.fails = append(kept, now)
	if len(p.fails) < pairFailLimit {
		return false
	}
	p.fails, p.code, p.locked = nil, nil, now.Add(pairLockMinutes*time.Minute)
	return true
}

// DecidePairing allows or denies pair request id, while it waits.
func (s *Service) DecidePairing(id string, allow bool) error {
	p := s.pairing
	p.mu.Lock()
	now := s.now()
	r := p.request
	if r == nil || r.ID != id || r.state != PairWaiting || now.UnixMilli() >= r.ExpiresAt {
		p.mu.Unlock()
		return notFound("pair request", id)
	}
	r.state, r.decided = PairDenied, now
	if allow {
		r.state = PairApproved
	}
	p.mu.Unlock()
	s.hub.Publish(Event{Type: "pair"})
	return nil
}

// PairingStatus answers the phone that holds claim: whether its request waits, was denied or
// waited too long, or was approved. An approved request returns the new session's token once,
// within PairRequestMinutes of the approval; the session is made then.
func (s *Service) PairingStatus(ctx context.Context, claim string) (string, string, error) {
	p := s.pairing
	p.mu.Lock()
	defer p.mu.Unlock()
	now := s.now()
	r := p.request
	if r == nil || subtle.ConstantTimeCompare(r.claim, phoneTokenHash(claim)) != 1 {
		return "", "", notFound("pair request", "")
	}
	if r.state == PairWaiting && now.UnixMilli() < r.ExpiresAt {
		return PairWaiting, "", nil
	}
	if r.state != PairApproved || now.Sub(r.decided) >= PairRequestMinutes*time.Minute {
		return PairDenied, "", nil
	}
	token, _, err := s.CreatePhoneSession(ctx, r.Name, r.Scope, r.PairedBy)
	if err != nil {
		return "", "", err
	}
	p.request = nil // a claim issues one session
	return PairApproved, token, nil
}

func validScope(scope string) bool {
	return scope == ScopeView || scope == ScopeChat || scope == ScopeApprove
}
