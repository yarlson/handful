package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"

	"handful/internal/config"
)

// The builds that the Settings page tells apart.
const (
	BuildApp    = "app"    // Handful.app with a Developer ID signature, which updates itself
	BuildAdhoc  = "adhoc"  // Handful.app built locally, which cannot update itself
	BuildSource = "source" // a server built from source, outside the app
)

// The sources of a runtime value.
const (
	SourceDefault = "default"
	SourceFlag    = "flag"
	SourceApp     = "app" // a flag that the app's LaunchAgent passes
)

// Settings is what the Settings page shows besides the file: how the server
// runs, and how it restarts.
type Settings struct {
	Path    string         // config.json; "" when the server has no settings file
	Running map[string]any // key -> the value the server started with
	Flags   map[string]string
	Runtime []RuntimeValue
	Build   string
	// Restart ends the server so that launchd starts it again: at once, or
	// once no work runs (idle), after the handler turned on drain mode. It
	// is nil when the server does not run under launchd.
	Restart func(idle bool)
	// CancelRestart drops an idle restart that waits, and its hold on drain
	// mode (DELETE /api/restart). It is nil when Restart is.
	CancelRestart func()
	// RestartState reports the idle restart that waits (nil when none), and
	// whether the last one expired. It is nil when Restart is.
	RestartState func() (wait *RestartWait, expired bool)
	// StartupCheck reports why a server started again now would not start
	// with the config file: the config key to blame ("" for the whole file)
	// and the startup error, or nil. nil skips the check.
	StartupCheck func() (key string, err error)
	// LookPath finds the Claude Code program; nil skips the check.
	LookPath func(string) (string, error)
}

// RestartWait is an idle restart that waits for the running work to end:
// since when, and until when (ms) before it gives up.
type RestartWait struct {
	Since    int64 `json:"since"`
	Deadline int64 `json:"deadline"`
}

// restartView is the state of an idle restart that the views report.
type restartView struct {
	// RestartWait is set while an idle restart waits. It is not "restart",
	// which the settings view uses for the restart mode.
	RestartWait *RestartWait `json:"restartWait,omitempty"`
	// RestartExpired is true after the time limit passed with work still
	// running, until the next restart request.
	RestartExpired bool `json:"restartExpired,omitempty"`
}

func (s *server) restartView() restartView {
	if s.settings.RestartState == nil {
		return restartView{}
	}
	wait, expired := s.settings.RestartState()
	return restartView{RestartWait: wait, RestartExpired: expired}
}

// RuntimeValue is a value of the command line that the page shows but
// cannot change.
type RuntimeValue struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"` // SourceDefault, SourceFlag or SourceApp
}

// WithSettings serves the settings of s.
func WithSettings(s Settings) Option {
	return func(srv *server) { srv.settings = s }
}

type settingsView struct {
	File    fileView `json:"file"`
	Build   string   `json:"build"`
	Restart string   `json:"restart"` // "launchd" or "manual"
	// StartError is the error a server started again now would stop with;
	// absent when it would start. Every restart path refuses while it is set.
	StartError string `json:"startError,omitempty"`
	restartView
	Values  orderedValues  `json:"values"`
	Runtime []RuntimeValue `json:"runtime"`
}

type fileView struct {
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
}

type valueView struct {
	key     string
	Value   any    `json:"value"`
	Default any    `json:"default"`
	Source  string `json:"source"` // file, default or flag
	Flag    string `json:"flag,omitempty"`
	// File is the file's value of a key that a flag sets; absent when the
	// file does not set it.
	File    any  `json:"file,omitempty"`
	Running any  `json:"running"`
	Restart bool `json:"restart"`
	// Error says why the server would not start with this value; a restart
	// is refused until it is fixed.
	Error string `json:"error,omitempty"`
}

// orderedValues encodes as an object whose keys keep the page's order.
type orderedValues []valueView

