package config

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// Phone access is off by default. These are the values of its keys when the file lacks them.
const (
	DefaultPhoneAddr = "127.0.0.1:7778"
	ProxyCloudflare  = "cloudflare" // a Cloudflare Tunnel with a Cloudflare Access login in front
	ProxyTailscale   = "tailscale"  // tailscale serve, on the user's tailnet
	maxPhoneHosts    = 10
)

// Phone is the "phone" object of the file: a second listener for a paired phone, which a proxy
// on this computer forwards to.
type Phone struct {
	Enabled    *bool        `json:"enabled,omitempty"`
	Addr       *string      `json:"addr,omitempty"`
	Hosts      []string     `json:"hosts,omitempty"`
	Proxy      *string      `json:"proxy,omitempty"`
	Cloudflare *PhoneAccess `json:"cloudflare,omitempty"`
}

// PhoneAccess is the Cloudflare Access application in front of the tunnel: the team's domain,
// which issues and signs its tokens, and the application's AUD tag.
type PhoneAccess struct {
	TeamDomain *string `json:"teamDomain,omitempty"`
	AUD        *string `json:"aud,omitempty"`
}

// PhoneSettings are the phone keys with their defaults applied.
type PhoneSettings struct {
	Enabled    bool
	Addr       string
	Hosts      []string
	Proxy      string
	TeamDomain string // a host name, such as you.cloudflareaccess.com
	AUD        string
}

// PhoneSettings returns the file's phone keys, with the defaults for those it lacks.
func (f File) PhoneSettings() PhoneSettings {
	p := f.Phone
	if p == nil {
		p = &Phone{}
	}
	cf := p.Cloudflare
	if cf == nil {
		cf = &PhoneAccess{}
	}
	return PhoneSettings{
		Enabled:    p.Enabled != nil && *p.Enabled,
		Addr:       valueOr(p.Addr, DefaultPhoneAddr),
		Hosts:      append([]string{}, p.Hosts...),
		Proxy:      valueOr(p.Proxy, ProxyCloudflare),
		TeamDomain: valueOr(cf.TeamDomain, ""),
		AUD:        valueOr(cf.AUD, ""),
	}
}

func valueOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// Check reports, as a *KeyError, what stops phone access from starting: it is on, but a key it
// needs is missing.
func (p PhoneSettings) Check() error {
	if !p.Enabled {
		return nil
	}
	if len(p.Hosts) == 0 {
		return &KeyError{Key: "phone.hosts", Message: "Phone access needs the address your phone opens, such as phone.example.com."}
	}
	if p.Proxy == ProxyCloudflare && p.TeamDomain == "" {
		return &KeyError{Key: "phone.cloudflare.teamDomain", Message: "A Cloudflare Tunnel needs the team domain of its Access login."}
	}
	if p.Proxy == ProxyCloudflare && p.AUD == "" {
		return &KeyError{Key: "phone.cloudflare.aud", Message: "A Cloudflare Tunnel needs the AUD tag of its Access application."}
	}
	return nil
}

func (p *Phone) get(key string) (any, bool) {
	if p == nil {
		return nil, false
	}
	cf := p.Cloudflare
	if cf == nil {
		cf = &PhoneAccess{}
	}
	switch key {
	case "phone.enabled":
		return deref(p.Enabled)
	case "phone.addr":
		return deref(p.Addr)
	case "phone.hosts":
		if p.Hosts == nil {
			return nil, false
		}
		return append([]string{}, p.Hosts...), true
	case "phone.proxy":
		return deref(p.Proxy)
	case "phone.cloudflare.teamDomain":
		return deref(cf.TeamDomain)
	case "phone.cloudflare.aud":
		return deref(cf.AUD)
	}
	return nil, false
}

func (f *File) setPhone(key string, raw json.RawMessage) error {
	if f.Phone == nil {
		f.Phone = &Phone{}
	}
	p := f.Phone
	if p.Cloudflare == nil {
		p.Cloudflare = &PhoneAccess{}
	}
	setters := map[string]func() error{
		"phone.enabled":               func() error { return setBool(&p.Enabled, key, raw) },
		"phone.addr":                  func() error { return setString(&p.Addr, key, raw, checkPhoneAddr) },
		"phone.hosts":                 func() error { return setHosts(&p.Hosts, raw) },
		"phone.proxy":                 func() error { return setString(&p.Proxy, key, raw, checkProxy) },
		"phone.cloudflare.teamDomain": func() error { return setString(&p.Cloudflare.TeamDomain, key, raw, checkTeamDomain) },
		"phone.cloudflare.aud":        func() error { return setString(&p.Cloudflare.AUD, key, raw, checkAUD) },
	}
	var err error = &KeyError{Key: key, Message: key + " isn't a setting."}
	if set, ok := setters[key]; ok {
		err = set()
	}
	if *p.Cloudflare == (PhoneAccess{}) {
		p.Cloudflare = nil
	}
	if p.Enabled == nil && p.Addr == nil && p.Hosts == nil && p.Proxy == nil && p.Cloudflare == nil {
		f.Phone = nil
	}
	return err
}

