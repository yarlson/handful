// Package httpapi serves YarGPT's JSON API, its event stream, the workers' MCP
// endpoint and the web UI on a loopback address.
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"yargpt/internal/agent"
	"yargpt/internal/core"
)

const maxBodyBytes = 1 << 20

// maxMessageBodyBytes fits a message with the most media, base64-encoded in
// JSON; maxBodyBytes covers the text and the JSON around the files.
var maxMessageBodyBytes = int64(maxBodyBytes + base64.StdEncoding.EncodedLen(core.MaxMediaBytes))

type server struct {
	svc    *core.Service
	runner *agent.Runner
	log    *slog.Logger
}

// New returns the HTTP handler. ui holds the built web app; it may be nil.
func New(svc *core.Service, runner *agent.Runner, ui fs.FS, log *slog.Logger) http.Handler {
	s := &server{svc: svc, runner: runner, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/bots", s.listBots)
	mux.HandleFunc("POST /api/bots", s.createBot)
	mux.HandleFunc("PUT /api/bots/order", s.reorderBots)
	mux.HandleFunc("GET /api/bots/{id}", s.getBot)
	mux.HandleFunc("PATCH /api/bots/{id}", s.updateBot)
	mux.HandleFunc("DELETE /api/bots/{id}", s.deleteBot)
	mux.HandleFunc("GET /api/bots/{id}/entries", s.listEntries)
	mux.HandleFunc("POST /api/bots/{id}/messages", s.sendMessage)
	mux.HandleFunc("GET /api/media/{name}", s.media)
	mux.HandleFunc("POST /api/bots/{id}/interrupt", s.interrupt)
	mux.HandleFunc("POST /api/bots/{id}/clear", s.clearContext)
	mux.HandleFunc("POST /api/bots/{id}/compact", s.compactContext)
	mux.HandleFunc("POST /api/rooms", s.createRoom)
	mux.HandleFunc("PUT /api/rooms/{id}/members", s.setRoomMembers)
	mux.HandleFunc("POST /api/teams", s.createTeam)
	mux.HandleFunc("PATCH /api/teams/{id}", s.updateTeam)
	mux.HandleFunc("GET /api/teams/{id}/board", s.board)
	mux.HandleFunc("POST /api/teams/{id}/goals", s.startGoal)
	mux.HandleFunc("POST /api/goals/{id}/extend", s.extendGoal)
	mux.HandleFunc("POST /api/goals/{id}/end", s.endGoal)
	mux.HandleFunc("POST /api/goals/{id}/accept", s.acceptGoal)
	mux.HandleFunc("GET /api/bots/{id}/memory", s.listMemory)
	mux.HandleFunc("POST /api/bots/{id}/memory", s.addMemory)
	mux.HandleFunc("DELETE /api/memory/{id}", s.deleteMemory)
	mux.HandleFunc("GET /api/bots/{id}/routines", s.listRoutines)
	mux.HandleFunc("POST /api/bots/{id}/routines", s.createRoutine)
	mux.HandleFunc("PATCH /api/routines/{id}", s.updateRoutine)
	mux.HandleFunc("POST /api/routines/{id}/run", s.runRoutine)
	mux.HandleFunc("DELETE /api/routines/{id}", s.deleteRoutine)
	mux.HandleFunc("GET /api/bots/{id}/grants", s.listGrants)
	mux.HandleFunc("DELETE /api/bots/{id}/grants/{tool}", s.revokeGrant)
	mux.HandleFunc("POST /api/approvals/{id}", s.resolveApproval)
	mux.HandleFunc("POST /api/entries/{id}/reactions", s.setReaction)
	mux.HandleFunc("GET /api/skills", s.listSkills)
	mux.HandleFunc("GET /api/skills/{id}", s.getSkill)
	mux.HandleFunc("DELETE /api/skills/{id}", s.deleteSkill)
	mux.HandleFunc("GET /api/artifacts", s.listArtifacts)
	mux.HandleFunc("POST /api/artifacts", s.createArtifact)
	mux.HandleFunc("GET /api/artifacts/{id}", s.getArtifact)
	mux.HandleFunc("PATCH /api/artifacts/{id}", s.updateArtifact)
	mux.HandleFunc("DELETE /api/artifacts/{id}", s.deleteArtifact)
	mux.HandleFunc("GET /api/connectors", s.listConnectors)
	mux.HandleFunc("POST /api/connectors/{name}/connect", s.connectConnector)
	mux.HandleFunc("DELETE /api/connectors/{name}/token", s.disconnectConnector)
	mux.Handle("GET /oauth/callback", runner.SignInCallback())
	mux.HandleFunc("GET /api/events", s.events)
	mux.Handle("/mcp", runner.MCPHandler())
	mux.Handle("/", spa(ui))
	return guard(mux)
}

// guard rejects requests that could come from a web page on another origin:
// a Host that is not loopback (DNS rebinding) or a foreign Origin (CSRF).
// The API has no other authentication, so this check is the trust boundary.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "YarGPT only answers on a loopback host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		// POST is the only write a page on another origin can send without a CORS
		// preflight, and then only with a form or text body. Other write methods
		// always need a preflight, which YarGPT never grants.
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func spa(ui fs.FS) http.Handler {
	if ui == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "the web UI is not built; run make ui", http.StatusNotFound)
		})
	}
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if _, err := fs.Stat(ui, name); err != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, core.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, core.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, core.ErrApprovalClosed):
		status = http.StatusConflict
	}
	if status == http.StatusInternalServerError {
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	return decodeLimited(r, v, maxBodyBytes)
}

