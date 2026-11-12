package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"handful/internal/db"
)

// The scopes of a paired phone: what it may do.
const (
	ScopeView    = "view"         // read only
	ScopeChat    = "chat"         // read, send messages and act on tasks; no approval decisions
	ScopeApprove = "chat+approve" // chat, plus Allow once and Deny on approval cards
)

const (
	// PhoneIdleDays and PhoneMaxDays end a phone session: 30 days after its last request, and
	// 90 days after it was paired.
	PhoneIdleDays = 30
	PhoneMaxDays  = 90
	// phoneTouch is how often a session's last request is written, at most.
	phoneTouch       = time.Minute
	maxPhoneName     = 60
	phoneTokenBytes  = 32
	phoneTokenPrefix = "hp_"
)

// ErrNotAllowed is an action that the caller may not take, for example a decision a phone
// may not make.
var ErrNotAllowed = errors.New("not allowed")

// notAllowed is an ErrNotAllowed whose text is a sentence for the user.
type notAllowed string

func (e notAllowed) Error() string        { return string(e) }
func (e notAllowed) Is(target error) bool { return target == ErrNotAllowed }

// PhoneSession is a paired phone.
type PhoneSession struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	PairedBy   string `json:"pairedBy,omitempty"`
	CreatedAt  int64  `json:"createdAt"`  // ms
	LastSeenAt int64  `json:"lastSeenAt"` // ms
	RevokedAt  int64  `json:"revokedAt,omitempty"`
}

// Active reports whether the session still works at now: not revoked, used within
// PhoneIdleDays, and paired within PhoneMaxDays.
func (p PhoneSession) Active(now time.Time) bool {
	ms := now.UnixMilli()
	day := (24 * time.Hour).Milliseconds()
	return p.RevokedAt == 0 && ms < p.LastSeenAt+PhoneIdleDays*day && ms < p.CreatedAt+PhoneMaxDays*day
}

// Allows reports whether the session's scope includes scope.
func (p PhoneSession) Allows(scope string) bool {
	rank := map[string]int{ScopeView: 1, ScopeChat: 2, ScopeApprove: 3}
	return rank[p.Scope] >= rank[scope] && rank[scope] > 0
}

func toPhoneSession(r db.PhoneSession) PhoneSession {
	return PhoneSession{ID: r.ID, Name: r.Name, Scope: r.Scope, PairedBy: r.PairedBy, CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt, RevokedAt: r.RevokedAt}
}

// phoneName returns name without its outer spaces, or ErrInvalid for a name that is empty, too
// long or has control characters.
func phoneName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxPhoneName || strings.ContainsFunc(name, func(r rune) bool { return r < ' ' }) {
		return "", invalid("a phone's name must be 1 to %d characters, with no control characters", maxPhoneName)
	}
	return name, nil
}

// phoneTokenHash is what the database keeps of a token.
func phoneTokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// CreatePhoneSession pairs a phone named name with scope, and returns the session's token,
// which only the phone keeps.
func (s *Service) CreatePhoneSession(ctx context.Context, name, scope, pairedBy string) (string, PhoneSession, error) {
	name, err := phoneName(name)
	if err != nil {
		return "", PhoneSession{}, err
	}
	if !validScope(scope) {
		return "", PhoneSession{}, invalid("a phone's scope must be view, chat or chat+approve")
	}
	raw := make([]byte, phoneTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", PhoneSession{}, fmt.Errorf("make a phone token: %w", err)
	}
	token := phoneTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	now := s.nowMs()
	p := PhoneSession{ID: uuid.NewString(), Name: name, Scope: scope, PairedBy: pairedBy, CreatedAt: now, LastSeenAt: now}
	if err := s.q.InsertPhoneSession(ctx, db.InsertPhoneSessionParams{ID: p.ID, Name: name, Scope: scope, TokenHash: phoneTokenHash(token), PairedBy: pairedBy, CreatedAt: now, LastSeenAt: now}); err != nil {
		return "", PhoneSession{}, fmt.Errorf("store phone %q: %w", name, err)
	}
	return token, p, nil
}

// PhoneSessionByToken returns the active session of token and notes the request, or
// ErrNotFound for a token that is unknown, revoked or expired.
func (s *Service) PhoneSessionByToken(ctx context.Context, token string) (PhoneSession, error) {
	if !strings.HasPrefix(token, phoneTokenPrefix) {
		return PhoneSession{}, notFound("phone session", "")
	}
	row, err := s.q.GetPhoneSessionByHash(ctx, phoneTokenHash(token))
	if errors.Is(err, sql.ErrNoRows) {
		return PhoneSession{}, notFound("phone session", "")
	}
	if err != nil {
		return PhoneSession{}, fmt.Errorf("find a phone session: %w", err)
	}
	p := toPhoneSession(row)
	now := s.now()
	if !p.Active(now) {
		return PhoneSession{}, notFound("phone session", p.ID)
	}
	if now.UnixMilli()-p.LastSeenAt >= phoneTouch.Milliseconds() {
		if err := s.q.TouchPhoneSession(ctx, db.TouchPhoneSessionParams{ID: p.ID, At: now.UnixMilli()}); err != nil {
			return PhoneSession{}, fmt.Errorf("note the request of phone %s: %w", p.ID, err)
		}
		p.LastSeenAt = now.UnixMilli()
	}
	return p, nil
}

// PhoneSessionActive reports whether session id still works, without noting a request.
func (s *Service) PhoneSessionActive(ctx context.Context, id string) (bool, error) {
	row, err := s.q.GetPhoneSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get phone session %s: %w", id, err)
	}
	return toPhoneSession(row).Active(s.now()), nil
}

// PhoneSessions lists every paired phone, oldest first, revoked and expired ones too.
func (s *Service) PhoneSessions(ctx context.Context) ([]PhoneSession, error) {
	rows, err := s.q.ListPhoneSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list phone sessions: %w", err)
	}
	out := make([]PhoneSession, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPhoneSession(r))
	}
	return out, nil
}

// RevokePhoneSession ends phone id's session. Revoking it again changes nothing.
func (s *Service) RevokePhoneSession(ctx context.Context, id string) error {
	if _, err := s.q.GetPhoneSession(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return notFound("phone session", id)
	} else if err != nil {
		return fmt.Errorf("get phone session %s: %w", id, err)
	}
	if _, err := s.q.RevokePhoneSession(ctx, db.RevokePhoneSessionParams{ID: id, At: s.nowMs()}); err != nil {
		return fmt.Errorf("revoke phone session %s: %w", id, err)
	}
	return nil
}

// RevokeAllPhoneSessions ends every phone's session.
func (s *Service) RevokeAllPhoneSessions(ctx context.Context) error {
	if _, err := s.q.RevokeAllPhoneSessions(ctx, s.nowMs()); err != nil {
		return fmt.Errorf("revoke the phone sessions: %w", err)
	}
	return nil
}
