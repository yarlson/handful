package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"handful/internal/config"

	"handful/internal/core"
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
		{name: "file sets effort", file: `{"effort": "max"}`, want: settings{Claude: "claude", Effort: "max", MaxParallel: 3}},
		{name: "effort flag wins over file", file: `{"effort": "max", "model": "opus"}`, set: map[string]bool{"effort": true}, want: settings{Claude: "claude", Model: "opus", MaxParallel: 3}},
		{name: "bad maxParallel", file: `{"maxParallel": 0}`, wantErr: "maxParallel"},
		{name: "update settings", file: `{"updates": {"autoCheck": true, "drainMinutes": 45}, "updateCheck": true}`, want: settings{Claude: "claude", MaxParallel: 3, Updates: updateSettings{AutoCheck: true, DrainMinutes: 45, UpdateCheck: true}}},
		{name: "drain limit at its least", file: `{"updates": {"drainMinutes": 5}}`, want: settings{Claude: "claude", MaxParallel: 3, Updates: updateSettings{DrainMinutes: 5}}},
		{name: "drain limit below the least", file: `{"updates": {"drainMinutes": 4}}`, want: settings{Claude: "claude", MaxParallel: 3, Updates: updateSettings{DrainMinutes: 5}}},
		{name: "drain limit at its most", file: `{"updates": {"drainMinutes": 240}}`, want: settings{Claude: "claude", MaxParallel: 3, Updates: updateSettings{DrainMinutes: 240}}},
		{name: "drain limit above the most", file: `{"updates": {"drainMinutes": 241}}`, want: settings{Claude: "claude", MaxParallel: 3, Updates: updateSettings{DrainMinutes: 240}}},
		{name: "updates without a drain limit", file: `{"updates": {"autoCheck": false}}`, want: defaults},
		{name: "unknown update setting", file: `{"updates": {"channel": "beta"}}`, wantErr: `unknown field "channel"`},
		{name: "phone access", file: `{"phone": {"enabled": true, "hosts": ["phone.example.com"], "proxy": "tailscale"}}`, want: settings{
			Claude: "claude", MaxParallel: 3,
			Phone: config.PhoneSettings{Enabled: true, Addr: config.DefaultPhoneAddr, Hosts: []string{"phone.example.com"}, Proxy: config.ProxyTailscale},
		}},
		{name: "a phone address on the network", file: `{"phone": {"addr": "0.0.0.0:7778"}}`, wantErr: "phone.addr"},
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
			if tc.want.Phone.Addr == "" {
				tc.want.Phone = config.File{}.PhoneSettings()
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("settings = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestGlobalDefaultsCheckTheModelAndEffortAndNameTheirSource(t *testing.T) {
	noEnv := func(string) string { return "" }
	s := settings{Model: " Opus ", Effort: "MAX"}
	d, err := globalDefaults(&s, map[string]bool{"effort": true}, noEnv)
	if err != nil || s.Model != "opus" || s.Effort != "max" ||
		d.Model != "opus" || d.ModelSource != core.DefaultFromConfig || d.Effort != "max" || d.EffortSource != core.DefaultFromFlag {
		t.Fatalf("globalDefaults = %+v, %+v, %v", d, s, err)
	}
	env := map[string]string{core.EnvModel: "haiku", core.EnvEffort: "low"}
	d, err = globalDefaults(&settings{}, nil, func(k string) string { return env[k] })
	if err != nil || d.Model != "haiku" || d.ModelSource != core.SourceEnv || d.Effort != "low" || d.EffortSource != core.SourceEnv || len(d.EnvOverrides) != 1 {
		t.Fatalf("globalDefaults from the environment = %+v, %v", d, err)
	}
	for _, c := range []struct {
		s    settings
		set  map[string]bool
		want string
	}{
		{settings{Effort: "huge"}, nil, `config.json: "effort" must be one of low, medium, high, xhigh, max`},
		{settings{Effort: "huge"}, map[string]bool{"effort": true}, `-effort: Effort must be one of low, medium, high, xhigh, max.`},
		{settings{Model: "a b"}, nil, `config.json: "model": A model ID can't contain spaces or start with '-'.`},
		{settings{Model: "inherit"}, map[string]bool{"model": true}, `-model: Choose Default instead.`},
	} {
		if _, err := globalDefaults(&c.s, c.set, noEnv); err == nil || err.Error() != c.want {
			t.Errorf("globalDefaults(%+v) err = %v; want %q", c.s, err, c.want)
		}
	}
}