func decodeLimited(r *http.Request, v any, limit int64) error {
	r.Body = http.MaxBytesReader(nil, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("%w: request body: %w", core.ErrInvalid, err)
	}
	return nil
}

type botView struct {
	core.Bot
	Running bool                  `json:"running"`
	Tasks   []core.BackgroundTask `json:"tasks"`
}

func (s *server) view(b core.Bot) botView {
	return botView{Bot: b, Running: s.runner.Running(b.ID), Tasks: s.runner.Tasks(b.ID)}
}

func (s *server) listBots(w http.ResponseWriter, r *http.Request) {
	bots, err := s.svc.ListBots(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]botView, 0, len(bots))
	for _, b := range bots {
		out = append(out, s.view(b))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createBot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string `json:"name"`
		Instructions string `json:"instructions"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	bot, err := s.svc.CreateBot(r.Context(), in.Name, in.Instructions)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.view(bot))
}

// reorderBots sets the order of one sidebar section (all bots, teams or rooms).
func (s *server) reorderBots(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.svc.ReorderBots(r.Context(), in.IDs); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) getBot(w http.ResponseWriter, r *http.Request) {
	bot, err := s.svc.GetBot(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(bot))
}

func (s *server) updateBot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         *string `json:"name"`
		Instructions *string `json:"instructions"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	bot, err := s.svc.UpdateBot(r.Context(), r.PathValue("id"), in.Name, in.Instructions)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(bot))
}

func (s *server) deleteBot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.runner.Interrupt(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for s.runner.Active(id) {
		if time.Now().After(deadline) {
			s.fail(w, r, fmt.Errorf("bot %s did not stop its running turn and processes", id))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := s.svc.DeleteBot(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listEntries(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	entries, err := s.svc.Entries(r.Context(), r.PathValue("id"), after, 1000)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text        string   `json:"text"`
		ClientNonce string   `json:"clientNonce"`
		Media       []string `json:"media"` // base64
	}
	if err := decodeLimited(r, &in, maxMessageBodyBytes); err != nil {
		s.fail(w, r, err)
		return
	}
	media := make([][]byte, len(in.Media))
	for i, data := range in.Media {
		img, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			s.fail(w, r, fmt.Errorf("%w: file %d is not valid base64: %w", core.ErrInvalid, i+1, err))
			return
		}
		media[i] = img
	}
	turnID, dup, err := s.svc.SubmitUserMessage(r.Context(), r.PathValue("id"), in.Text, in.ClientNonce, media...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status := http.StatusAccepted
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"turnId": turnID, "duplicate": dup})
}

func (s *server) media(w http.ResponseWriter, r *http.Request) {
	f, typ, err := s.svc.OpenMedia(r.PathValue("name"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.fail(w, r, fmt.Errorf("read media file %s: %w", r.PathValue("name"), err))
		return
	}
	// The extension tables of mime cover images but not every video type.
	w.Header().Set("Content-Type", typ)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (s *server) interrupt(w http.ResponseWriter, r *http.Request) {
	stopped, err := s.runner.Interrupt(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": stopped})
}

// clearContext starts a fresh Claude context; the transcript stays.
func (s *server) clearContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.runner.Running(id) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the bot is working; wait for it to finish or stop it first"})
		return
	}
	// The process of a session with background tasks would go on in the old context.
	if s.runner.HasSession(id) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "background tasks are running; stop them first"})
		return
	}
	if err := s.svc.ClearContext(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// compactContext queues a compaction; it runs after the bot's current turn.
func (s *server) compactContext(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.QueueCompaction(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"queued": n})
}

