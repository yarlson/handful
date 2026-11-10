package httpapi

import (
	"net/http"

	"yargpt/internal/core"
)

// LatestReleaseURL is the page of the newest YarGPT release.
const LatestReleaseURL = "https://github.com/yarlson/yargpt/releases/latest"

// Updates is what the server knows about its version and about updates.
type Updates struct {
	Version string // "1.4.0", or "dev-<sha>" for a build without a tag
	// SourceBuild is true when the server does not run inside YarGPT.app,
	// which cannot replace it.
	SourceBuild bool
	// Latest returns a release newer than Version, or "" when none is known.
	Latest func() string
}

// DefaultUpdates are the updates of a server that WithUpdates does not set.
func DefaultUpdates() Updates {
	return Updates{Version: "dev", SourceBuild: true}
}

// WithUpdates sets the version and the updates that the API reports.
func WithUpdates(u Updates) Option {
	return func(s *server) { s.updates = u }
}

type versionInfo struct {
	Version       string      `json:"version"`
	Draining      bool        `json:"draining"`
	DrainingSince int64       `json:"drainingSince,omitempty"`
	Running       []core.Busy `json:"running"`
	Latest        string      `json:"latest,omitempty"`
	ReleaseURL    string      `json:"releaseUrl,omitempty"`
	SourceBuild   bool        `json:"sourceBuild"`
	restartView
}

// version reports the server's version, whether it drains, and the work that
// keeps it from restarting. The app polls it while it waits for an update.
func (s *server) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.versionInfo(r))
}

func (s *server) versionInfo(r *http.Request) versionInfo {
	draining, since := s.svc.Draining()
	v := versionInfo{Version: s.updates.Version, Draining: draining, DrainingSince: since, Running: s.runner.Busy(r.Context()), SourceBuild: s.updates.SourceBuild, restartView: s.restartView()}
	if s.updates.Latest != nil {
		v.Latest = s.updates.Latest()
	}
	if v.Latest != "" {
		v.ReleaseURL = LatestReleaseURL
	}
	return v
}

// setDrain turns the updater's hold on drain mode on (POST) or off (DELETE),
// which a waiting restart's own hold outlasts, and reports the version
// as GET /api/version does.
func (s *server) setDrain(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.svc.SetDraining(on)
		writeJSON(w, http.StatusOK, s.versionInfo(r))
	}
}
