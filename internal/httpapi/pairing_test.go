package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

// startCode opens f's phone listener and makes a pairing code for scope on the computer.
func (f *phoneFixture) startCode(scope string) string {
	f.t.Helper()
	if f.phone.Addr() == "" {
		f.openPhone() //nolint:bodyclose // it only opens the listener
	}
	rec := do(f.main, http.MethodPost, "/api/phone/pairing", `{"scope":"`+scope+`"}`, jsonHeader)
	var out struct {
		Code    string       `json:"code"`
		Pairing core.Pairing `json:"pairing"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Pairing.Scope != scope {
		f.t.Fatalf("POST /api/phone/pairing = %d %s", rec.Code, rec.Body)
	}
	return out.Code
}

// cookie returns the value rec sets for name, and the cookie.
func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// pairState is a phone's poll of /pair-status with its claim.
func (f *phoneFixture) pairState(claim *http.Cookie) (string, *httptest.ResponseRecorder) {
	f.t.Helper()
	header := map[string]string{}
	if claim != nil {
		header["Cookie"] = claim.Name + "=" + claim.Value
	}
	rec := f.do(req{path: "/pair-status", header: header})
	var out struct {
		State string `json:"state"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		f.t.Fatalf("GET /pair-status = %d %s", rec.Code, rec.Body)
	}
	return out.State, rec
}

func (f *phoneFixture) pending() *core.PairRequest {
	f.t.Helper()
	var view phoneView
	if rec := do(f.main, http.MethodGet, "/api/phone", "", nil); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &view) != nil {
		f.t.Fatalf("GET /api/phone = %d %s", rec.Code, rec.Body)
	}
	return view.Pairing.Request
}

// AC 46 and 16: a code typed in lower case without dashes, then Allow on the computer, gives the
// phone one session with the chosen scope.
func TestAPhonePairsWithATypedCodeAfterTheComputerAllowsIt(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		code := f.startCode(core.ScopeChat)
		rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"` + strings.ToLower(code) + `","deviceName":"Pixel 8"}`})
		claim := cookie(rec, pairCookie)
		if rec.Code != http.StatusAccepted || claim == nil || !claim.Secure || !claim.HttpOnly || claim.SameSite != http.SameSiteStrictMode || claim.Path != "/" {
			t.Fatalf("POST /pair = %d %s, claim %+v; want 202 and a Secure, HttpOnly, Strict claim cookie", rec.Code, rec.Body, claim)
		}
		if state, rec := f.pairState(claim); state != core.PairWaiting || cookie(rec, phoneCookie) != nil {
			t.Fatalf("before a decision: %q; want waiting and no session", state)
		}
		pending := f.pending()
		wantBy := ""
		if f.tunnel {
			wantBy = "you@example.com"
		}
		if pending == nil || pending.Name != "Pixel 8" || pending.Scope != core.ScopeChat || pending.PairedBy != wantBy {
			t.Fatalf("the computer shows %+v; want Pixel 8 as chat, paired by %q", pending, wantBy)
		}
		if rec := do(f.main, http.MethodPost, "/api/phone/pairing/"+pending.ID, `{"decision":"maybe"}`, jsonHeader); rec.Code != http.StatusBadRequest {
			t.Fatalf("decision maybe: %d; want 400", rec.Code)
		}
		if rec := do(f.main, http.MethodPost, "/api/phone/pairing/"+pending.ID, `{"decision":"allow"}`, jsonHeader); rec.Code != http.StatusNoContent {
			t.Fatalf("allow: %d %s", rec.Code, rec.Body)
		}
		state, rec := f.pairState(claim)
		session := cookie(rec, phoneCookie)
		if state != core.PairApproved || session == nil || !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
			t.Fatalf("after Allow: %q, session %+v; want approved with the session cookie", state, session)
		}
		if gone := cookie(rec, pairCookie); gone == nil || gone.MaxAge >= 0 {
			t.Fatalf("after Allow the claim cookie is %+v; want it removed", gone)
		}
		rec = f.do(req{path: "/api/session", session: session.Value})
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"scope":"chat"`) || !strings.Contains(rec.Body.String(), `"device":"Pixel 8"`) {
			t.Fatalf("the new session: %d %s; want Pixel 8 as chat", rec.Code, rec.Body)
		}
		if state, _ := f.pairState(claim); state != core.PairDenied {
			t.Fatalf("a second claim: %q; want denied", state)
		}
		if list, _ := f.svc.PhoneSessions(t.Context()); len(list) != 1 {
			t.Fatalf("there are %d phones; want 1", len(list))
		}
	})
}

