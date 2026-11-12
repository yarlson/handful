// Package config reads, checks and writes <data>/config.json, the settings
// file that the server reads when it starts, the desktop app reads before
// each update check, and the Settings page edits.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"handful/internal/core"
)

// The settings' defaults: the values that apply when the file lacks a key.
const (
	DefaultClaude       = "claude"
	DefaultMaxParallel  = 3
	DefaultAutoCheck    = true
	DefaultDrainMinutes = 30
	DefaultUpdateCheck  = true
)

// Limits of updates.drainMinutes. The server clamps a file value outside
// them when it starts; Set rejects it.
const (
	MinDrainMinutes = 5
	MaxDrainMinutes = 240
)

const (
	maxFileBytes = 64 << 10
	maxClaudeLen = 1024
)

// Keys are the settings, in the order of the Settings page and the file.
var Keys = []string{
	"model", "effort", "maxParallel", "claude", "updates.autoCheck", "updates.drainMinutes", "updateCheck",
	"phone.enabled", "phone.addr", "phone.hosts", "phone.proxy", "phone.cloudflare.teamDomain", "phone.cloudflare.aud",
}

// flags names the command-line flag that overrides a key.
var flags = map[string]string{"model": "model", "effort": "effort", "maxParallel": "max-parallel", "claude": "claude"}

// restart holds the keys that apply only when the server starts again.
var restart = map[string]bool{
	"model": true, "effort": true, "maxParallel": true, "claude": true, "updateCheck": true,
	"phone.enabled": true, "phone.addr": true, "phone.hosts": true, "phone.proxy": true, "phone.cloudflare.teamDomain": true, "phone.cloudflare.aud": true,
}

// File is config.json. A nil field is a key the file does not set. The
// fields are in the order of Keys, which is the order Write keeps.
type File struct {
	Model       *string  `json:"model,omitempty"`
	Effort      *string  `json:"effort,omitempty"`
	MaxParallel *int     `json:"maxParallel,omitempty"`
	Claude      *string  `json:"claude,omitempty"`
	Updates     *Updates `json:"updates,omitempty"`
	UpdateCheck *bool    `json:"updateCheck,omitempty"`
	Phone       *Phone   `json:"phone,omitempty"`
}

// Updates is the "updates" object of the file, which the desktop app reads.
type Updates struct {
	AutoCheck    *bool `json:"autoCheck,omitempty"`
	DrainMinutes *int  `json:"drainMinutes,omitempty"`
}

// KeyError is a value that a key does not accept.
type KeyError struct {
	Key     string
	Message string
}

func (e *KeyError) Error() string { return fmt.Sprintf("%q: %s", e.Key, e.Message) }

// Flag returns the command-line flag that overrides key, or "".
func Flag(key string) string { return flags[key] }

// NeedsRestart reports whether a change of key applies only after the
// server starts again.
func NeedsRestart(key string) bool { return restart[key] }

// Default returns the value of key when the file does not set it.
func Default(key string) any {
	values := map[string]any{
		"model": "", "effort": "", "maxParallel": DefaultMaxParallel, "claude": DefaultClaude,
		"updates.autoCheck": DefaultAutoCheck, "updates.drainMinutes": DefaultDrainMinutes, "updateCheck": DefaultUpdateCheck,
		"phone.enabled": false, "phone.addr": DefaultPhoneAddr, "phone.hosts": []string{}, "phone.proxy": ProxyCloudflare,
		"phone.cloudflare.teamDomain": "", "phone.cloudflare.aud": "",
	}
	return values[key]
}

// Load reads the file at path strictly, as the server does when it starts:
// an unknown key, data after the object or a maxParallel below 1 is an
// error. A missing file is an empty File.
func Load(path string) (File, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("open config file: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return File{}, fmt.Errorf("read config file %s: %w", path, err)
	}
	if len(raw) > maxFileBytes {
		return File{}, fmt.Errorf("read config file %s: larger than %d bytes", path, maxFileBytes)
	}
	var f File
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("read config file %s: %w", path, err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return File{}, fmt.Errorf("read config file %s: unexpected data after the JSON object", path)
	}
	if f.MaxParallel != nil {
		if err := checkMaxParallel(*f.MaxParallel); err != nil {
			return File{}, fmt.Errorf("config file %s: %w", path, err)
		}
	}
	if err := f.Phone.check(); err != nil {
		return File{}, fmt.Errorf("config file %s: %w", path, err)
	}
	return f, nil
}

// Get returns the file's value of key, and false when the file does not set it.
func (f File) Get(key string) (any, bool) {
	u := f.Updates
	if u == nil {
		u = &Updates{}
	}
	if strings.HasPrefix(key, "phone.") {
		return f.Phone.get(key)
	}
	values := map[string]func() (any, bool){
		"model":                func() (any, bool) { return deref(f.Model) },
		"effort":               func() (any, bool) { return deref(f.Effort) },
		"maxParallel":          func() (any, bool) { return deref(f.MaxParallel) },
		"claude":               func() (any, bool) { return deref(f.Claude) },
		"updates.autoCheck":    func() (any, bool) { return deref(u.AutoCheck) },
		"updates.drainMinutes": func() (any, bool) { return deref(u.DrainMinutes) },
		"updateCheck":          func() (any, bool) { return deref(f.UpdateCheck) },
	}
	get, ok := values[key]
	if !ok {
		return nil, false
	}
	return get()
}

