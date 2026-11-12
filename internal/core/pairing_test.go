package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// pairTyped is code as a phone may type it: lower case, with no dashes.
func pairTyped(code string) string { return strings.ToLower(code) }

func TestAPairingCodeIs12SymbolsThatCantBeMisread(t *testing.T) {
	svc, _ := phoneFixture(t)
	seen := map[string]bool{}
	for range 50 {
		code, _, err := svc.StartPairing(ScopeChat)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 12 || strings.Trim(code, "0123456789ABCDEFGHJKMNPQRSTVWXYZ") != "" {
			t.Fatalf("code %q: want 12 of 0-9 and A-Z without I, L, O, U", code)
		}
		seen[code] = true
	}
	if len(seen) != 50 {
		t.Fatalf("50 codes gave %d different ones", len(seen))
	}
}

func TestAPairingCodeWorksOnceInAnyCaseWithOrWithoutDashes(t *testing.T) {
	svc, now := phoneFixture(t)
	code, view, err := svc.StartPairing(ScopeApprove)
	if err != nil || view.Scope != ScopeApprove || view.CodeExpiresAt != now.Add(5*time.Minute).UnixMilli() {
		t.Fatalf("start = %+v, %v; want scope chat+approve, ending in 5 minutes", view, err)
	}
	typed := " " + pairTyped(code[:4]) + "-" + pairTyped(code[4:8]) + " " + code[8:]
	claim, err := svc.RequestPairing(typed, "Pixel 8", "you@example.com")
	if err != nil || claim == "" {
		t.Fatalf("request with %q = %q, %v", typed, claim, err)
	}
	req := svc.Pairing().Request
	if req == nil || req.Name != "Pixel 8" || req.Scope != ScopeApprove || req.PairedBy != "you@example.com" || req.ExpiresAt != now.Add(2*time.Minute).UnixMilli() {
		t.Fatalf("pair request = %+v; want Pixel 8 as chat+approve, by you@example.com, for 2 minutes", req)
	}
	if svc.Pairing().CodeExpiresAt != 0 {
		t.Fatal("the code is still open after its use")
	}
	// AC 13: the second use gets 400 and makes nothing.
	*now = now.Add(time.Second)
	if _, err := svc.RequestPairing(code, "Other", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second use: err = %v; want ErrInvalid", err)
	}
	if got := svc.Pairing().Request; got == nil || got.ID != req.ID {
		t.Fatalf("after the second use the request is %+v; want still %s", got, req.ID)
	}
}

func TestAPairingCodeEndsAfter5Minutes(t *testing.T) {
	svc, now := phoneFixture(t)
	code, _, err := svc.StartPairing(ScopeChat)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(5 * time.Minute)
	if _, err := svc.RequestPairing(code, "Pixel 8", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("after 5 minutes: err = %v; want ErrInvalid (AC 14)", err)
	}
	if v := svc.Pairing(); v.CodeExpiresAt != 0 || v.Request != nil {
		t.Fatalf("after 5 minutes pairing shows %+v; want nothing open", v)
	}
	code, _, _ = svc.StartPairing(ScopeChat)
	*now = now.Add(5*time.Minute - time.Millisecond)
	if _, err := svc.RequestPairing(code, "Pixel 8", ""); err != nil {
		t.Fatalf("1 ms before the end: %v", err)
	}
}

func TestANewCodeVoidsTheOldOne(t *testing.T) {
	svc, now := phoneFixture(t)
	old, _, _ := svc.StartPairing(ScopeChat)
	code, _, _ := svc.StartPairing(ScopeView)
	if _, err := svc.RequestPairing(old, "Pixel 8", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("old code: err = %v; want ErrInvalid", err)
	}
	*now = now.Add(time.Second)
	if _, err := svc.RequestPairing(code, "Pixel 8", ""); err != nil || svc.Pairing().Request.Scope != ScopeView {
		t.Fatalf("new code: %v, %+v; want a request as view", err, svc.Pairing().Request)
	}
}

