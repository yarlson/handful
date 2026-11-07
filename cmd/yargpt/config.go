package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

const maxConfigFile = 64 << 10

// settings are the server settings that <data>/config.json can set.
type settings struct {
	Claude      string
	Model       string
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
	if file.Claude != nil && !set["claude"] {
		s.Claude = *file.Claude
	}
	if file.Model != nil && !set["model"] {
		s.Model = *file.Model
	}
	if file.MaxParallel != nil && !set["max-parallel"] {
		s.MaxParallel = *file.MaxParallel
	}
	return nil
}