func (s *server) createRoom(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string   `json:"name"`
		Instructions string   `json:"instructions"`
		MemberIDs    []string `json:"memberIds"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	room, err := s.svc.CreateRoom(r.Context(), in.Name, in.Instructions, in.MemberIDs)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.view(room))
}

func (s *server) setRoomMembers(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MemberIDs []string `json:"memberIds"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	room, err := s.svc.SetRoomMembers(r.Context(), r.PathValue("id"), in.MemberIDs)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(room))
}

func (s *server) createTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string   `json:"name"`
		Instructions string   `json:"instructions"`
		MemberIDs    []string `json:"memberIds"`
		LeadID       string   `json:"leadId"`
		Agreement    string   `json:"agreement"`
		GoalBudget   int64    `json:"goalBudget"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	team, err := s.svc.CreateTeam(r.Context(), in.Name, in.Instructions, in.MemberIDs, core.TeamSettings{LeadID: in.LeadID, Agreement: in.Agreement, GoalBudget: in.GoalBudget})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.view(team))
}

func (s *server) updateTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LeadID     *string `json:"leadId"`
		Agreement  *string `json:"agreement"`
		GoalBudget *int64  `json:"goalBudget"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	team, err := s.svc.UpdateTeam(r.Context(), r.PathValue("id"), core.TeamPatch{LeadID: in.LeadID, Agreement: in.Agreement, GoalBudget: in.GoalBudget})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.view(team))
}

func (s *server) board(w http.ResponseWriter, r *http.Request) {
	board, err := s.svc.Board(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *server) startGoal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	goal, err := s.svc.StartGoal(r.Context(), r.PathValue("id"), in.Text)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, goal)
}

// extendGoal adds turns to a goal's budget and resumes it when it is paused.
func (s *server) extendGoal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Turns int64 `json:"turns"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	goal, err := s.svc.ExtendGoal(r.Context(), r.PathValue("id"), in.Turns)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, goal)
}

// acceptGoal is the user's acceptance of a delivered goal.
func (s *server) acceptGoal(w http.ResponseWriter, r *http.Request) {
	goal, err := s.svc.AcceptGoal(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, goal)
}

// endGoal closes a goal and stops the turns that work for it, in every team.
func (s *server) endGoal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.svc.EndGoal(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.runner.CancelGoal(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listMemory(w http.ResponseWriter, r *http.Request) {
	facts, err := s.svc.PanelMemories(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, facts)
}

// addMemory saves a fact that the user writes in the app; bots see it as the user's.
func (s *server) addMemory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Scope string `json:"scope"`
		Tier  string `json:"tier"`
		Fact  string `json:"fact"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.svc.AddUserFact(r.Context(), r.PathValue("id"), in.Scope, in.Tier, in.Fact); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteMemory(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listRoutines(w http.ResponseWriter, r *http.Request) {
	routines, err := s.svc.ListRoutines(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, routines)
}

func (s *server) createRoutine(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Prompt   string `json:"prompt"`
		Schedule string `json:"schedule"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	routine, err := s.svc.CreateRoutine(r.Context(), r.PathValue("id"), in.Name, in.Prompt, in.Schedule)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, routine)
}

func (s *server) updateRoutine(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     *string `json:"name"`
		Prompt   *string `json:"prompt"`
		Schedule *string `json:"schedule"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	routine, err := s.svc.UpdateRoutine(r.Context(), r.PathValue("id"), core.RoutinePatch{Name: in.Name, Prompt: in.Prompt, Schedule: in.Schedule, Enabled: in.Enabled}, "")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, routine)
}