func setHosts(dst *[]string, raw json.RawMessage) error {
	if isNull(raw) {
		*dst = nil
		return nil
	}
	var hosts []string
	if err := json.Unmarshal(raw, &hosts); err != nil {
		return &KeyError{Key: "phone.hosts", Message: "phone.hosts must be a list of host names."}
	}
	hosts, err := checkHosts(hosts)
	if err != nil {
		return err
	}
	*dst = hosts
	return nil
}

// check applies the per-key rules to a file read from disk, and keeps the values as Set would
// store them: host names, the team domain and the AUD tag in lower case, the domain without
// https://.
func (p *Phone) check() error {
	if p == nil {
		return nil
	}
	hosts, err := checkHosts(p.Hosts)
	if err != nil {
		return err
	}
	p.Hosts = hosts
	if err := normalize(p.Addr, checkPhoneAddr); err != nil {
		return err
	}
	if err := normalize(p.Proxy, checkProxy); err != nil {
		return err
	}
	if cf := p.Cloudflare; cf != nil {
		if err := normalize(cf.TeamDomain, checkTeamDomain); err != nil {
			return err
		}
		return normalize(cf.AUD, checkAUD)
	}
	return nil
}

// normalize replaces *v, when it is set, with what check makes of it.
func normalize(v *string, check func(string) (string, error)) error {
	if v == nil {
		return nil
	}
	s, err := check(*v)
	if err != nil {
		return err
	}
	*v = s
	return nil
}

// checkPhoneAddr accepts a loopback address with a port: Handful never listens on the network,
// and a proxy on this computer forwards the phone's requests to it.
func checkPhoneAddr(s string) (string, error) {
	host, port, err := net.SplitHostPort(s)
	n, perr := strconv.Atoi(port)
	if err != nil || perr != nil || n < 1 || n > 65535 {
		return "", &KeyError{Key: "phone.addr", Message: "phone.addr must be a loopback address with a port, such as 127.0.0.1:7778."}
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", &KeyError{Key: "phone.addr", Message: "phone.addr must be a loopback address; Handful never listens on the network itself."}
	}
	return s, nil
}

// hostName is a DNS name of letters, digits and hyphens, with at least one dot.
var hostName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// checkHosts accepts 1 to maxPhoneHosts public host names, in lower case. A loopback name
// would let any local request through, and a quick tunnel has no login in front.
func checkHosts(hosts []string) ([]string, error) {
	if hosts == nil {
		return nil, nil
	}
	if len(hosts) == 0 || len(hosts) > maxPhoneHosts {
		return nil, &KeyError{Key: "phone.hosts", Message: fmt.Sprintf("phone.hosts must list 1 to %d host names.", maxPhoneHosts)}
	}
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if !hostName.MatchString(h) || len(h) > 253 || net.ParseIP(h) != nil {
			return nil, &KeyError{Key: "phone.hosts", Message: fmt.Sprintf("%q isn't a host name, such as phone.example.com.", h)}
		}
		if h == "localhost" || strings.HasSuffix(h, ".localhost") {
			return nil, &KeyError{Key: "phone.hosts", Message: "phone.hosts must be the name your phone opens, not localhost."}
		}
		if h == "trycloudflare.com" || strings.HasSuffix(h, ".trycloudflare.com") {
			return nil, &KeyError{Key: "phone.hosts", Message: "Quick tunnels (trycloudflare.com) have no login in front; use a named tunnel with Cloudflare Access."}
		}
		out = append(out, h)
	}
	return out, nil
}

func checkProxy(s string) (string, error) {
	if s != ProxyCloudflare && s != ProxyTailscale {
		return "", &KeyError{Key: "phone.proxy", Message: `phone.proxy must be "cloudflare" or "tailscale".`}
	}
	return s, nil
}

// checkTeamDomain accepts the Access team domain as a host name, or as an https URL of it.
func checkTeamDomain(s string) (string, error) {
	h := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "https://"), "/")
	if !hostName.MatchString(h) {
		return "", &KeyError{Key: "phone.cloudflare.teamDomain", Message: "The team domain must be a host name, such as you.cloudflareaccess.com."}
	}
	return h, nil
}

// audTag is an Access application's AUD tag: hexadecimal.
var audTag = regexp.MustCompile(`^[0-9a-f]{16,128}$`)

func checkAUD(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !audTag.MatchString(s) {
		return "", &KeyError{Key: "phone.cloudflare.aud", Message: "The AUD tag is the application's hexadecimal tag, from its Overview page."}
	}
	return s, nil
}