func TestFiveWrongCodesIn10MinutesPausePairingFor10Minutes(t *testing.T) {
	svc, now := phoneFixture(t)
	code, _, _ := svc.StartPairing(ScopeChat)
	start := *now
	for i := range 4 {
		*now = start.Add(time.Duration(i) * 2 * time.Minute)
		if _, err := svc.RequestPairing("AAAABBBBCCCC", "Pixel 8", ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("wrong code %d: err = %v; want ErrInvalid", i+1, err)
		}
	}
	*now = start.Add(9 * time.Minute)
	if _, err := svc.RequestPairing("AAAABBBBCCCC", "Pixel 8", ""); !errors.Is(err, ErrTooMany) || err.Error() != "5 wrong pairing codes were tried. Pairing is paused for 10 minutes." {
		t.Fatalf("5th wrong code: err = %v; want ErrTooMany with the P6 text", err)
	}
	if v := svc.Pairing(); v.LockedUntil != now.Add(10*time.Minute).UnixMilli() || v.CodeExpiresAt != 0 {
		t.Fatalf("after 5 wrong codes pairing shows %+v; want paused for 10 minutes, the code void", v)
	}
	// AC 15: the right code doesn't work either while paused, nor after it, being void.
	*now = now.Add(10*time.Minute - time.Millisecond)
	if _, err := svc.RequestPairing(code, "Pixel 8", ""); !errors.Is(err, ErrTooMany) {
		t.Fatalf("the right code while paused: err = %v; want ErrTooMany", err)
	}
	if _, _, err := svc.StartPairing(ScopeChat); !errors.Is(err, ErrTooMany) {
		t.Fatalf("a new code while paused: err = %v; want ErrTooMany", err)
	}
	*now = now.Add(time.Millisecond)
	if _, err := svc.RequestPairing(code, "Pixel 8", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the voided code after the pause: err = %v; want ErrInvalid", err)
	}
	if v := svc.Pairing(); v.LockedUntil != 0 {
		t.Fatalf("after 10 minutes pairing shows %+v; want no pause", v)
	}
}

func TestWrongCodesOlderThan10MinutesDontCount(t *testing.T) {
	svc, now := phoneFixture(t)
	for range 4 {
		*now = now.Add(time.Second)
		if _, err := svc.RequestPairing("AAAABBBBCCCC", "Pixel 8", ""); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	*now = now.Add(10 * time.Minute)
	if _, err := svc.RequestPairing("AAAABBBBCCCC", "Pixel 8", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a 5th wrong code 10 minutes after the 4th: err = %v; want ErrInvalid, no pause", err)
	}
}

func TestPairRequestsComeAtMostOnceASecond(t *testing.T) {
	svc, now := phoneFixture(t)
	code, _, _ := svc.StartPairing(ScopeChat)
	if _, err := svc.RequestPairing("AAAABBBBCCCC", "Pixel 8", ""); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	*now = now.Add(999 * time.Millisecond)
	if _, err := svc.RequestPairing(code, "Pixel 8", ""); !errors.Is(err, ErrTooMany) {
		t.Fatalf("a request 999 ms after the last: err = %v; want ErrTooMany", err)
	}
	*now = now.Add(time.Millisecond)
	if _, err := svc.RequestPairing(code, "Pixel 8", ""); err != nil {
		t.Fatalf("a request 1 s after the last: %v", err)
	}
}

func TestABadPhoneNameDoesntUseTheCode(t *testing.T) {
	svc, now := phoneFixture(t)
	code, _, _ := svc.StartPairing(ScopeChat)
	for _, name := range []string{"", "  ", "a\nb", strings.Repeat("x", 61)} {
		if _, err := svc.RequestPairing(code, name, ""); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "name") {
			t.Fatalf("name %q: err = %v; want ErrInvalid about the name", name, err)
		}
	}
	*now = now.Add(time.Second)
	if _, err := svc.RequestPairing(code, strings.Repeat("x", 60), ""); err != nil {
		t.Fatalf("the code after bad names: %v", err)
	}
}

func TestAPairRequestGivesASessionOnlyAfterTheComputerAllowsIt(t *testing.T) {
	svc, now := phoneFixture(t)
	ctx := context.Background()
	code, _, _ := svc.StartPairing(ScopeApprove)
	claim, err := svc.RequestPairing(code, "Pixel 8", "you@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if state, token, err := svc.PairingStatus(ctx, claim); state != PairWaiting || token != "" || err != nil {
		t.Fatalf("before a decision: %q, %q, %v; want waiting, no session", state, token, err)
	}
	if _, _, err := svc.PairingStatus(ctx, "another claim"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown claim: err = %v; want ErrNotFound", err)
	}
	if err := svc.DecidePairing("another id", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("allow an unknown request: err = %v; want ErrNotFound", err)
	}
	if list, _ := svc.PhoneSessions(ctx); len(list) != 0 {
		t.Fatalf("before a decision there are %d phones", len(list))
	}
	*now = now.Add(2*time.Minute - time.Millisecond)
	if err := svc.DecidePairing(svc.Pairing().Request.ID, true); err != nil {
		t.Fatal(err)
	}
	if svc.Pairing().Request != nil {
		t.Fatal("the request still waits after Allow")
	}
	*now = now.Add(2*time.Minute - time.Millisecond)
	state, token, err := svc.PairingStatus(ctx, claim)
	if state != PairApproved || err != nil {
		t.Fatalf("after Allow: %q, %v; want approved", state, err)
	}
	p, err := svc.PhoneSessionByToken(ctx, token)
	if err != nil || p.Name != "Pixel 8" || p.Scope != ScopeApprove || p.PairedBy != "you@example.com" {
		t.Fatalf("the session = %+v, %v; want Pixel 8 as chat+approve by you@example.com", p, err)
	}
	// AC 16: exactly one session.
	if _, _, err := svc.PairingStatus(ctx, claim); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a second claim: err = %v; want ErrNotFound", err)
	}
	if list, _ := svc.PhoneSessions(ctx); len(list) != 1 {
		t.Fatalf("after one Allow there are %d phones; want 1", len(list))
	}
}

