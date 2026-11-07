package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yargpt/internal/core"
)

// runContext identifies the bot and turn that a worker's MCP calls belong to.
type runContext struct {
	BotID   string
	BotName string
	TurnID  string
	RoomID  string // set when the bot takes a turn in a room
	Hops    int64
}

// transcript is where the run's messages and approval cards appear.
func (rc runContext) transcript() string {
	if rc.RoomID != "" {
		return rc.RoomID
	}
	return rc.BotID
}

// tokenRegistry maps each running worker's bearer token to its run.
type tokenRegistry struct {
	mu sync.Mutex
	m  map[string]runContext
}

func newTokenRegistry() *tokenRegistry { return &tokenRegistry{m: make(map[string]runContext)} }

func (t *tokenRegistry) issue(rc runContext) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate MCP token: %w", err)
	}
	token := hex.EncodeToString(buf)
	t.mu.Lock()
	t.m[token] = rc
	t.mu.Unlock()
	return token, nil
}

// set points a live token at another run: a session's process keeps one
// token while it runs several turns.
func (t *tokenRegistry) set(token string, rc runContext) {
	t.mu.Lock()
	t.m[token] = rc
	t.mu.Unlock()
}

func (t *tokenRegistry) revoke(token string) {
	t.mu.Lock()
	delete(t.m, token)
	t.mu.Unlock()
}

func (t *tokenRegistry) get(token string) (runContext, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rc, ok := t.m[token]
	return rc, ok
}

func (t *tokenRegistry) lookup(r *http.Request) (runContext, bool) {
	token, ok := bearerToken(r)
	if !ok {
		return runContext{}, false
	}
	return t.get(token)
}

func bearerToken(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token, ok && token != ""
}

// MCPHandler serves YarGPT's tools to running workers. Every request must carry
// the bearer token of a live run; the tools act on that run's bot.
func (r *Runner) MCPHandler() http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		token, ok := bearerToken(req)
		if _, live := r.tokens.get(token); !ok || !live {
			return nil
		}
		return newToolServer(r.svc, func() (runContext, bool) { return r.tokens.get(token) })
	}, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, ok := r.tokens.lookup(req); !ok {
			http.Error(w, "unknown or expired run token", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, req)
	})
}

func textResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}}}
}

type sendInput struct {
	Text       string   `json:"text,omitempty" jsonschema:"the message for the user, in markdown; it can be empty when files are attached"`
	Files      []string `json:"files,omitempty" jsonschema:"paths of images (PNG, JPEG, GIF, WebP) or videos (MP4, WebM, QuickTime) to show with the message, at most 4; a relative path is in your working directory; a file must be in your working directory, the media folder or a temp folder such as /tmp"`
	NeedsReply bool     `json:"needs_reply,omitempty" jsonschema:"true when the message asks the user a question or for a decision and you cannot continue without the answer; the app then shows that you are waiting for the user"`
}

type agentMessageInput struct {
	TargetID string   `json:"target_id" jsonschema:"id of a teammate bot, or of a room you are in"`
	Message  string   `json:"message" jsonschema:"the message, self-contained and to the point"`
	Files    []string `json:"files,omitempty" jsonschema:"room posts only: paths of images or videos to attach, at most 4, as for send_to_user"`
	Priority bool     `json:"priority" jsonschema:"true wakes a teammate now (they must act or answer); false delivers it at their next turn (updates, FYIs). Room posts always land at once."`
}

type readRoomInput struct {
	RoomID    string `json:"room_id" jsonschema:"id of a room you are in"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many of the latest messages to return, 1 to 50 (default 20)"`
	BeforeSeq int64  `json:"before_seq,omitempty" jsonschema:"return only messages older than this #seq, to page back from the oldest line you have (default: the newest messages)"`
}

type recallInput struct {
	Query string `json:"query" jsonschema:"words to look for; facts sharing more words rank first"`
	Scope string `json:"scope,omitempty" jsonschema:"agent, user or all (default all)"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum facts to return, 1 to 50 (default 20)"`
}

