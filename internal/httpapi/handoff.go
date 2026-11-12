package httpapi

import (
	"net/http"

	"handful/internal/core"
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

func pushBack(svc *core.Service, r *http.Request, a core.Actor, id, text string) (core.Goal, error) {
	return svc.PushBack(r.Context(), a, id, text)
}

func stopTask(svc *core.Service, r *http.Request, a core.Actor, id, _ string) (core.Goal, error) {
	return svc.StopTask(r.Context(), a, id)
}

func reopenTask(svc *core.Service, r *http.Request, a core.Actor, id, _ string) (core.Goal, error) {
	return svc.ReopenTask(r.Context(), a, id)
}

func approveTask(svc *core.Service, r *http.Request, a core.Actor, id, _ string) (core.Goal, error) {
	return svc.ApproveTask(r.Context(), a, id)
}

func changesTask(svc *core.Service, r *http.Request, a core.Actor, id, text string) (core.Goal, error) {
	return svc.ChangesTask(r.Context(), a, id, text)
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

func dismissTask(svc *core.Service, r *http.Request, _ core.Actor, id, _ string) (core.Goal, error) {
	return svc.DismissTask(r.Context(), id)
}

// userTask gives a team a task from the user: into its inbox, or, with
// startNow, to the top of its plan.
func (s *server) userTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text        string   `json:"text"`
		Brief       string   `json:"brief"`
		ArtifactIDs []string `json:"artifactIds"`
		StartNow    bool     `json:"startNow"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	g, err := s.svc.UserTask(r.Context(), r.PathValue("id"), core.NewTask{Text: in.Text, Brief: in.Brief, ArtifactIDs: in.ArtifactIDs}, in.StartNow)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

// makeTask makes a task taken at once from turns of the team that were on
// no task.
func (s *server) makeTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text    string   `json:"text"`
		Brief   string   `json:"brief"`
		TurnIDs []string `json:"turnIds"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	g, err := s.svc.MakeTask(r.Context(), core.Actor{Role: core.RoleUser}, r.PathValue("id"), core.NewTask{Text: in.Text, Brief: in.Brief}, in.TurnIDs)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

// offTask lists the team's turns of the last hour that were on no task.
func (s *server) offTask(w http.ResponseWriter, r *http.Request) {
	off, err := s.svc.RecentOffTask(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, off)
}

// dismissOffTask puts away the team's turns on no task up to now.
func (s *server) dismissOffTask(w http.ResponseWriter, r *http.Request) {
	off, err := s.svc.DismissOffTask(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, off)
}

// waitForBudget is the user's "Wait until midnight" on a team at its daily
// budget: Needs you no longer lists the pause today.
func (s *server) waitForBudget(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.WaitForBudget(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	budget, err := s.svc.TeamBudget(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, budget)
}

// takeTask is the user's take of a task from a team's inbox into its plan.
func (s *server) takeTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Position int64 `json:"position"`
	}
	if err := decodeOptional(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	g, err := s.svc.TakeTask(r.Context(), core.Actor{Role: core.RoleUser}, r.PathValue("id"), in.Position)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// orderPlan puts a team's planned tasks in the user's order.
func (s *server) orderPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Order []string `json:"order"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.svc.OrderPlan(r.Context(), core.Actor{Role: core.RoleUser}, r.PathValue("id"), in.Order); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pinTask pins or unpins a planned task.
func (s *server) pinTask(pinned bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g, err := s.svc.PinTask(r.Context(), r.PathValue("id"), pinned)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, g)
	}
}

// taskEvents is a task's history: its decisions and its steps' events.
func (s *server) taskEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.svc.TaskEvents(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}
