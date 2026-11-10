package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"yargpt/internal/core"
)

func TestVersionReportsTheBuildDrainModeAndAnEmptyRunningList(t *testing.T) {
	h, svc := newModelHandler(t, core.Defaults{})
	rec := do(h, "GET", "/api/version", "", nil)
	want := `{"version":"dev","draining":false,"running":[],"sourceBuild":true}` + "\n"
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("version = %d %s; want %s", rec.Code, rec.Body, want)
	}

	rec = do(h, "POST", "/api/drain", "", jsonHeader)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"draining":true`) || strings.Contains(rec.Body.String(), `"drainingSince":0`) {
		t.Fatalf("drain = %d %s; want drain mode on, with its start", rec.Code, rec.Body)
	}
	if on, _ := svc.Draining(); !on {
		t.Fatal("POST /api/drain left drain mode off")
	}
	if rec := do(h, "GET", "/api/version", "", nil); !strings.Contains(rec.Body.String(), `"draining":true`) {
		t.Fatalf("version while draining = %s", rec.Body)
	}
	rec = do(h, "DELETE", "/api/drain", "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("undrain = %d %s; want %s", rec.Code, rec.Body, want)
	}
	if on, _ := svc.Draining(); on {
		t.Fatal("DELETE /api/drain left drain mode on")
	}
	if rec := do(h, "POST", "/api/drain", "", nil); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("drain without JSON = %d; want the guard's 415", rec.Code)
	}
}

func TestVersionNamesANewerReleaseOfASourceBuild(t *testing.T) {
	latest := ""
	u := Updates{Version: "1.3.2", SourceBuild: true, Latest: func() string { return latest }}
	h, _ := newModelHandler(t, core.Defaults{}, WithUpdates(u))
	if rec := do(h, "GET", "/api/version", "", nil); strings.Contains(rec.Body.String(), "latest") || !strings.Contains(rec.Body.String(), `"version":"1.3.2"`) {
		t.Fatalf("version with no newer release = %s; want no latest", rec.Body)
	}
	latest = "1.4.0"
	rec := do(h, "GET", "/api/version", "", nil)
	if !strings.Contains(rec.Body.String(), `"latest":"1.4.0","releaseUrl":"`+LatestReleaseURL+`"`) {
		t.Fatalf("version with a newer release = %s; want latest and its page", rec.Body)
	}
	// The update settings are keys of GET /api/settings now; the old endpoint is gone.
	if rec := do(h, "GET", "/api/settings/updates", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/settings/updates = %d %s; want 404: it is gone", rec.Code, rec.Body)
	}

	app, _ := newModelHandler(t, core.Defaults{}, WithUpdates(Updates{Version: "1.4.0"}))
	if rec := do(app, "GET", "/api/version", "", nil); !strings.Contains(rec.Body.String(), `"sourceBuild":false`) || strings.Contains(rec.Body.String(), "latest") {
		t.Fatalf("version inside the app = %s; want no source build and no latest", rec.Body)
	}
}