func (vs orderedValues) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, v := range vs {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(v.key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// settingsProblem is the body of a settings change that failed: the key it
// is about ("" for the whole body or file) and what to do.
type settingsProblem struct {
	Key     string `json:"key"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func writeProblem(w http.ResponseWriter, status int, key, message string) {
	writeJSON(w, status, settingsProblem{Key: key, Message: message, Error: message})
}

// getSettings reports every setting with its value, default and source,
// read fresh from the file so that hand edits show at once.
func (s *server) getSettings(w http.ResponseWriter, _ *http.Request) {
	if s.settings.Path == "" {
		writeProblem(w, http.StatusNotFound, "", "This server has no settings file.")
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	writeJSON(w, http.StatusOK, s.settingsView())
}

// settingsView builds the page's view; s.settingsMu is held.
func (s *server) settingsView() settingsView {
	f, err := config.Load(s.settings.Path)
	v := settingsView{File: fileView{Path: s.settings.Path}, Build: s.settings.Build, Restart: "manual", Runtime: s.settings.Runtime}
	if s.settings.Restart != nil {
		v.Restart = "launchd"
	}
	if err != nil {
		v.File.Error = err.Error()
	}
	v.restartView = s.restartView()
	if _, startErr := s.wouldNotStart(); startErr != nil {
		v.StartError = startErr.Error()
	}
	for _, key := range config.Keys {
		v.Values = append(v.Values, s.valueOf(f, err == nil, key))
	}
	return v
}

// valueOf is key's view: the flag's value when a flag sets it, else the
// file's, else the default. Of a file that does not load, it shows the
// running value.
func (s *server) valueOf(f config.File, loaded bool, key string) valueView {
	v := valueView{key: key, Default: config.Default(key), Source: "default", Running: s.settings.Running[key], Restart: config.NeedsRestart(key)}
	v.Value = v.Default
	if flag, ok := s.settings.Flags[key]; ok {
		v.Source, v.Flag, v.Value = "flag", flag, v.Running
		if value, ok := f.Get(key); ok { // a file that does not load sets nothing
			v.File = value
		}
		return v
	}
	if !loaded {
		v.Source, v.Value = "file", v.Running
		return v
	}
	if value, ok := f.Get(key); ok {
		v.Source, v.Value = "file", value
	}
	v.Error = s.startupProblem(key, v.Value)
	return v
}

// startupProblem says why the server would not start with value as key,
// by the checks of startup that the file's load does not make: the model,
// the effort, and a Claude Code program that LookPath must find.
func (s *server) startupProblem(key string, value any) string {
	const after = " The server won't start with it after a restart."
	text, _ := value.(string) // the values of these keys are text
	if key == "model" || key == "effort" {
		if ke, ok := errors.AsType[*config.KeyError](config.CheckText(key, text)); ok {
			return ke.Message + after
		}
	}
	if key != "claude" || s.settings.LookPath == nil {
		return ""
	}
	if _, err := s.settings.LookPath(text); err != nil {
		return "Handful won't start with this program. Fix it before restarting."
	}
	return ""
}

// patchSettings changes the keys of the body, all or none: null resets a
// key to its default. Every value goes through the server's own checks
// before the file is written.
func (s *server) patchSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings.Path == "" {
		writeProblem(w, http.StatusNotFound, "", "This server has no settings file.")
		return
	}
	var body map[string]json.RawMessage
	if err := decode(r, &body); err != nil || body == nil {
		writeProblem(w, http.StatusBadRequest, "", "The body must be an object of settings, such as {\"maxParallel\": 4}.")
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	f, err := config.Load(s.settings.Path)
	if err != nil {
		writeProblem(w, http.StatusConflict, "", "config.json has an error: "+err.Error()+". Fix it in an editor, or reset it.")
		return
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusOK, s.settingsView())
		return
	}
	for _, key := range slices.Sorted(maps.Keys(body)) {
		if status, msg := s.locked(key); status != 0 {
			writeProblem(w, status, key, msg)
			return
		}
		if err := f.Set(key, body[key]); err != nil {
			if ke, ok := errors.AsType[*config.KeyError](err); ok {
				writeProblem(w, http.StatusBadRequest, ke.Key, ke.Message)
				return
			}
			writeProblem(w, http.StatusBadRequest, key, err.Error())
			return
		}
	}
	s.writeSettings(w, r, f)
}

// locked returns 409 and why for a key that the page cannot change: a
// runtime value, or a setting that a flag decides for this run.
func (s *server) locked(key string) (int, string) {
	if slices.ContainsFunc(s.settings.Runtime, func(v RuntimeValue) bool { return v.Key == key }) {
		return http.StatusConflict, key + " is a runtime value; it can't be changed here."
	}
	if _, ok := s.settings.Flags[key]; ok {
		return http.StatusConflict, "Set by the -" + config.Flag(key) + " flag for this run; remove the flag to change it here."
	}
	return 0, ""
}

// resetSettingsFile replaces config.json with an empty object, which also
// repairs a file that no longer loads.
func (s *server) resetSettingsFile(w http.ResponseWriter, r *http.Request) {
	if s.settings.Path == "" {
		writeProblem(w, http.StatusNotFound, "", "This server has no settings file.")
		return
	}
	var body map[string]json.RawMessage
	if err := decode(r, &body); err != nil || len(body) > 0 {
		writeProblem(w, http.StatusBadRequest, "", "The body must be {}.")
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.writeSettings(w, r, config.File{})
}

// writeSettings stores f and answers with the page's view; s.settingsMu is held.
func (s *server) writeSettings(w http.ResponseWriter, r *http.Request, f config.File) {
	if err := config.Write(s.settings.Path, f); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.settingsView())
}

// restart ends the server so that launchd starts it again: now, or once
// the running work is done, through the same drain mode as an update.
func (s *server) restart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		When string `json:"when"`
	}
	if err := decode(r, &in); err != nil || (in.When != "now" && in.When != "idle") {
		writeProblem(w, http.StatusBadRequest, "when", `when must be "now" or "idle".`)
		return
	}
	if s.settings.Restart == nil {
		writeProblem(w, http.StatusNotImplemented, "", "This server doesn't run under launchd. Stop it and start ./bin/handful again.")
		return
	}
	if key, err := s.wouldNotStart(); err != nil {
		writeProblem(w, http.StatusConflict, key, "Handful wouldn't start after a restart: "+err.Error()+". Fix it, then restart.")
		return
	}
	if in.When == "idle" {
		// The restarter holds drain mode; the answer shows it and the wait.
		s.settings.Restart(true)
		writeJSON(w, http.StatusAccepted, s.versionInfo(r))
		return
	}
	// Answer first: a restart now ends the request's context.
	writeJSON(w, http.StatusAccepted, s.versionInfo(r))
	s.settings.Restart(false)
}

// cancelRestart drops the idle restart that waits, and its hold on drain
// mode; a hold of the updater stays.
func (s *server) cancelRestart(w http.ResponseWriter, r *http.Request) {
	if s.settings.CancelRestart == nil {
		writeProblem(w, http.StatusNotImplemented, "", "This server doesn't run under launchd. Stop it and start ./bin/handful again.")
		return
	}
	s.settings.CancelRestart()
	writeJSON(w, http.StatusOK, s.versionInfo(r))
}

// wouldNotStart runs the startup check of the settings, if there is one.
func (s *server) wouldNotStart() (string, error) {
	if s.settings.StartupCheck == nil {
		return "", nil
	}
	return s.settings.StartupCheck()
}