type memoryInput struct {
	Action   string `json:"action" jsonschema:"write or forget"`
	Fact     string `json:"fact" jsonschema:"one self-contained sentence; for forget, the exact recorded text"`
	Scope    string `json:"scope,omitempty" jsonschema:"agent (default, your own memory) or user (shared with all the user's bots)"`
	Tier     string `json:"tier,omitempty" jsonschema:"write only: profile (always in mind, keep small), log (default, dated history) or note (fades after a week)"`
	Replaces string `json:"replaces,omitempty" jsonschema:"write only: the exact text of a fact in the same scope that the new fact replaces; it is removed in the same step"`
}

type routineInput struct {
	Action   string `json:"action" jsonschema:"create, update, pause, resume, delete, run_now or list"`
	ID       string `json:"id,omitempty" jsonschema:"routine id; required except for create and list"`
	Name     string `json:"name,omitempty" jsonschema:"short name; required on create"`
	Prompt   string `json:"prompt,omitempty" jsonschema:"what to do each time it fires, written as an intent for your future self; required on create"`
	Schedule string `json:"schedule,omitempty" jsonschema:"5-field cron in the user's time zone, or @hourly/@daily/@weekly/@every 2h; required on create"`
}

type skillInput struct {
	Action      string `json:"action" jsonschema:"write, delete or list"`
	Name        string `json:"name,omitempty" jsonschema:"skill name; its id is made from the name; required for write"`
	ID          string `json:"id,omitempty" jsonschema:"skill id; required for delete"`
	Description string `json:"description,omitempty" jsonschema:"one line that says when to use the skill; required for write"`
	Body        string `json:"body,omitempty" jsonschema:"the generic recipe in markdown; required for write"`
}

type profileInput struct {
	Name         *string `json:"name,omitempty" jsonschema:"your new display name"`
	Instructions *string `json:"instructions,omitempty" jsonschema:"your new standing instructions"`
}

// addRunTool adds a tool whose handler acts on the token's current run. A
// session's claude process keeps one MCP session while it runs several turns,
// so the run is looked up on every call.
func addRunTool[In any](s *mcp.Server, current func() (runContext, bool), tool *mcp.Tool, handle func(context.Context, runContext, In) (*mcp.CallToolResult, any, error)) {
	mcp.AddTool(s, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		rc, ok := current()
		if !ok {
			return nil, nil, errors.New("the run of this token has ended")
		}
		return handle(ctx, rc, in)
	})
}

