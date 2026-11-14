package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

type hueView struct {
	ID, Name, Hue string
}

// A new bot comes back with its stored hue; PATCH hue saves it, GET and the list show it, and
// a name that is not in the palette is refused.
func TestBotHueRoundTripsThroughTheAPI(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := do(h, "POST", "/api/bots", `{"name":"Scout"}`, jsonHeader)
	var created hueView
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &created) != nil || created.Hue != "sky" {
		t.Fatalf("create bot: %d %s; want 201 with hue sky", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/bots/"+created.ID, `{"hue":"fuchsia"}`, jsonHeader); rec.Code != http.StatusOK {
		t.Fatalf("PATCH hue: %d %s; want 200", rec.Code, rec.Body)
	}
	var got hueView
	if rec := do(h, "GET", "/api/bots/"+created.ID, "", nil); json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Hue != "fuchsia" {
		t.Fatalf("GET bot after PATCH: %d %s; want hue fuchsia", rec.Code, rec.Body)
	}
	var list []hueView
	if rec := do(h, "GET", "/api/bots", "", nil); json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list) != 1 || list[0].Hue != "fuchsia" {
		t.Fatalf("GET bots after PATCH: %d %s; want the bot with hue fuchsia", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/bots/"+created.ID, `{"hue":"rose"}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH hue rose: %d %s; want 400", rec.Code, rec.Body)
	}
}