func deref[T any](p *T) (any, bool) {
	if p == nil {
		return nil, false
	}
	return *p, true
}

// Set checks raw, a JSON value, as the server's own rules do and stores it as
// key. JSON null removes the key, so its default applies. It returns a
// *KeyError for an unknown key or a value the key does not accept.
func (f *File) Set(key string, raw json.RawMessage) error {
	setters := map[string]func(json.RawMessage) error{
		"model":                func(r json.RawMessage) error { return setString(&f.Model, key, r, textRules[key]) },
		"effort":               func(r json.RawMessage) error { return setString(&f.Effort, key, r, textRules[key]) },
		"maxParallel":          func(r json.RawMessage) error { return setInt(&f.MaxParallel, key, r, checkMaxParallel) },
		"claude":               func(r json.RawMessage) error { return setString(&f.Claude, key, r, textRules[key]) },
		"updates.autoCheck":    func(r json.RawMessage) error { return setBool(&f.updates().AutoCheck, key, r) },
		"updates.drainMinutes": func(r json.RawMessage) error { return setInt(&f.updates().DrainMinutes, key, r, checkDrainMinutes) },
		"updateCheck":          func(r json.RawMessage) error { return setBool(&f.UpdateCheck, key, r) },
	}
	if strings.HasPrefix(key, "phone.") {
		return f.setPhone(key, raw)
	}
	set, ok := setters[key]
	if !ok {
		return &KeyError{Key: key, Message: key + " isn't a setting."}
	}
	err := set(raw)
	if u := f.Updates; u != nil && u.AutoCheck == nil && u.DrainMinutes == nil {
		f.Updates = nil
	}
	return err
}

// textRules check and normalize the text keys.
var textRules = map[string]func(string) (string, error){"model": core.NormalizeModel, "effort": core.NormalizeEffort, "claude": checkClaude}

// CheckText reports, as a *KeyError, whether the text key accepts value by
// the rules of Set. A key that is not a text key accepts nothing.
func CheckText(key, value string) error {
	normalize, ok := textRules[key]
	if !ok {
		return &KeyError{Key: key, Message: key + " isn't a text setting."}
	}
	_, err := normalizeText(key, value, normalize)
	return err
}

func (f *File) updates() *Updates {
	if f.Updates == nil {
		f.Updates = &Updates{}
	}
	return f.Updates
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func setString(dst **string, key string, raw json.RawMessage, normalize func(string) (string, error)) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return &KeyError{Key: key, Message: key + " must be text."}
	}
	s, err := normalizeText(key, s, normalize)
	if err != nil {
		return err
	}
	*dst = &s
	return nil
}

// normalizeText runs normalize on s, with its error as a *KeyError of key.
func normalizeText(key, s string, normalize func(string) (string, error)) (string, error) {
	s, err := normalize(s)
	if ke, ok := errors.AsType[*KeyError](err); ok {
		return "", ke
	}
	if err != nil {
		return "", &KeyError{Key: key, Message: strings.TrimPrefix(err.Error(), core.ErrInvalid.Error()+": ")}
	}
	return s, nil
}

func setInt(dst **int, key string, raw json.RawMessage, check func(int) error) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return &KeyError{Key: key, Message: key + " must be a whole number."}
	}
	if err := check(n); err != nil {
		return err
	}
	*dst = &n
	return nil
}

func setBool(dst **bool, key string, raw json.RawMessage) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return &KeyError{Key: key, Message: key + " must be true or false."}
	}
	*dst = &b
	return nil
}

func checkMaxParallel(n int) error {
	if n < 1 {
		return &KeyError{Key: "maxParallel", Message: "Turns at once must be 1 or more."}
	}
	return nil
}

func checkDrainMinutes(n int) error {
	if n < MinDrainMinutes || n > MaxDrainMinutes {
		return &KeyError{Key: "updates.drainMinutes", Message: fmt.Sprintf("Wait for running bots must be from %d to %d minutes.", MinDrainMinutes, MaxDrainMinutes)}
	}
	return nil
}

// checkClaude checks the Claude Code program. It returns the value as given,
// since it may be a path whose spaces matter.
func checkClaude(s string) (string, error) {
	if s == "" || utf8.RuneCountInString(s) > maxClaudeLen || strings.ContainsRune(s, 0) {
		return "", &KeyError{Key: "claude", Message: fmt.Sprintf("The Claude Code program must be 1 to %d characters, with no NUL character.", maxClaudeLen)}
	}
	return s, nil
}

// Write stores f at path atomically: a temporary file in the same folder,
// synced and renamed over the old one, so a reader never sees half a file.
// The keys keep the order of Keys, indented by 2 spaces.
func Write(path string, f File) (err error) {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmp.Name()))
		}
	}()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return errors.Join(fmt.Errorf("write config file: %w", err), tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync config file: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace config file %s: %w", path, err)
	}
	return nil
}
