package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func phoneKeyError(t *testing.T, key, raw string) string {
	t.Helper()
	var f File
	err := f.Set(key, json.RawMessage(raw))
	ke, ok := errors.AsType[*KeyError](err)
	if !ok || ke.Key != key {
		t.Fatalf("Set(%s, %s) = %v; want a *KeyError for %s", key, raw, err, key)
	}
	if f.Phone != nil {
		t.Fatalf("Set(%s, %s) refused, but stored %+v", key, raw, f.Phone)
	}
	return ke.Message
}

func TestThePhoneAddressMustBeLoopback(t *testing.T) {
	for _, raw := range []string{`"0.0.0.0:7778"`, `"192.168.1.5:7778"`, `"phone.example.com:443"`, `"127.0.0.1"`, `"127.0.0.1:0"`, `"127.0.0.1:70000"`, `7778`} {
		phoneKeyError(t, "phone.addr", raw)
	}
	var f File
	for _, ok := range []string{`"127.0.0.1:7778"`, `"[::1]:7778"`, `"localhost:1"`, `"127.0.0.1:65535"`} {
		if err := f.Set("phone.addr", json.RawMessage(ok)); err != nil {
			t.Errorf("Set(phone.addr, %s) = %v; want it accepted", ok, err)
		}
	}
}

func TestPhoneHostsArePublicNamesAndNoQuickTunnel(t *testing.T) {
	if msg := phoneKeyError(t, "phone.hosts", `["x.trycloudflare.com"]`); !strings.Contains(msg, "Quick tunnels") {
		t.Errorf("a quick tunnel: %q", msg)
	}
	for _, raw := range []string{
		`["trycloudflare.com"]`, `["localhost"]`, `["a.localhost"]`, `["127.0.0.1"]`, `["no_dots"]`, `["phone"]`, `[]`, `"phone.example.com"`,
		`["a.b","c.d","e.f","g.h","i.j","k.l","m.n","o.p","q.r","s.t","u.v"]`,
	} {
		phoneKeyError(t, "phone.hosts", raw)
	}
	var f File
	if err := f.Set("phone.hosts", json.RawMessage(`["Phone.Example.com","mac.tail1234.ts.net"]`)); err != nil {
		t.Fatal(err)
	}
	if got := f.PhoneSettings().Hosts; strings.Join(got, ",") != "phone.example.com,mac.tail1234.ts.net" {
		t.Fatalf("hosts = %q", got)
	}
	ten := `["a.b","c.d","e.f","g.h","i.j","k.l","m.n","o.p","q.r","s.t"]`
	if err := f.Set("phone.hosts", json.RawMessage(ten)); err != nil {
		t.Fatalf("10 hosts: %v", err)
	}
	// A host name is at most 253 characters.
	label := strings.Repeat("a", 61)
	longest := strings.Repeat(label+".", 5)[:253-1] + "b"
	if len(longest) != 253 {
		t.Fatalf("test host is %d characters", len(longest))
	}
	if err := f.Set("phone.hosts", json.RawMessage(`["`+longest+`"]`)); err != nil {
		t.Fatalf("a host of 253 characters: %v", err)
	}
	phoneKeyError(t, "phone.hosts", `["`+longest+`b"]`)
}

func TestThePhoneProxyAndAccessKeysAreChecked(t *testing.T) {
	phoneKeyError(t, "phone.proxy", `"ngrok"`)
	phoneKeyError(t, "phone.cloudflare.teamDomain", `"not a domain"`)
	phoneKeyError(t, "phone.cloudflare.aud", `"xyz"`)
	phoneKeyError(t, "phone.enabled", `"yes"`)
	phoneKeyError(t, "phone.nothing", `1`)
}

func TestPhoneAccessIsOffByDefaultAndNeedsItsKeysWhenOn(t *testing.T) {
	off := File{}.PhoneSettings()
	if off.Enabled || off.Addr != DefaultPhoneAddr || off.Proxy != ProxyCloudflare || len(off.Hosts) != 0 || off.Check() != nil {
		t.Fatalf("defaults = %+v, %v; want off, on 7778, cloudflare", off, off.Check())
	}
	on := PhoneSettings{Enabled: true, Addr: DefaultPhoneAddr, Proxy: ProxyCloudflare}
	for _, c := range []struct {
		change func(*PhoneSettings)
		key    string
	}{
		{func(*PhoneSettings) {}, "phone.hosts"},
		{func(p *PhoneSettings) { p.Hosts = []string{"phone.example.com"} }, "phone.cloudflare.teamDomain"},
		{func(p *PhoneSettings) {
			p.Hosts, p.TeamDomain = []string{"phone.example.com"}, "you.cloudflareaccess.com"
		}, "phone.cloudflare.aud"},
	} {
		p := on
		c.change(&p)
		if ke, ok := errors.AsType[*KeyError](p.Check()); !ok || ke.Key != c.key {
			t.Errorf("Check(%+v) = %v; want an error for %s", p, p.Check(), c.key)
		}
	}
	ready := PhoneSettings{Enabled: true, Hosts: []string{"phone.example.com"}, Proxy: ProxyCloudflare, TeamDomain: "you.cloudflareaccess.com", AUD: "abcdef0123456789"}
	tailnet := PhoneSettings{Enabled: true, Hosts: []string{"mac.tail1234.ts.net"}, Proxy: ProxyTailscale}
	if ready.Check() != nil || tailnet.Check() != nil {
		t.Fatalf("complete settings: %v, %v; want nil", ready.Check(), tailnet.Check())
	}
}

func TestLoadRefusesAFileWithABadPhoneKey(t *testing.T) {
	for _, body := range []string{
		`{"phone":{"addr":"0.0.0.0:7778"}}`, `{"phone":{"hosts":["x.trycloudflare.com"]}}`, `{"phone":{"proxy":"x"}}`,
		`{"phone":{"cloudflare":{"teamDomain":"no"}}}`, `{"phone":{"cloudflare":{"aud":"no"}}}`,
	} {
		if _, err := Load(writeFile(t, body)); err == nil {
			t.Errorf("Load(%s) = nil; want an error", body)
		}
	}
	f, err := Load(writeFile(t, `{"phone":{"enabled":true,"hosts":["phone.example.com"],"cloudflare":{"teamDomain":"you.cloudflareaccess.com","aud":"abcdef0123456789"}}}`))
	if err != nil || !f.PhoneSettings().Enabled || f.PhoneSettings().AUD != "abcdef0123456789" {
		t.Fatalf("Load = %+v, %v", f.Phone, err)
	}
}
