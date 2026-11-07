package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigFileSetsDefaultsThatFlagsOverride(t *testing.T) {
	defaults := settings{Claude: "claude", Model: "", MaxParallel: 3}
	cases := []struct {
		name    string
		file    string // "" means no file
		set     map[string]bool
		want    settings
		wantErr string
	}{
		{name: "missing file", want: defaults},
		{name: "file sets model", file: `{"model": "sonnet"}`, want: settings{Claude: "claude", Model: "sonnet", MaxParallel: 3}},
		{name: "file sets all", file: `{"claude": "/opt/claude", "model": "opus", "maxParallel": 5}`, want: settings{Claude: "/opt/claude", Model: "opus", MaxParallel: 5}},
		{name: "flag wins over file", file: `{"model": "sonnet", "maxParallel": 5}`, set: map[string]bool{"model": true}, want: settings{Claude: "claude", Model: "", MaxParallel: 5}},
		{name: "unknown field", file: `{"modle": "sonnet"}`, wantErr: `unknown field "modle"`}, //nolint:misspell // the misspelled field name is the input under test
		{name: "trailing data", file: `{"model": "sonnet"} {}`, wantErr: "after the JSON object"},
		{name: "bad maxParallel", file: `{"maxParallel": 0}`, wantErr: "maxParallel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if tc.file != "" {
				if err := os.WriteFile(path, []byte(tc.file), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := defaults
			err := applyConfigFile(path, tc.set, &got)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v; want an error naming %s and %q", err, path, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("settings = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}
