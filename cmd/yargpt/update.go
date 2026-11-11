package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"yargpt/internal/httpapi"
)

// version is the server's version, without the "v" of its tag. The Makefile
// sets it with -ldflags "-X main.version=..."; a build without a tag is
// "dev-<short sha>", and a plain go build is "dev".
var version = "dev"

// latestReleaseURL is the page that redirects to the newest release; tests
// point it at a local server.
var latestReleaseURL = httpapi.LatestReleaseURL

const (
	updateCheckEvery = 24 * time.Hour
	updateCheckTick  = time.Hour
	updateCheckFile  = "update-check.json"
	noUpdateCheckEnv = "YARGPT_NO_UPDATE_CHECK"
	// maxReleaseRedirects bounds the redirects before the release tag: a
	// renamed repo adds one to the latest page's own.
	maxReleaseRedirects = 5
)

// inAppBundle reports whether the executable at path is the server inside
// YarGPT.app, whose updates the app installs.
func inAppBundle(path string) bool {
	return strings.Contains(path, ".app/Contents/Resources/")
}

// checksForUpdates reports whether a server of version v at path exe looks
// for new releases: only a source build with a release version, when neither
// the environment nor the config file (on) turns the check off.
func checksForUpdates(v, exe string, on bool, getenv func(string) string) bool {
	if !on || getenv(noUpdateCheckEnv) != "" || inAppBundle(exe) {
		return false
	}
	_, ok := parseVersion(v)
	return ok
}

// updateNotice looks for a release newer than the running version, at most
// once per updateCheckEvery, and logs one line when it finds one.
type updateNotice struct {
	version   string
	url       string // the latest release page, which redirects to the tag
	cachePath string
	client    *http.Client
	now       func() time.Time
	log       *slog.Logger

	mu     sync.Mutex
	latest string // a newer release; "" when none is known
	logged string // the release that the log names already
}

type updateCache struct {
	CheckedAt int64  `json:"checkedAt"` // ms
	Latest    string `json:"latest"`
}

func newUpdateNotice(dataDir string, log *slog.Logger) *updateNotice {
	return &updateNotice{
		version: version, url: latestReleaseURL, cachePath: filepath.Join(dataDir, updateCheckFile),
		client: &http.Client{Timeout: 15 * time.Second}, now: time.Now, log: log,
	}
}

// Latest returns a release newer than the running version, or "".
func (n *updateNotice) Latest() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.latest
}

// run checks now and then every updateCheckTick until ctx ends; the cache
// keeps the requests to one per updateCheckEvery.
func (n *updateNotice) run(ctx context.Context) {
	ticker := time.NewTicker(updateCheckTick)
	defer ticker.Stop()
	for {
		n.check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// check asks for the latest release unless the cache is fresh, and notes the
// result. A failure, such as being offline, is logged at debug level only.
func (n *updateNotice) check(ctx context.Context) {
	c, err := n.readCache()
	if err != nil || n.now().Sub(time.UnixMilli(c.CheckedAt)) >= updateCheckEvery {
		latest, err := n.fetch(ctx)
		if err != nil {
			n.log.Debug("check for a new release", "err", err)
			return
		}
		c = updateCache{CheckedAt: n.now().UnixMilli(), Latest: latest}
		if err := n.writeCache(c); err != nil {
			n.log.Debug("save the release check", "err", err)
		}
	}
	n.note(c.Latest)
}

// note keeps latest when it is newer than the running version, and logs it
// once.
func (n *updateNotice) note(latest string) {
	if !newer(latest, n.version) {
		latest = ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.latest = latest
	if latest == "" || latest == n.logged {
		return
	}
	n.logged = latest
	n.log.Info(fmt.Sprintf("Update available: v%s (you have v%s): %s", latest, n.version, httpapi.LatestReleaseURL))
}

// fetch reads the tag of the latest release from the redirect of its page,
// which needs no GitHub API call. It follows the redirects of a renamed repo
// on the same host, and stops at the one that names the tag.
func (n *updateNotice) fetch(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, n.url, nil)
	if err != nil {
		return "", err
	}
	client := *n.client
	client.CheckRedirect = releaseRedirect
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	_, tag, ok := strings.Cut(resp.Header.Get("Location"), "/releases/tag/")
	if !ok {
		return "", fmt.Errorf("the latest release page answered %s without a release tag", resp.Status)
	}
	tag = strings.TrimPrefix(tag, "v")
	if _, ok := parseVersion(tag); !ok {
		return "", fmt.Errorf("the latest release tag %q is not a version", tag)
	}
	return tag, nil
}

// releaseRedirect follows a redirect of the latest release page while it
// stays on the page's host, up to maxReleaseRedirects, and stops at the
// redirect to the release tag, whose Location fetch reads.
func releaseRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("the latest release page redirected to another host, %s", req.URL.Host)
	}
	if strings.Contains(req.URL.Path, "/releases/tag/") {
		return http.ErrUseLastResponse
	}
	if len(via) > maxReleaseRedirects {
		return fmt.Errorf("the latest release page redirected more than %d times", maxReleaseRedirects)
	}
	return nil
}

func (n *updateNotice) readCache() (updateCache, error) {
	data, err := os.ReadFile(n.cachePath)
	if err != nil {
		return updateCache{}, err
	}
	var c updateCache
	return c, json.Unmarshal(data, &c)
}

func (n *updateNotice) writeCache(c updateCache) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(n.cachePath, data, 0o600)
}

// semver is a version such as 1.4.0 or 1.4.0-rc.1.
type semver struct {
	nums [3]int
	pre  string
}

func parseVersion(v string) (semver, bool) {
	numbers, pre, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(numbers, ".")
	if len(parts) != len(semver{}.nums) {
		return semver{}, false
	}
	s := semver{pre: pre}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		s.nums[i] = n
	}
	return s, true
}

// newer reports whether version a is a newer release than b. A version that
// does not parse is never newer, and nothing is newer than it.
func newer(a, b string) bool {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	return okA && okB && compareVersions(va, vb) > 0
}

func compareVersions(a, b semver) int {
	if c := slices.Compare(a.nums[:], b.nums[:]); c != 0 {
		return c
	}
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}
