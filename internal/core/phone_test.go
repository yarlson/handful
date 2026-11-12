package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func phoneFixture(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	svc := newTestService(t)
	now := time.UnixMilli(1_800_000_000_000)
	svc.now = func() time.Time { return now }
	return svc, &now
}

func TestAPhoneSessionWorksByItsTokenAndTheDatabaseKeepsOnlyAHash(t *testing.T) {
	svc, _ := phoneFixture(t)
	ctx := context.Background()
	token, p, err := svc.CreatePhoneSession(ctx, "  Pixel 8 ", ScopeApprove, "you@example.com")
	if err != nil || p.Name != "Pixel 8" || p.Scope != ScopeApprove || p.PairedBy != "you@example.com" || !strings.HasPrefix(token, "hp_") || len(token) < 40 {
		t.Fatalf("create = %q, %+v, %v", token, p, err)
	}
	got, err := svc.PhoneSessionByToken(ctx, token)
	if err != nil || got.ID != p.ID {
		t.Fatalf("by token = %+v, %v; want %s", got, err, p.ID)
	}
	other, _, err := svc.CreatePhoneSession(ctx, "iPad", ScopeView, "")
	if err != nil || other == token {
		t.Fatalf("a second token = %q, %v; want a different one", other, err)
	}
	// AC 17: no raw token is stored, in any column.
	var dump bytes.Buffer
	rows, err := svc.sqlDB.QueryContext(ctx, `SELECT id, name, scope, token_hash, paired_by FROM phone_sessions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, scope, by string
		var hash []byte
		if err := rows.Scan(&id, &name, &scope, &hash, &by); err != nil {
			t.Fatal(err)
		}
		dump.WriteString(id + name + scope + by)
		dump.Write(hash)
		if len(hash) != 32 {
			t.Fatalf("token hash of %s is %d bytes; want a SHA-256", id, len(hash))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{token, other, strings.TrimPrefix(token, "hp_")} {
		if bytes.Contains(dump.Bytes(), []byte(tok)) {
			t.Fatal("a raw token is in the phone_sessions table")
		}
	}
	for _, bad := range []string{"", "hp_", "hp_nope", "x" + token[1:], token + "x", strings.Repeat("hp_x", 30)} {
		if _, err := svc.PhoneSessionByToken(ctx, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("token %q: err = %v; want ErrNotFound", bad, err)
		}
	}
}

func TestAPhoneSessionEndsAfter30IdleDaysOr90DaysInAll(t *testing.T) {
	svc, now := phoneFixture(t)
	ctx := context.Background()
	token, p, err := svc.CreatePhoneSession(ctx, "Pixel", ScopeView, "")
	if err != nil {
		t.Fatal(err)
	}
	// Used every 29 days, it lasts until 90 days after pairing.
	for range 3 {
		*now = now.Add(29 * 24 * time.Hour)
		if _, err := svc.PhoneSessionByToken(ctx, token); err != nil {
			t.Fatalf("after %v: %v", now.Sub(time.UnixMilli(p.CreatedAt)), err)
		}
	}
	*now = time.UnixMilli(p.CreatedAt).Add(90*24*time.Hour - time.Millisecond)
	if _, err := svc.PhoneSessionByToken(ctx, token); err != nil {
		t.Fatalf("just before 90 days: %v", err)
	}
	*now = time.UnixMilli(p.CreatedAt).Add(90 * 24 * time.Hour)
	if _, err := svc.PhoneSessionByToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("at 90 days: %v; want ErrNotFound", err)
	}
	idle, q, err := svc.CreatePhoneSession(ctx, "Idle", ScopeView, "")
	if err != nil {
		t.Fatal(err)
	}
	*now = time.UnixMilli(q.CreatedAt).Add(30*24*time.Hour - time.Millisecond)
	if ok, err := svc.PhoneSessionActive(ctx, q.ID); !ok || err != nil {
		t.Fatalf("just before 30 idle days: %v, %v", ok, err)
	}
	*now = time.UnixMilli(q.CreatedAt).Add(30 * 24 * time.Hour)
	if _, err := svc.PhoneSessionByToken(ctx, idle); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after 30 idle days: %v; want ErrNotFound", err)
	}
	if ok, err := svc.PhoneSessionActive(ctx, q.ID); ok || err != nil {
		t.Fatalf("active after 30 idle days = %v, %v", ok, err)
	}
	if ok, err := svc.PhoneSessionActive(ctx, "missing"); ok || err != nil {
		t.Fatalf("a missing session: %v, %v", ok, err)
	}
}

func TestARequestIsNotedAtMostEveryMinute(t *testing.T) {
	svc, now := phoneFixture(t)
	ctx := context.Background()
	token, p, err := svc.CreatePhoneSession(ctx, "Pixel", ScopeView, "")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(phoneTouch - time.Millisecond)
	if got, _ := svc.PhoneSessionByToken(ctx, token); got.LastSeenAt != p.LastSeenAt {
		t.Fatalf("within a minute: last seen %d; want %d", got.LastSeenAt, p.LastSeenAt)
	}
	*now = now.Add(time.Millisecond)
	if got, _ := svc.PhoneSessionByToken(ctx, token); got.LastSeenAt != now.UnixMilli() {
		t.Fatalf("after a minute: last seen %d; want %d", got.LastSeenAt, now.UnixMilli())
	}
	list, err := svc.PhoneSessions(ctx)
	if err != nil || len(list) != 1 || list[0].LastSeenAt != now.UnixMilli() {
		t.Fatalf("list = %+v, %v; want the request noted", list, err)
	}
}

func TestRevokeEndsOnePhoneAndRevokeAllEndsEvery(t *testing.T) {
	svc, now := phoneFixture(t)
	ctx := context.Background()
	a, pa, _ := svc.CreatePhoneSession(ctx, "A", ScopeChat, "")
	b, pb, _ := svc.CreatePhoneSession(ctx, "B", ScopeChat, "")
	c, _, _ := svc.CreatePhoneSession(ctx, "C", ScopeChat, "")
	if err := svc.RevokePhoneSession(ctx, pa.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PhoneSessionByToken(ctx, a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked: %v; want ErrNotFound", err)
	}
	if _, err := svc.PhoneSessionByToken(ctx, b); err != nil {
		t.Fatalf("the other phone: %v", err)
	}
	first := now.UnixMilli()
	*now = now.Add(time.Hour)
	if err := svc.RevokePhoneSession(ctx, pa.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.PhoneSessions(ctx); list[0].RevokedAt != first {
		t.Fatalf("revoked again at %d; want the first time %d", list[0].RevokedAt, first)
	}
	if err := svc.RevokePhoneSession(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke a missing session: %v", err)
	}
	if err := svc.RevokeAllPhoneSessions(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{b, c} {
		if _, err := svc.PhoneSessionByToken(ctx, tok); !errors.Is(err, ErrNotFound) {
			t.Fatalf("after revoke all: %v; want ErrNotFound", err)
		}
	}
	if ok, _ := svc.PhoneSessionActive(ctx, pb.ID); ok {
		t.Fatal("B is still active after revoke all")
	}
}

func TestAPhoneNeedsANameAndAScope(t *testing.T) {
	svc, _ := phoneFixture(t)
	ctx := context.Background()
	for _, c := range [][2]string{{"", ScopeView}, {"  ", ScopeView}, {strings.Repeat("x", 61), ScopeView}, {"a\nb", ScopeView}, {"P", "admin"}, {"P", ""}} {
		if _, _, err := svc.CreatePhoneSession(ctx, c[0], c[1], ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("create %q %q: %v; want ErrInvalid", c[0], c[1], err)
		}
	}
	if _, _, err := svc.CreatePhoneSession(ctx, strings.Repeat("é", 60), ScopeView, ""); err != nil {
		t.Fatalf("60 characters: %v", err)
	}
}

func TestScopesIncludeTheOnesBelowThem(t *testing.T) {
	cases := map[string][]bool{ScopeView: {true, false, false}, ScopeChat: {true, true, false}, ScopeApprove: {true, true, true}, "x": {false, false, false}}
	for scope, want := range cases {
		p := PhoneSession{Scope: scope}
		for i, need := range []string{ScopeView, ScopeChat, ScopeApprove} {
			if p.Allows(need) != want[i] {
				t.Errorf("%s allows %s = %v; want %v", scope, need, p.Allows(need), want[i])
			}
		}
		if p.Allows("x") || p.Allows("") {
			t.Errorf("%s allows an unknown scope", scope)
		}
	}
}

// pendingApproval starts a request for tool and returns its card's id and the request's result.
func pendingApproval(t *testing.T, svc *Service, tool string) (string, chan bool) {
	t.Helper()
	bot := newTestBot(t, svc)
	events, stop := svc.Hub().Subscribe()
	defer stop()
	done := make(chan bool, 1)
	go func() {
		ok, _, _ := svc.RequestApproval(context.Background(), ApprovalRequest{BotID: bot.ID, TranscriptID: bot.ID, TurnID: "t-" + tool, Tool: tool, Input: json.RawMessage(`{}`)})
		done <- ok
	}()
	for e := range events {
		if e.Type == "entry" && e.Entry.Kind == KindApproval {
			return e.Entry.ID, done
		}
	}
	return "", done
}

func TestAPhoneAllowsOnceOrDeniesAndTheCardNamesIt(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	card, done := pendingApproval(t, svc, "Bash")
	if err := svc.ResolveApprovalOnPhone(ctx, card, DecisionAlways, "Pixel 8"); !errors.Is(err, ErrNotAllowed) || !strings.Contains(err.Error(), "Always allow can only be chosen on the computer.") {
		t.Fatalf("always from a phone: %v; want ErrNotAllowed", err)
	}
	if grants, _ := svc.ToolGrants(ctx, entryBot(t, svc, card)); len(grants) != 0 {
		t.Fatalf("grants = %v; want none", grants)
	}
	if err := svc.ResolveApprovalOnPhone(ctx, card, DecisionOnce, "Pixel 8"); err != nil {
		t.Fatal(err)
	}
	if !<-done {
		t.Fatal("allowed once from a phone; the request was denied")
	}
	if body := cardBody(t, svc, card); body.DecidedOn != "Pixel 8" || body.Decision != DecisionOnce || body.Status != "allowed" {
		t.Fatalf("card = %+v; want allowed once on Pixel 8", body)
	}
	deny, done := pendingApproval(t, svc, "Write")
	if err := svc.ResolveApprovalOnPhone(ctx, deny, DecisionDeny, "Pixel 8"); err != nil || <-done {
		t.Fatalf("deny from a phone: %v", err)
	}
	if body := cardBody(t, svc, deny); body.DecidedOn != "Pixel 8" || body.Status != "denied" {
		t.Fatalf("card = %+v; want denied on Pixel 8", body)
	}
	desk, done := pendingApproval(t, svc, "Edit")
	if err := svc.ResolveApproval(ctx, desk, DecisionOnce); err != nil || !<-done {
		t.Fatal(err)
	}
	if body := cardBody(t, svc, desk); body.DecidedOn != "" {
		t.Fatalf("a decision on the computer names %q; want none", body.DecidedOn)
	}
}

func TestAPhoneOnlyDeniesASetup(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	card, done := pendingApproval(t, svc, ToolCreateSetup)
	if err := svc.ResolveApprovalOnPhone(ctx, card, DecisionOnce, "Pixel"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("allow a setup from a phone: %v; want ErrNotAllowed", err)
	}
	if err := svc.ResolveApprovalOnPhone(ctx, card, DecisionDeny, "Pixel"); err != nil || <-done {
		t.Fatalf("deny a setup from a phone: %v", err)
	}
}

func entryBot(t *testing.T, svc *Service, id string) string {
	t.Helper()
	e, err := svc.GetEntry(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e.BotID
}

func cardBody(t *testing.T, svc *Service, id string) approvalBody {
	t.Helper()
	e, err := svc.GetEntry(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var b approvalBody
	if err := json.Unmarshal(e.Body, &b); err != nil {
		t.Fatal(err)
	}
	return b
}
