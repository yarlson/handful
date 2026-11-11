package httpapi

import "net/http"

// MoveNotice is the welcome notice after the move from YarGPT, Handful's
// old name (spec ad09e72c §5, S4).
type MoveNotice struct {
	Show  func() (bool, error) // whether the notice still shows
	Close func() error         // records that the user closed it
}

// WithMoveNotice serves the welcome notice of a move. Without it, the API
// reports no notice.
func WithMoveNotice(n MoveNotice) Option {
	return func(s *server) { s.moveNotice = n }
}

func (s *server) getMoveNotice(w http.ResponseWriter, r *http.Request) {
	show := false
	if s.moveNotice.Show != nil {
		var err error
		if show, err = s.moveNotice.Show(); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"show": show})
}

func (s *server) closeMoveNotice(w http.ResponseWriter, r *http.Request) {
	if s.moveNotice.Close != nil {
		if err := s.moveNotice.Close(); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