func TestAnAllowedPhoneHas2MinutesToCollectItsSession(t *testing.T) {
	svc, now := phoneFixture(t)
	code, _, _ := svc.StartPairing(ScopeChat)
	claim, _ := svc.RequestPairing(code, "Pixel 8", "")
	if err := svc.DecidePairing(svc.Pairing().Request.ID, true); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if state, token, err := svc.PairingStatus(context.Background(), claim); state != PairDenied || token != "" || err != nil {
		t.Fatalf("2 minutes after Allow: %q, %q, %v; want denied, no session", state, token, err)
	}
}

func TestADeniedOrUnansweredPairRequestGivesNoSession(t *testing.T) {
	svc, now := phoneFixture(t)
	ctx := context.Background()
	code, _, _ := svc.StartPairing(ScopeChat)
	claim, _ := svc.RequestPairing(code, "Pixel 8", "")
	id := svc.Pairing().Request.ID
	if err := svc.DecidePairing(id, false); err != nil {
		t.Fatal(err)
	}
	if state, token, err := svc.PairingStatus(ctx, claim); state != PairDenied || token != "" || err != nil {
		t.Fatalf("after Deny: %q, %q, %v; want denied", state, token, err)
	}
	if err := svc.DecidePairing(id, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Allow after Deny: err = %v; want ErrNotFound", err)
	}

	*now = now.Add(time.Second)
	code, _, _ = svc.StartPairing(ScopeChat)
	claim, _ = svc.RequestPairing(code, "Pixel 8", "")
	id = svc.Pairing().Request.ID
	*now = now.Add(2 * time.Minute)
	if svc.Pairing().Request != nil {
		t.Fatal("the request still shows after 2 minutes")
	}
	if state, _, err := svc.PairingStatus(ctx, claim); state != PairDenied || err != nil {
		t.Fatalf("after 2 minutes: %q, %v; want denied", state, err)
	}
	if err := svc.DecidePairing(id, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Allow after 2 minutes: err = %v; want ErrNotFound", err)
	}
	if list, _ := svc.PhoneSessions(ctx); len(list) != 0 {
		t.Fatalf("there are %d phones; want none", len(list))
	}
}

func TestTheScopeOfAnOpenCodeCanChange(t *testing.T) {
	svc, now := phoneFixture(t)
	if _, err := svc.SetPairingScope(ScopeView); !errors.Is(err, ErrNotFound) {
		t.Fatalf("without a code: err = %v; want ErrNotFound", err)
	}
	code, _, _ := svc.StartPairing(ScopeApprove)
	if v, err := svc.SetPairingScope(ScopeView); err != nil || v.Scope != ScopeView {
		t.Fatalf("set view = %+v, %v", v, err)
	}
	for _, bad := range []string{"", "always", "admin"} {
		if _, err := svc.SetPairingScope(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("scope %q: err = %v; want ErrInvalid", bad, err)
		}
		if _, _, err := svc.StartPairing(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("start with scope %q: err = %v; want ErrInvalid", bad, err)
		}
	}
	if _, err := svc.RequestPairing(code, "Pixel 8", ""); err != nil || svc.Pairing().Request.Scope != ScopeView {
		t.Fatalf("request: %v, %+v; want view", err, svc.Pairing().Request)
	}
	*now = now.Add(5 * time.Minute)
	if _, err := svc.SetPairingScope(ScopeChat); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after its use: err = %v; want ErrNotFound", err)
	}
}

func TestPairingTellsTheComputerAtEachChange(t *testing.T) {
	svc, now := phoneFixture(t)
	events, stop := svc.Hub().Subscribe()
	defer stop()
	next := func(step string) {
		t.Helper()
		select {
		case e := <-events:
			if e.Type != "pair" {
				t.Fatalf("%s: event %q; want pair", step, e.Type)
			}
		default:
			t.Fatalf("%s: no event", step)
		}
	}
	code, _, _ := svc.StartPairing(ScopeChat)
	next("start")
	_, _ = svc.SetPairingScope(ScopeView)
	next("scope")
	_, _ = svc.RequestPairing(code, "Pixel 8", "")
	next("request")
	_ = svc.DecidePairing(svc.Pairing().Request.ID, false)
	next("decision")
	for range 4 {
		*now = now.Add(time.Second)
		_, _ = svc.RequestPairing("AAAABBBBCCCC", "Pixel 8", "")
	}
	select {
	case e := <-events:
		t.Fatalf("a wrong code sent %q; want nothing until the pause", e.Type)
	default:
	}
	*now = now.Add(time.Second)
	_, _ = svc.RequestPairing("AAAABBBBCCCC", "Pixel 8", "")
	next("pause")
}
