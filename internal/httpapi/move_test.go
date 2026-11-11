package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"handful/internal/core"
)

// The welcome notice shows until the user closes it, and a server without a
// move shows none.
func TestTheMoveNoticeShowsUntilTheUserClosesIt(t *testing.T) {
	plain, _ := newTestHandler(t)
	if rec := do(plain, "GET", "/api/move-notice", "", nil); rec.Code != http.StatusOK || rec.Body.String() != `{"show":false}`+"\n" {
		t.Fatalf("no move: %d %s; want no notice", rec.Code, rec.Body)
	}
	if rec := do(plain, "POST", "/api/move-notice/close", `{}`, jsonHeader); rec.Code != http.StatusNoContent {
		t.Fatalf("close with no move: %d %s; want 204", rec.Code, rec.Body)
	}

	open := true
	h, _ := newModelHandler(t, core.Defaults{}, WithMoveNotice(MoveNotice{
		Show:  func() (bool, error) { return open, nil },
		Close: func() error { open = false; return nil },
	}))
	if rec := do(h, "GET", "/api/move-notice", "", nil); rec.Body.String() != `{"show":true}`+"\n" {
		t.Fatalf("after a move: %s; want the notice", rec.Body)
	}
	if rec := do(h, "POST", "/api/move-notice/close", `{}`, jsonHeader); rec.Code != http.StatusNoContent || open {
		t.Fatalf("close: %d, open %v; want it closed", rec.Code, open)
	}
	if rec := do(h, "GET", "/api/move-notice", "", nil); rec.Body.String() != `{"show":false}`+"\n" {
		t.Fatalf("after closing: %s; want no notice", rec.Body)
	}

	broken, _ := newModelHandler(t, core.Defaults{}, WithMoveNotice(MoveNotice{
		Show:  func() (bool, error) { return false, errors.New("read moved-from-yargpt.json: bad") },
		Close: func() error { return errors.New("disk full") },
	}))
	if rec := do(broken, "GET", "/api/move-notice", "", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("unreadable record: %d %s; want 500", rec.Code, rec.Body)
	}
	if rec := do(broken, "POST", "/api/move-notice/close", `{}`, jsonHeader); rec.Code != http.StatusInternalServerError {
		t.Fatalf("unwritable record: %d %s; want 500", rec.Code, rec.Body)
	}
}