func newToolServer(svc *core.Service, current func() (runContext, bool)) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: mcpServerName, Version: "1"}, nil)

	addRunTool(s, current, &mcp.Tool{Name: "send_to_user", Description: "Send a message to the user, with images or videos if you attach files. This is the only output the user sees. During a turn in a room, it posts into the room for everyone."},
		func(ctx context.Context, rc runContext, in sendInput) (*mcp.CallToolResult, any, error) {
			if rc.RoomID != "" {
				woken, err := svc.PostToRoom(ctx, rc.RoomID, rc.BotID, rc.TurnID, in.Text, in.Files, rc.Hops, in.NeedsReply)
				if err != nil {
					return nil, nil, err
				}
				room, err := svc.GetBot(ctx, rc.RoomID)
				if err != nil {
					return nil, nil, err
				}
				posted := fmt.Sprintf("Posted in room %q, seen by its %d members and the user.", room.Name, len(room.Members))
				if len(woken) > 0 {
					return textResult("%s Woke %s; their replies come in their own turns.", posted, strings.Join(woken, ", ")), nil, nil
				}
				return textResult("%s", posted), nil, nil
			}
			if _, err := svc.AppendBotMessage(ctx, rc.BotID, rc.TurnID, in.Text, in.Files, false, in.NeedsReply); err != nil {
				return nil, nil, err
			}
			return textResult("Delivered."), nil, nil
		})

	addRunTool(s, current, &mcp.Tool{Name: "send_to_agent", Description: fmt.Sprintf("Message one of the user's other bots, or post into a room you are in; at most %d characters. Asynchronous like texting: the reply, if any, arrives later as its own turn, so do not wait or poll for it. To ask a teammate to act, message them directly. A room post wakes only the members it names with @Name; use that only when the room should see the request.", core.MaxAgentMessage)},
		func(ctx context.Context, rc runContext, in agentMessageInput) (*mcp.CallToolResult, any, error) {
			res, err := svc.SendAgentMessage(ctx, rc.BotID, in.TargetID, in.Message, in.Files, in.Priority, rc.Hops)
			if err != nil {
				return nil, nil, err
			}
			switch res.Delivered {
			case "woke":
				return textResult("Sent; the recipient is reading it now. Their reply, if any, arrives later as its own turn."), nil, nil
			case "held":
				return textResult("Sent; the recipient reads it at the start of their next turn."), nil, nil
			}
			if len(res.Woken) > 0 {
				return textResult("Posted in the room. Woke %s.", strings.Join(res.Woken, ", ")), nil, nil
			}
			return textResult("Posted in the room; no one was woken. To ask a teammate to act, message them directly."), nil, nil
		})

	addRunTool(s, current, &mcp.Tool{Name: "read_room", Description: "Read the latest messages of a room you are in, oldest first, with the files they carry. Each line starts with the message's #seq; pass the oldest one as before_seq to read further back. Reading does not change what your next room turn shows as new."},
		func(ctx context.Context, rc runContext, in readRoomInput) (*mcp.CallToolResult, any, error) {
			limit := in.Limit
			if limit == 0 {
				limit = 20
			}
			entries, err := svc.RoomHistory(ctx, in.RoomID, rc.BotID, limit, in.BeforeSeq)
			if err != nil {
				return nil, nil, err
			}
			room, err := svc.GetBot(ctx, in.RoomID)
			if err != nil {
				return nil, nil, err
			}
			if len(entries) == 0 {
				return textResult("The room has no messages yet."), nil, nil
			}
			var b strings.Builder
			for _, e := range entries {
				fmt.Fprintf(&b, "#%d [%s] %s\n", e.Seq, time.UnixMilli(e.CreatedAt).In(svc.Location()).Format("Mon Jan 2 15:04"), roomLine(e, room.Members, svc.MediaDir()))
			}
			return textResult("%s", b.String()), nil, nil
		})

	addRunTool(s, current, &mcp.Tool{Name: "recall_memory", Description: "Search your durable memory and the shared user memory for facts that are not in your prompt."},
		func(ctx context.Context, rc runContext, in recallInput) (*mcp.CallToolResult, any, error) {
			facts, err := svc.Recall(ctx, rc.BotID, in.Query, in.Scope, in.Limit)
			if err != nil {
				return nil, nil, err
			}
			if len(facts) == 0 {
				return textResult("No facts match %q.", in.Query), nil, nil
			}
			bots, err := svc.ListBots(ctx)
			if err != nil {
				return nil, nil, err
			}
			authors := factAuthors(rc.BotID, bots)
			var b strings.Builder
			for _, f := range facts {
				fmt.Fprintf(&b, "%s\n", formatFact(f, svc.Location(), authors))
			}
			return textResult("%s", b.String()), nil, nil
		})

	addRunTool(s, current, &mcp.Tool{Name: "update_memory", Description: fmt.Sprintf("Save a durable fact (write), replace one (write with replaces) or drop one by its exact text (forget). A fact is one sentence of at most %d characters; keep long lists in a file in your working directory. Save without asking and mention it only in passing.", core.MaxFactLen)},
		func(ctx context.Context, rc runContext, in memoryInput) (*mcp.CallToolResult, any, error) {
			switch in.Action {
			case "write":
				if in.Replaces != "" {
					if err := svc.ReplaceFact(ctx, rc.BotID, in.Scope, in.Tier, in.Replaces, in.Fact); err != nil {
						return nil, nil, err
					}
					return textResult("Replaced."), nil, nil
				}
				created, err := svc.WriteFact(ctx, rc.BotID, in.Scope, in.Tier, in.Fact)
				if err != nil {
					return nil, nil, err
				}
				if !created {
					return textResult("Already remembered."), nil, nil
				}
				return textResult("Saved."), nil, nil
			case "forget":
				found, err := svc.ForgetFact(ctx, rc.BotID, in.Scope, in.Fact)
				if err != nil {
					return nil, nil, err
				}
				if !found {
					return textResult("No fact with that exact text in scope %q. Use recall_memory to find it.", in.Scope), nil, nil
				}
				return textResult("Forgotten."), nil, nil
			}
			return nil, nil, fmt.Errorf("action must be write or forget")
		})

	addRunTool(s, current, &mcp.Tool{Name: "update_routine", Description: "Create, change, pause, resume, delete, run or list your routines: saved prompts that run on a schedule, even while the user is away."},
		func(ctx context.Context, rc runContext, in routineInput) (*mcp.CallToolResult, any, error) {
			return routineAction(ctx, svc, rc, in)
		})

	addRunTool(s, current, &mcp.Tool{Name: "update_skill", Description: "Save, rewrite, delete or list reusable skills (SKILL.md recipes) shared by all of the user's bots. A skill has no trigger; use a routine for scheduled work."},
		func(ctx context.Context, rc runContext, in skillInput) (*mcp.CallToolResult, any, error) {
			switch in.Action {
			case "write":
				sk, err := svc.WriteSkill(in.Name, in.Description, in.Body)
				if err != nil {
					return nil, nil, err
				}
				return textResult("Saved skill %q (id %s). It is available through the Skill tool from the next turn.", sk.Name, sk.ID), nil, nil
			case "delete":
				if err := svc.DeleteSkill(in.ID); err != nil {
					return nil, nil, err
				}
				return textResult("Deleted skill %s.", in.ID), nil, nil
			case "list":
				skills, err := svc.ListSkills()
				if err != nil {
					return nil, nil, err
				}
				if len(skills) == 0 {
					return textResult("No skills yet."), nil, nil
				}
				var b strings.Builder
				for _, sk := range skills {
					fmt.Fprintf(&b, "- %s: %s\n", sk.ID, sk.Description)
				}
				return textResult("%s", b.String()), nil, nil
			}
			return nil, nil, fmt.Errorf("action must be write, delete or list")
		})

	addRunTool(s, current, &mcp.Tool{Name: "update_profile", Description: "Change your display name and/or standing instructions. Fields you omit stay the same."},
		func(ctx context.Context, rc runContext, in profileInput) (*mcp.CallToolResult, any, error) {
			bot, err := svc.UpdateBot(ctx, rc.BotID, in.Name, in.Instructions)
			if err != nil {
				return nil, nil, err
			}
			return textResult("Profile updated: name %q.", bot.Name), nil, nil
		})

	// approve answers Claude Code's permission prompts (--permission-prompt-tool).
	// Its input and output shapes are fixed by Claude Code.
	addRunTool(s, current, &mcp.Tool{Name: "approve", Description: "Internal: asks the user to approve a tool call."},
		func(ctx context.Context, rc runContext, in map[string]any) (*mcp.CallToolResult, any, error) {
			tool, _ := in["tool_name"].(string)
			input, err := json.Marshal(in["input"])
			if err != nil {
				return nil, nil, fmt.Errorf("encode tool input: %w", err)
			}
			allowed, reason, err := svc.RequestApproval(ctx, core.ApprovalRequest{BotID: rc.BotID, BotName: rc.BotName, TranscriptID: rc.transcript(), TurnID: rc.TurnID, Tool: tool, Input: input})
			if err != nil {
				return nil, nil, err
			}
			var answer any
			if allowed {
				answer = map[string]any{"behavior": "allow", "updatedInput": in["input"]}
			} else {
				answer = map[string]any{"behavior": "deny", "message": reason}
			}
			out, err := json.Marshal(answer)
			if err != nil {
				return nil, nil, fmt.Errorf("encode approval answer: %w", err)
			}
			return textResult("%s", out), nil, nil
		})
	return s
}

