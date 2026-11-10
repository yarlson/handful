package httpapi

import "net/http"

// itemEvents lists the history of a board item, oldest first.
func (s *server) itemEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.svc.ItemEvents(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// waiting lists what waits for the user: open questions, approvals and paused goals.
func (s *server) waiting(w http.ResponseWriter, r *http.Request) {
	waiting, err := s.svc.Waiting(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, waiting)
}

// needsYou lists what waits for the user, oldest first.
func (s *server) needsYou(w http.ResponseWriter, r *http.Request) {
	needs, err := s.svc.NeedsYou(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, needs)
}

// chatQuestions lists the open questions of one chat.
func (s *server) chatQuestions(w http.ResponseWriter, r *http.Request) {
	questions, err := s.svc.OpenQuestions(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, questions)
}

// dismissQuestion closes an open question without an answer.
func (s *server) dismissQuestion(w http.ResponseWriter, r *http.Request) {
	e, err := s.svc.DismissQuestion(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// reopenQuestion undoes a dismissal.
func (s *server) reopenQuestion(w http.ResponseWriter, r *http.Request) {
	e, err := s.svc.ReopenQuestion(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}
