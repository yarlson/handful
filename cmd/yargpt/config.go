package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"yargpt/internal/core"
)

const maxConfigFile = 64 << 10

// settings are the server settings that <data>/config.json can set.
type settings struct {
	Claude      string
	Model       string
	Effort      string
	MaxParallel int
}

// applyConfigFile reads the config file at path into s, except the settings
// whose flags were given on the command line (set, by flag name). A missing
// file changes nothing.
func applyConfigFile(path string, set map[string]bool, s *settings) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer f.Close()
	var file struct {
		Claude      *string `json:"claude"`
		Model       *string `json:"model"`
		Effort      *string `json:"effort"`
		MaxParallel *int    `json:"maxParallel"`
	}
	dec := json.NewDecoder(io.LimitReader(f, maxConfigFile))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return fmt.Errorf("read config file %s: %w", path, err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("read config file %s: unexpected data after the JSON object", path)
	}
	if file.MaxParallel != nil && *file.MaxParallel < 1 {
		return fmt.Errorf("config file %s: maxParallel must be at least 1", path)
	}
	fromFile(&s.Claude, file.Claude, set["claude"])
	fromFile(&s.Model, file.Model, set["model"])
	fromFile(&s.Effort, file.Effort, set["effort"])
	fromFile(&s.MaxParallel, file.MaxParallel, set["max-parallel"])
	return nil
}

// fromFile sets *dst to the file's value v, unless the file lacks it or its
// flag was given.
func fromFile[T any](dst, v *T, flagGiven bool) {
	if v != nil && !flagGiven {
		*dst = *v
	}
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
		return fmt.Errorf("-%s: %s", name, msg)
	}
	if rest, ok := strings.CutPrefix(msg, "Effort must"); ok {
		return fmt.Errorf("config.json: %q must%s", name, strings.TrimSuffix(rest, "."))
	}
	return fmt.Errorf("config.json: %q: %s", name, msg)
}