func routineAction(ctx context.Context, svc *core.Service, rc runContext, in routineInput) (*mcp.CallToolResult, any, error) {
	owned := func() (core.Routine, error) {
		r, err := svc.GetRoutine(ctx, in.ID)
		if err != nil {
			return core.Routine{}, err
		}
		if r.BotID != rc.BotID {
			return core.Routine{}, fmt.Errorf("routine %s belongs to another bot", in.ID)
		}
		return r, nil
	}
	describe := func(r core.Routine) string {
		next := "paused"
		if r.Enabled && r.NextRunAt != nil {
			next = "next run " + time.UnixMilli(*r.NextRunAt).In(svc.Location()).Format("Mon Jan 2 15:04 MST")
		}
		if last := lastRun(r, svc.Location()); last != "" {
			next += ", " + last
		}
		return fmt.Sprintf("%q (id %s, schedule %q, %s)", r.Name, r.ID, r.Schedule, next)
	}
	setEnabled := func(v bool) (*mcp.CallToolResult, any, error) {
		if _, err := owned(); err != nil {
			return nil, nil, err
		}
		r, err := svc.UpdateRoutine(ctx, in.ID, core.RoutinePatch{Enabled: &v}, rc.BotName)
		if err != nil {
			return nil, nil, err
		}
		return textResult("Routine %s.", describe(r)), nil, nil
	}
	switch in.Action {
	case "create":
		r, err := svc.CreateRoutine(ctx, rc.BotID, in.Name, in.Prompt, in.Schedule)
		if err != nil {
			return nil, nil, err
		}
		return textResult("Created routine %s.", describe(r)), nil, nil
	case "update":
		if _, err := owned(); err != nil {
			return nil, nil, err
		}
		var p core.RoutinePatch
		if in.Name != "" {
			p.Name = &in.Name
		}
		if in.Prompt != "" {
			p.Prompt = &in.Prompt
		}
		if in.Schedule != "" {
			p.Schedule = &in.Schedule
		}
		r, err := svc.UpdateRoutine(ctx, in.ID, p, rc.BotName)
		if err != nil {
			return nil, nil, err
		}
		return textResult("Updated routine %s.", describe(r)), nil, nil
	case "pause":
		return setEnabled(false)
	case "resume":
		return setEnabled(true)
	case "delete":
		if _, err := owned(); err != nil {
			return nil, nil, err
		}
		if err := svc.DeleteRoutine(ctx, in.ID, rc.BotName); err != nil {
			return nil, nil, err
		}
		return textResult("Deleted routine %s.", in.ID), nil, nil
	case "run_now":
		if _, err := owned(); err != nil {
			return nil, nil, err
		}
		if _, err := svc.RunRoutineNow(ctx, in.ID, rc.BotName); err != nil {
			return nil, nil, err
		}
		return textResult("Queued a run of routine %s; it starts after this turn.", in.ID), nil, nil
	case "list":
		routines, err := svc.ListRoutines(ctx, rc.BotID)
		if err != nil {
			return nil, nil, err
		}
		if len(routines) == 0 {
			return textResult("No routines."), nil, nil
		}
		var b strings.Builder
		for _, r := range routines {
			fmt.Fprintf(&b, "- %s\n  prompt: %s\n", describe(r), r.Prompt)
		}
		return textResult("%s", b.String()), nil, nil
	}
	return nil, nil, fmt.Errorf("action must be create, update, pause, resume, delete, run_now or list")
}
