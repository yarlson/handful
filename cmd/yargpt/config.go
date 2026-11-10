package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"yargpt/internal/config"
	"yargpt/internal/core"
)

// settings are the server settings that <data>/config.json can set.
type settings struct {
	Claude      string
	Model       string
	Effort      string
	MaxParallel int
	Updates     updateSettings
}

// updateSettings are the update settings of config.json.
type updateSettings struct {
	AutoCheck    bool // the app checks for updates by itself
	DrainMinutes int  // how long the app waits for running work before it asks again
	UpdateCheck  bool // a source build checks for new releases
}

// applyConfigFile reads the config file at path into s, except the settings
// whose flags were given on the command line (set, by flag name). A missing
// file changes nothing.
func applyConfigFile(path string, set map[string]bool, s *settings) error {
	file, err := config.Load(path)
	if err != nil {
		return err
	}
	fromFile(&s.Claude, file.Claude, set["claude"])
	fromFile(&s.Model, file.Model, set["model"])
	fromFile(&s.Effort, file.Effort, set["effort"])
	fromFile(&s.MaxParallel, file.MaxParallel, set["max-parallel"])
	fromFile(&s.Updates.UpdateCheck, file.UpdateCheck, false)
	if u := file.Updates; u != nil {
		fromFile(&s.Updates.AutoCheck, u.AutoCheck, false)
		applyDrainMinutes(&s.Updates, u.DrainMinutes)
	}
	return nil
}

// applyDrainMinutes sets the file's drain limit, if it has one, clamped to
// the limits of config.
func applyDrainMinutes(u *updateSettings, minutes *int) {
	if minutes != nil {
		u.DrainMinutes = min(max(*minutes, config.MinDrainMinutes), config.MaxDrainMinutes)
	}
}

// fromFile sets *dst to the file's value v, unless the file lacks it or its
// flag was given.
func fromFile[T any](dst, v *T, flagGiven bool) {
	if v != nil && !flagGiven {
		*dst = *v
	}
}

// loadSettings applies the config file to the settings of o and checks
// them as the server does when it starts: the model, the effort and the
// Claude Code program, which lookPath must find. It is the one check of
// startup and of a restart, which starts from the flags again. An error
// names its config key when one key is to blame (a *startupError).
func (o *options) loadSettings(getenv func(string) string, lookPath func(string) (string, error)) error {
	if err := applyConfigFile(filepath.Join(o.dataDir, "config.json"), o.set, &o.settings); err != nil {
		return err
	}
	var err error
	if o.defaults, err = globalDefaults(&o.settings, o.set, getenv); err != nil {
		return err
	}
	if o.claudePath, err = lookPath(o.Claude); err != nil {
		return &startupError{key: "claude", err: fmt.Errorf("find Claude Code CLI %q: %w", o.Claude, err)}
	}
	return nil
}

// startupError is a setting that stops the server from starting.
type startupError struct {
	key string // the config key
	err error
}

func (e *startupError) Error() string { return e.err.Error() }

// startupKey returns the config key that err blames, or "" when it blames
// the whole file.
func startupKey(err error) string {
	if se, ok := errors.AsType[*startupError](err); ok {
		return se.key
	}
	return ""
}

// globalDefaults checks the global model and effort of s, normalizes them in
// s, and returns them with their sources and the environment's variables.
// set holds the flags given on the command line; a value set without its
// flag comes from the config file.
func globalDefaults(s *settings, set map[string]bool, getenv func(string) string) (core.Defaults, error) {
	model, err := core.NormalizeModel(s.Model)
	if err != nil {
		return core.Defaults{}, settingError("model", set, err)
	}
	effort, err := core.NormalizeEffort(s.Effort)
	if err != nil {
		return core.Defaults{}, settingError("effort", set, err)
	}
	s.Model, s.Effort = model, effort
	return core.NewDefaults(model, settingSource(set, "model"), effort, settingSource(set, "effort"), getenv), nil
}

func settingSource(set map[string]bool, name string) string {
	if set[name] {
		return core.DefaultFromFlag
	}
	return core.DefaultFromConfig
}

// settingError names the flag or config key of a bad setting, as in
// `config.json: "effort" must be one of low, ...`.
func settingError(name string, set map[string]bool, err error) error {
	msg := strings.TrimPrefix(err.Error(), core.ErrInvalid.Error()+": ")
	if set[name] {
		return &startupError{key: name, err: fmt.Errorf("-%s: %s", name, msg)}
	}
	if rest, ok := strings.CutPrefix(msg, "Effort must"); ok {
		return &startupError{key: name, err: fmt.Errorf("config.json: %q must%s", name, strings.TrimSuffix(rest, "."))}
	}
	return &startupError{key: name, err: fmt.Errorf("config.json: %q: %s", name, msg)}
}