func TestADeniedPhoneGetsNoSession(t *testing.T) {
	f := newPhoneFixture(t, false)
	code := f.startCode(core.ScopeApprove)
	rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"` + code + `","deviceName":"Pixel 8"}`})
	claim := cookie(rec, pairCookie)
	if rec := do(f.main, http.MethodPost, "/api/phone/pairing/"+f.pending().ID, `{"decision":"deny"}`, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("deny: %d", rec.Code)
	}
	if state, rec := f.pairState(claim); state != core.PairDenied || cookie(rec, phoneCookie) != nil {
		t.Fatalf("after Deny: %q; want denied, no session", state)
	}
	if state, _ := f.pairState(nil); state != core.PairDenied {
		t.Fatalf("without a claim: %q; want denied", state)
	}
}

// AC 13 and 15 over HTTP: a used code is 400; five wrong codes pause pairing with 429, and the
// computer shows the pause.
func TestWrongCodesPausePairingAndTheComputerShowsIt(t *testing.T) {
	f := newPhoneFixture(t, false)
	code := f.startCode(core.ScopeChat)
	if rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"` + code + `","deviceName":"Pixel 8"}`}); rec.Code != http.StatusAccepted {
		t.Fatalf("first use: %d", rec.Code)
	}
	f.advance(time.Second)
	if rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"` + code + `","deviceName":"Pixel 8"}`}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "This code doesn't work.") {
		t.Fatalf("second use: %d %s; want 400", rec.Code, rec.Body)
	}
	for i := range 3 {
		f.advance(time.Second)
		if rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"AAAA-BBBB-CCCC","deviceName":"Pixel 8"}`}); rec.Code != http.StatusBadRequest {
			t.Fatalf("wrong code %d: %d; want 400", i+2, rec.Code)
		}
	}
	f.advance(time.Second)
	rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"AAAA-BBBB-CCCC","deviceName":"Pixel 8"}`})
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Pairing is paused for 10 minutes.") {
		t.Fatalf("5th wrong code: %d %s; want 429 with P6", rec.Code, rec.Body)
	}
	var view phoneView
	_ = json.Unmarshal(do(f.main, http.MethodGet, "/api/phone", "", nil).Body.Bytes(), &view)
	if view.Pairing.LockedUntil == 0 {
		t.Fatalf("the computer shows %+v; want the pause", view.Pairing)
	}
	if rec := do(f.main, http.MethodPost, "/api/phone/pairing", `{"scope":"chat"}`, jsonHeader); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a new code while paused: %d; want 429", rec.Code)
	}
}

// AC 36 and 39: in tunnel mode pairing needs the Access login, and requests without it never
// count toward the pause.
func TestWithoutTheAccessLoginWrongCodesDontPausePairing(t *testing.T) {
	f := newPhoneFixture(t, true)
	code := f.startCode(core.ScopeChat)
	for range 20 {
		f.advance(time.Second)
		if rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"AAAABBBBCCCC","deviceName":"x"}`, noToken: true}); rec.Code != http.StatusForbidden || rec.Body.String() != "This address needs a Cloudflare login.\n" {
			t.Fatalf("a pair request without the login: %d %q; want 403 with P9", rec.Code, rec.Body)
		}
	}
	if rec := f.do(req{path: "/pair-status", noToken: true}); rec.Code != http.StatusForbidden {
		t.Fatalf("pair-status without the login: %d; want 403", rec.Code)
	}
	f.advance(time.Second)
	if rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"` + code + `","deviceName":"Pixel 8"}`}); rec.Code != http.StatusAccepted {
		t.Fatalf("the user's code after 20 requests without the login: %d %s; want 202", rec.Code, rec.Body)
	}
}

func TestAPairRequestMustBeJSONFromThePhonesOwnPage(t *testing.T) {
	f := newPhoneFixture(t, false)
	code := f.startCode(core.ScopeChat)
	body := `{"code":"` + code + `","deviceName":"Pixel 8"}`
	if rec := f.do(req{method: http.MethodPost, path: "/pair", body: body, header: map[string]string{"Content-Type": "text/plain"}}); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain: %d; want 415", rec.Code)
	}
	if rec := f.do(req{method: http.MethodPost, path: "/pair", body: body, header: map[string]string{"Origin": "https://evil.test"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("a foreign Origin: %d; want 403", rec.Code)
	}
	if rec := f.do(req{method: http.MethodPost, path: "/pair", body: `{"code":"` + code + `","deviceName":"` + strings.Repeat("x", 2000) + `"}`}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a 2 KB body: %d; want 400", rec.Code)
	}
	if rec := f.do(req{method: http.MethodPost, path: "/pair", body: body, header: map[string]string{"Origin": "https://" + phoneHost}}); rec.Code != http.StatusAccepted {
		t.Fatalf("from the phone's own page: %d %s; want 202", rec.Code, rec.Body)
	}
}

// The scope of an open code changes from Settings, and pairing is the computer's only.
func TestPairingIsControlledFromTheComputerOnly(t *testing.T) {
	f := newPhoneFixture(t, false)
	f.startCode(core.ScopeApprove)
	if rec := do(f.main, http.MethodPatch, "/api/phone/pairing", `{"scope":"view"}`, jsonHeader); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"scope":"view"`) {
		t.Fatalf("PATCH scope: %d %s", rec.Code, rec.Body)
	}
	token, _ := f.pair(core.ScopeApprove)
	for _, r := range []req{
		{method: http.MethodPost, path: "/api/phone/pairing", body: `{"scope":"chat"}`},
		{method: http.MethodPatch, path: "/api/phone/pairing", body: `{"scope":"chat"}`},
		{method: http.MethodPost, path: "/api/phone/pairing/x", body: `{"decision":"allow"}`},
		{method: http.MethodPost, path: "/api/phone/on", body: `{}`},
		{path: "/api/phone"},
	} {
		r.session = token
		if rec := f.do(r); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s from a phone: %d; want 404", r.method, r.path, rec.Code)
		}
	}
	if rec := f.do(req{method: http.MethodPost, path: "/pair"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /pair with no body: %d; want 400", rec.Code)
	}
}