func (s *server) runRoutine(w http.ResponseWriter, r *http.Request) {
	turnID, err := s.svc.RunRoutineNow(r.Context(), r.PathValue("id"), "")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"turnId": turnID})
}

func (s *server) deleteRoutine(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteRoutine(r.Context(), r.PathValue("id"), ""); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := s.svc.ToolGrants(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

func (s *server) revokeGrant(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.RevokeToolGrant(r.Context(), r.PathValue("id"), r.PathValue("tool")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) resolveApproval(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.svc.ResolveApproval(r.Context(), r.PathValue("id"), in.Decision); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setReaction puts the user's emoji on a message or takes it off and
// returns the updated entry.
func (s *server) setReaction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Emoji string `json:"emoji"`
		On    bool   `json:"on"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	e, err := s.svc.SetReaction(r.Context(), r.PathValue("id"), in.Emoji, in.On)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *server) listSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := s.svc.ListSkills()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if skills == nil {
		skills = []core.Skill{}
	}
	writeJSON(w, http.StatusOK, skills)
}

func (s *server) getSkill(w http.ResponseWriter, r *http.Request) {
	skill, err := s.svc.GetSkill(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (s *server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteSkill(r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listArtifacts returns the artifacts that match the kind, author, tag, q and
// limit query parameters, without their content.
func (s *server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := core.ArtifactFilter{Kind: q.Get("kind"), AuthorID: q.Get("author"), Tag: q.Get("tag"), Query: q.Get("q")}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			s.fail(w, r, fmt.Errorf("%w: limit must be a number", core.ErrInvalid))
			return
		}
		f.Limit = n
	}
	list, err := s.svc.SearchArtifacts(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// The user creates, changes and deletes artifacts through these handlers,
// so the actor is always the user, who may change any artifact.

func (s *server) createArtifact(w http.ResponseWriter, r *http.Request) {
	var in core.ArtifactInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.svc.CreateArtifact(r.Context(), core.ArtifactAuthorUser, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *server) getArtifact(w http.ResponseWriter, r *http.Request) {
	a, err := s.svc.GetArtifact(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *server) updateArtifact(w http.ResponseWriter, r *http.Request) {
	var in core.ArtifactInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.svc.UpdateArtifact(r.Context(), core.ArtifactAuthorUser, r.PathValue("id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *server) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteArtifact(r.Context(), core.ArtifactAuthorUser, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listConnectors(w http.ResponseWriter, r *http.Request) {
	list, err := s.runner.Connectors()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// connectConnector starts a sign-in and returns the URL for the browser.
func (s *server) connectConnector(w http.ResponseWriter, r *http.Request) {
	authURL, err := s.runner.Connect(r.Context(), r.PathValue("name"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

func (s *server) disconnectConnector(w http.ResponseWriter, r *http.Request) {
	if err := s.runner.Disconnect(r.PathValue("name")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// events streams every change as server-sent events. A client that connects
// (or reconnects) gets a "hello" event first, then a "pause" event if new
// turns wait for a usage limit, and should refetch entries after the highest
// rev it has.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	events, stop := s.svc.Hub().Subscribe()
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	if until := s.runner.PausedUntil(); !until.IsZero() {
		data, err := json.Marshal(core.Event{Type: "pause", PausedUntil: &until})
		if err != nil {
			s.log.Error("encode pause event", "err", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
		case e, open := <-events:
			if !open {
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				s.log.Error("encode event", "err", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		flusher.Flush()
	}
}
