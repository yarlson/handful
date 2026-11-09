package httpapi

import (
	"net/http"

	"yargpt/internal/core"
)

// handOver gives a team a goal of its own from the user, for a team or for none.
func (s *server) handOver(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FromTeamID  string   `json:"fromTeamId"`
		Text        string   `json:"text"`
		Brief       string   `json:"brief"`
		ArtifactIDs []string `json:"artifactIds"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	g, err := s.svc.HandOver(r.Context(), core.Actor{Role: core.RoleUser}, in.FromTeamID, r.PathValue("id"),
		core.NewHandoff{Text: in.Text, Brief: in.Brief, ArtifactIDs: in.ArtifactIDs})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

// handoffAction makes a handler of the user's action on a handoff. text
// reports whether the action takes a text.
func (s *server) handoffAction(act func(*core.Service, *http.Request, core.Actor, string, string) (core.Goal, error), text bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		if text {
			if err := decode(r, &in); err != nil {
				s.fail(w, r, err)
				return
			}
		}
		g, err := act(s.svc, r, core.Actor{Role: core.RoleUser}, r.PathValue("id"), in.Text)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, g)
	}
}

func acceptHandoff(svc *core.Service, r *http.Request, a core.Actor, id, _ string) (core.Goal, error) {
	return svc.AcceptHandoff(r.Context(), a, id)
}

func answerHandoff(svc *core.Service, r *http.Request, a core.Actor, id, text string) (core.Goal, error) {
	return svc.AnswerHandoff(r.Context(), a, id, text)
}

func declineHandoff(svc *core.Service, r *http.Request, a core.Actor, id, text string) (core.Goal, error) {
	return svc.DeclineHandoff(r.Context(), a, id, text)
}

func withdrawHandoff(svc *core.Service, r *http.Request, a core.Actor, id, _ string) (core.Goal, error) {
	return svc.WithdrawHandoff(r.Context(), a, id)
}
