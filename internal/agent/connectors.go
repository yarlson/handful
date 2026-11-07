package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
)

const maxConnectorsFile = 1 << 20

var connectorName = regexp.MustCompile(`^[a-z0-9-]+$`)

// connector is an external MCP server in <data>/connectors.json. Headers
// hold tokens, so errors and logs never show their values. A connector
// without headers (nil, not empty) signs in with OAuth.
type connector struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	OAuth   *connectorOAuth   `json:"oauth"`
	Bots    []string          `json:"bots"`
}

// connectorOAuth is the client of a server that has no dynamic client
// registration, and the scopes to ask for. CallbackPort, as in Claude Code's
// .mcp.json, makes the redirect URI http://localhost:<port>/callback, for a
// client registered with that redirect.
type connectorOAuth struct {
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret"`
	Scopes       []string `json:"scopes"`
	CallbackPort int      `json:"callbackPort"`
}

// mcpServer is a server entry of Claude Code's --mcp-config file.
type mcpServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// loadConnectors reads the connectors file at path. A missing file means no
// connectors. The file holds tokens, so it must have mode 0600.
func loadConnectors(path string) (map[string]connector, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open connectors file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("check connectors file: %w", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		return nil, fmt.Errorf("connectors file %s has mode %#o; it holds tokens, so it must be 0600 (run chmod 600 %s)", path, perm, path)
	}
	var file struct {
		Servers map[string]connector `json:"servers"`
	}
	dec := json.NewDecoder(io.LimitReader(f, maxConnectorsFile))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("read connectors file %s: %w", path, err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read connectors file %s: unexpected data after the JSON object", path)
	}
	for name, c := range file.Servers {
		if !connectorName.MatchString(name) || name == mcpServerName {
			return nil, fmt.Errorf("connectors file %s: server name %q must match [a-z0-9-]+ and must not be %q", path, name, mcpServerName)
		}
		u, err := url.Parse(c.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("connectors file %s: server %q: url must be an http or https URL with a host", path, name)
		}
		if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("connectors file %s: server %q: url must use https; http sends the headers in cleartext, so it is allowed only for loopback hosts", path, name)
		}
		if c.Headers != nil && c.OAuth != nil {
			return nil, fmt.Errorf("connectors file %s: server %q: set headers or oauth, not both", path, name)
		}
		if c.OAuth != nil && c.OAuth.ClientSecret != "" && c.OAuth.ClientID == "" {
			return nil, fmt.Errorf("connectors file %s: server %q: oauth.clientSecret needs oauth.clientId", path, name)
		}
		if c.OAuth != nil && c.OAuth.CallbackPort != 0 && (c.OAuth.CallbackPort < 1024 || c.OAuth.CallbackPort > 65535) {
			return nil, fmt.Errorf("connectors file %s: server %q: oauth.callbackPort %d must be between 1024 and 65535", path, name, c.OAuth.CallbackPort)
		}
	}
	return file.Servers, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// writeMCPConfig writes the MCP config of one claude process for bot: YarGPT's
// server with token, and the connectors that list the bot. The file is 0600
// because it holds tokens; the caller removes it when the process ends. It
// leaves out the OAuth connectors that have no valid token and returns their
// names, sorted.
func (r *Runner) writeMCPConfig(ctx context.Context, botID, token string) (string, []string, error) {
	connectors, err := loadConnectors(r.connectorsPath())
	if err != nil {
		return "", nil, err
	}
	servers := map[string]mcpServer{
		mcpServerName: {Type: "http", URL: r.cfg.MCPURL, Headers: map[string]string{"Authorization": "Bearer " + token}},
	}
	var signIn []string
	for name, c := range connectors {
		if !slices.Contains(c.Bots, botID) {
			continue
		}
		headers := c.Headers
		if headers == nil {
			access, err := r.accessToken(ctx, name, c)
			if err != nil {
				r.log.Warn("connector left out", "connector", name, "err", err)
				signIn = append(signIn, name)
				continue
			}
			headers = map[string]string{"Authorization": "Bearer " + access}
		}
		servers[name] = mcpServer{Type: "http", URL: c.URL, Headers: headers}
	}
	slices.Sort(signIn)
	data, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return "", nil, fmt.Errorf("encode MCP config: %w", err)
	}
	dir := r.runDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "mcp-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create MCP config file: %w", err)
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		r.removeMCPConfig(f.Name())
		return "", nil, fmt.Errorf("write MCP config file %s: %w", f.Name(), err)
	}
	return f.Name(), signIn, nil
}

func (r *Runner) removeMCPConfig(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		r.log.Warn("remove MCP config file", "path", path, "err", err)
	}
}

func (r *Runner) runDir() string { return filepath.Join(r.svc.DataDir(), "run") }

// removeLeftoverMCPConfigs removes MCP config files of claude processes from
// an earlier run, for example after a hard kill. No such process survives a
// restart, so none of these files is in use.
func (r *Runner) removeLeftoverMCPConfigs() {
	paths, err := filepath.Glob(filepath.Join(r.runDir(), "mcp-*.json"))
	if err != nil {
		r.log.Warn("find leftover MCP config files", "err", err)
		return
	}
	for _, path := range paths {
		r.removeMCPConfig(path)
	}
}
