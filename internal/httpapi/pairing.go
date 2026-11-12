package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"handful/internal/config"
	"handful/internal/core"
)

// pairCookie carries the claim of a phone's pair request, which it polls /pair-status with.
const pairCookie = "__Host-handful_pair"

// phoneView is what Settings › Phones shows: whether the phone listener is open, its settings,
// and pairing.
type phoneView struct {
	On         bool         `json:"on"`
	Addr       string       `json:"addr"`
	Proxy      string       `json:"proxy"`
	Hosts      []string     `json:"hosts"`
	TeamDomain string       `json:"teamDomain"`
	AUD        string       `json:"aud"`
	Pairing    core.Pairing `json:"pairing"`
}

func (s *server) phoneView() phoneView {
	settings := s.phone.Settings()
	return phoneView{
		On: s.phone.Addr() != "", Addr: settings.Addr, Proxy: settings.Proxy, Hosts: append([]string{}, settings.Hosts...),
		TeamDomain: settings.TeamDomain, AUD: settings.AUD, Pairing: s.svc.Pairing(),
	}
}

func (s *server) getPhone(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.phoneView())
}

// phoneOn turns phone access on with the way in and the names the body gives: it opens the phone
// listener, without a restart, and then stores them in config.json. A file that says on always has
// an address that opened, so the next start doesn't fail on it.
func (s *server) phoneOn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Proxy      string   `json:"proxy"`
		Hosts      []string `json:"hosts"`
		TeamDomain string   `json:"teamDomain"`
		AUD        string   `json:"aud"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	f, err := s.settingsFile()
	if err != nil {
		writeProblem(w, http.StatusConflict, "", "config.json has an error: "+err.Error()+". Fix it in an editor, or reset it.")
		return
	}
	keys := map[string]any{"phone.enabled": true, "phone.proxy": in.Proxy, "phone.hosts": in.Hosts}
	if in.Proxy == config.ProxyCloudflare {
		keys["phone.cloudflare.teamDomain"], keys["phone.cloudflare.aud"] = in.TeamDomain, in.AUD
	}
	settings, err := phoneSettingsWith(f, keys)
	if err != nil {
		if ke, ok := errors.AsType[*config.KeyError](err); ok {
			writeProblem(w, http.StatusBadRequest, ke.Key, ke.Message)
			return
		}
		s.fail(w, r, err)
		return
	}
	if err := s.phone.TurnOn(settings); err != nil {
		writeProblem(w, http.StatusConflict, "phone.addr", err.Error())
		return
	}
	if s.settings.Path != "" {
		if err := config.Write(s.settings.Path, *f); err != nil {
			s.phone.Stop()
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.phoneView())
}

// settingsFile is config.json, or an empty file for a server without one.
func (s *server) settingsFile() (*config.File, error) {
	if s.settings.Path == "" {
		return &config.File{}, nil
	}
	f, err := config.Load(s.settings.Path)
	return &f, err
}

// phoneSettingsWith sets keys in f and returns its phone settings, checked.
func phoneSettingsWith(f *config.File, keys map[string]any) (config.PhoneSettings, error) {
	for key, v := range keys {
		raw, err := json.Marshal(v)
		if err != nil {
			return config.PhoneSettings{}, err
		}
		if err := f.Set(key, raw); err != nil {
			return config.PhoneSettings{}, err
		}
	}
	settings := f.PhoneSettings()
	return settings, settings.Check()
}

// startPairing makes a pairing code for a phone with the body's scope.
func (s *server) startPairing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Scope string `json:"scope"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if s.phone.Addr() == "" {
		writeProblem(w, http.StatusConflict, "", "Turn on phone access first.")
		return
	}
	code, pairing, err := s.svc.StartPairing(in.Scope)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "pairing": pairing})
}

func (s *server) setPairingScope(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Scope string `json:"scope"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	pairing, err := s.svc.SetPairingScope(in.Scope)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pairing)
}

// decidePairing allows or denies a pair request: the body's decision is "allow" or "deny".
func (s *server) decidePairing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Decision != "allow" && in.Decision != "deny" {
		writeProblem(w, http.StatusBadRequest, "decision", `The decision must be "allow" or "deny".`)
		return
	}
	if err := s.svc.DecidePairing(r.PathValue("id"), in.Decision == "allow"); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requestPairing takes the code a phone typed, and its name. With a working code, the phone
// waits for the user's decision on the computer, and polls /pair-status with the claim cookie.
func (s *server) requestPairing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code       string `json:"code"`
		DeviceName string `json:"deviceName"`
	}
	if err := decodeLimited(r, &in, 1<<10); err != nil {
		s.fail(w, r, err)
		return
	}
	email, _ := r.Context().Value(signedInKey{}).(string)
	claim, err := s.svc.RequestPairing(in.Code, in.DeviceName, email)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: pairCookie, Value: claim, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 2 * core.PairRequestMinutes * 60})
	writeJSON(w, http.StatusAccepted, map[string]string{"state": core.PairWaiting})
}

// pairingStatus tells a phone that waits whether the computer decided. On Allow it sets the
// phone's session cookie.
func (s *server) pairingStatus(w http.ResponseWriter, r *http.Request) {
	state := core.PairDenied
	if c, err := r.Cookie(pairCookie); err == nil {
		got, token, err := s.svc.PairingStatus(r.Context(), c.Value)
		if err != nil && !errors.Is(err, core.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
		if got == core.PairApproved {
			http.SetCookie(w, phoneSessionCookie(token))
		}
		if got != "" {
			state = got
		}
	}
	if state != core.PairWaiting {
		http.SetCookie(w, &http.Cookie{Name: pairCookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": state})
}
