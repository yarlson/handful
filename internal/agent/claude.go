package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"handful/internal/core"
)

// builtinTools is the Claude Code tool set a bot can use. Anything outside
// readOnlyTools goes through the approval tool before it runs; the approval
// tool allows the tools that the user always allows. TaskStop only stops the
// bot's own background tasks.
var (
	builtinTools  = []string{"Task", "Bash", "Monitor", "TaskStop", "Read", "Edit", "Write", "Glob", "Grep", "WebSearch", "Skill"}
	readOnlyTools = []string{"Task", "TaskStop", "Read", "Glob", "Grep", "WebSearch", "Skill"}
)

const (
	mcpServerName = "handful"
	// oldMCPServerName was the server's name before the rename; it stays
	// reserved, so old notes' tool names never reach a connector.
	oldMCPServerName = "yargpt"
	approveTool      = "mcp__handful__approve"
)

func handfulTool(name string) string { return "mcp__" + mcpServerName + "__" + name }

type claudeRun struct {
	Prompt string
	// Command is the user's skill command, which goes after Prompt as the message's last text
	// block, so that Claude Code runs the skill itself (skillCommand); "" for none.
	Command      string
	SystemPrompt string // goes in the SystemPromptFile file, never in argv
	// SystemPromptFile is the path of the file that holds SystemPrompt.
	// Argv is visible to ps and matched by pkill -f, so text from a bot's
	// memory there would let a pattern kill stop the bot.
	SystemPromptFile string
	SessionID        string
	Resume           bool
	MCPConfig        string // path of the file that writeMCPConfig wrote
	BotName          string
	Flags            runFlags // the model and effort
	HelperModel      string   // Config.HelperModel
	// DataDir is Handful's data folder, whose own files the bot's file tools may not read.
	DataDir string
}

// handfulSecrets are the files and folders of the data folder that hold Handful's secrets:
// its API key, the MCP configs of running bots with their per-run tokens, connector sign-ins
// and settings, and the database with every copy of it: yargpt.db and its WAL files, the
// move's backup folder, and handful-after-move.db and its stamped copies. Paths are relative
// to the data folder, in Claude Code's gitignore-style patterns; "*" stays in one folder.
var handfulSecrets = []string{"ui-key", "run/**", "oauth/**", "connectors.json", "*.db*", "backup/**"}

// turnSettings is the --settings value of every claude run. It turns off auto memory and the
// Co-Authored-By and "Generated with Claude Code" lines that Claude Code adds to commits and
// pull requests, since the bot's own Claude Code folder keeps the user's own attribution
// setting out. Its rules keep Claude Code's file tools (Read, and Grep and Glob, which follow
// Read's rules) out of handfulSecrets in dataDir; Bash, and the programs it runs, can still
// read them. claudeMdExcludes keeps out the instruction files above dataDir (outsideInstructions).
func turnSettings(dataDir string) (string, error) {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("find the absolute path of the data folder %s: %w", dataDir, err)
	}
	excludes := outsideInstructions(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
		excludes = append(excludes, outsideInstructions(real)...)
	}
	// Claude Code checks a rule against a file's real path, so the folder's real path goes in.
	if real, err := filepath.EvalSymlinks(dataDir); err == nil {
		dataDir = real
	}
	deny := make([]string, 0, len(handfulSecrets))
	for _, p := range handfulSecrets {
		// "//" starts an absolute path in a rule.
		deny = append(deny, "Read(/"+filepath.ToSlash(filepath.Join(dataDir, p))+")")
	}
	raw, err := json.Marshal(map[string]any{
		"autoMemoryEnabled": false,
		"attribution":       map[string]string{"commit": "", "pr": ""},
		"permissions":       map[string][]string{"deny": deny},
		"claudeMdExcludes":  excludes,
	})
	if err != nil {
		return "", fmt.Errorf("encode the run's settings: %w", err)
	}
	return string(raw), nil
}

// outsideFiles are the instruction files that Claude Code loads from each folder above the
// working folder, relative to that folder.
var outsideFiles = []string{"CLAUDE.md", "CLAUDE.local.md", "AGENTS.md", ".claude/CLAUDE.md", ".claude/AGENTS.md", ".claude/rules/**"}

// outsideInstructions returns claudeMdExcludes patterns for the instruction files in every
// folder above dataDir, an absolute path. These folders are above every working folder of a
// bot too, and hold the user's own files, such as ~/CLAUDE.md and ~/.claude/CLAUDE.md, which
// Claude Code would otherwise load into every bot.
func outsideInstructions(dataDir string) []string {
	var patterns []string
	for dir := filepath.Dir(dataDir); ; dir = filepath.Dir(dir) {
		for _, name := range outsideFiles {
			patterns = append(patterns, strings.TrimSuffix(literalGlob(filepath.ToSlash(dir)), "/")+"/"+name)
		}
		if dir == filepath.Dir(dir) {
			return patterns
		}
	}
}

// literalGlob puts each character that Claude Code's glob matcher reads as special in a
// character class of its own, so a folder named "a (2)" matches only itself. A backslash
// can't escape them: Claude Code turns every backslash in a pattern into "/".
func literalGlob(path string) string {
	var b strings.Builder
	for _, c := range path {
		if strings.ContainsRune("*?[{}()+@|$", c) {
			b.WriteString("[" + string(c) + "]")
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// AutocompactWindow is the context size, in tokens, at which Claude Code compacts a session's
// conversation (--autocompact). Without it, Claude Code waits until the model's window is nearly full.
const AutocompactWindow = 200000

// args builds the claude CLI arguments; the prompt goes to stdin as a
// userMessage. --setting-sources user,project loads the settings of the
// bot's own Claude Code folder, which startSession sets, so the user's
// personal settings and hooks stay out, and
// --strict-mcp-config keeps out every MCP server that is not in the
// MCPConfig file.
func (r claudeRun) args() ([]string, error) {
	agents, err := json.Marshal(executorAgents(r.BotName, r.HelperModel))
	if err != nil {
		return nil, fmt.Errorf("encode agents: %w", err)
	}
	settings, err := turnSettings(r.DataDir)
	if err != nil {
		return nil, err
	}
	allowed := append([]string{}, readOnlyTools...)
	for _, t := range []string{"send_to_user", "send_to_agent", "read_room", "recall_memory", "update_memory", "update_routine", "update_skill", "find_artifacts", "read_artifact", "update_artifact", "update_profile", "update_board", "list_members", core.ToolCreateSetup} {
		allowed = append(allowed, handfulTool(t))
	}

	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--setting-sources", "user,project",
		"--strict-mcp-config",
		"--mcp-config", r.MCPConfig,
		"--settings", settings,
		"--system-prompt-snapshot", "on",
		"--autocompact", strconv.Itoa(AutocompactWindow),
		"--append-system-prompt-file", r.SystemPromptFile,
		"--agents", string(agents),
		"--tools", strings.Join(builtinTools, ","),
		"--allowedTools", strings.Join(allowed, ","),
		"--permission-prompt-tool", approveTool,
	}
	args = appendModelEffort(args, r.Flags.model, r.Flags.effort)
	if r.Resume {
		args = append(args, "--resume", r.SessionID)
	} else {
		args = append(args, "--session-id", r.SessionID)
	}
	return args, nil
}

// userMessage encodes a prompt as a stream-json input line, with command, a skill command, as
// its last text block when it is set (skillCommand). Claude Code
// reports the message's progress in command_lifecycle events with this id.
func userMessage(id, text, command string) ([]byte, error) {
	var content any = text
	if command != "" {
		content = []map[string]string{{"type": "text", "text": text}, {"type": "text", "text": command}}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	err := enc.Encode(map[string]any{
		"type":               "user",
		"uuid":               id,
		"session_id":         "",
		"parent_tool_use_id": nil,
		"message":            map[string]any{"role": "user", "content": content},
	})
	if err != nil {
		return nil, fmt.Errorf("encode prompt: %w", err)
	}
	return b.Bytes(), nil
}

// interruptMessage asks Claude Code to stop the model's current run; id names the request.
// The process and its background tasks go on. With cancelQueued it also drops the messages still queued
// behind that run, each with a "cancelled" lifecycle event.
func interruptMessage(id string, cancelQueued bool) []byte {
	return fmt.Appendf(nil, `{"type":"control_request","request_id":"stop-%s","request":{"subtype":"interrupt","cancel_queued":%t}}`+"\n", id, cancelQueued)
}

// compactArgs starts claude on a stored session to run Claude Code's /compact,
// which goes to stdin. The isolation flags match a normal turn; no tools or
// MCP servers are needed.
func compactArgs(sessionID, model, effort, dataDir string) ([]string, error) {
	settings, err := turnSettings(dataDir)
	if err != nil {
		return nil, err
	}
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--setting-sources", "project",
		"--strict-mcp-config",
		"--settings", settings,
		"--resume", sessionID,
	}
	return appendModelEffort(args, model, effort), nil
}

// appendModelEffort adds --model and --effort for the values that are set.
func appendModelEffort(args []string, model, effort string) []string {
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return args
}

type compactMetadata struct {
	PreTokens  int64 `json:"pre_tokens"`
	PostTokens int64 `json:"post_tokens"`
}

// streamEvent is the subset of Claude Code's stream-json output that Handful reads.
type streamEvent struct {
	CompactMetadata *compactMetadata `json:"compact_metadata"`
	Type            string           `json:"type"`
	Model           string           `json:"model"` // system/init: the full ID of the model Claude Code runs
	Subtype         string           `json:"subtype"`
	CommandUUID     string           `json:"command_uuid"` // command_lifecycle: the message's uuid
	State           string           `json:"state"`        // command_lifecycle: queued, started, completed or cancelled
	Tasks           []streamTask     `json:"tasks"`        // background_tasks_changed: all running tasks
	TaskID          string           `json:"task_id"`      // task_notification
	ToolUseID       string           `json:"tool_use_id"`  // task_notification: the call that started the task
	Summary         string           `json:"summary"`      // task_notification
	Status          string           `json:"status"`       // task_notification: completed, failed or stopped
	SessionID       string           `json:"session_id"`
	ParentToolUseID *string          `json:"parent_tool_use_id"`
	Message         *streamMessage   `json:"message"`
	ToolUseResult   json.RawMessage  `json:"tool_use_result"` // user: the result of the event's tool call, in the tool's own shape
	Usage           *tokenUsage      `json:"usage"`           // result: the turn's tokens
	// result: each model's tokens and cost since the process's previous result. Unlike
	// total_cost_usd and modelUsage, which run on across the session's turns and its resumes.
	ModelUsageSinceLastResult map[string]modelUsage `json:"model_usage_since_last_result"`
	SubagentStats             *subagentStats        `json:"subagent_stats"` // result
	Result                    string                `json:"result"`
	IsError                   bool                  `json:"is_error"`
	Errors                    json.RawMessage       `json:"errors"`
	Error                     string                `json:"error"`            // assistant: kind of a failure, such as model_not_found
	APIError                  string                `json:"api_error"`        // assistant, result: kind of an API error, such as usage_limit_reached
	APIErrorParams            *apiErrorParams       `json:"api_error_params"` // assistant
	RateLimitInfo             *rateLimitInfo        `json:"rate_limit_info"`  // rate_limit_event: the plan's usage windows
}

type apiErrorParams struct {
	RateLimitInfo *struct {
		ResetsAt int64 `json:"resetsAt"` // Unix seconds
	} `json:"rate_limit_info"`
}

type streamTask struct {
	TaskID      string `json:"task_id"`
	Description string `json:"description"`
}

type streamMessage struct {
	ID      string         `json:"id"`    // assistant: one per model call; a call's blocks come in several events
	Usage   *tokenUsage    `json:"usage"` // assistant: the call's tokens
	Content []contentBlock `json:"content"`
}

// tokenUsage is the tokens of a model call, or of a turn's calls in a result event. The call's
// context is all three input counts.
type tokenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

// modelUsage is the tokens and the cost of the calls to one model, helpers' calls included.
type modelUsage struct {
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
}

type subagentStats struct {
	Spawned int64 `json:"spawned"` // the helpers the process has started, since it started
}

func (u tokenUsage) context() int64 {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// UnmarshalJSON accepts message content as a string or as a list of blocks.
func (m *streamMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID      string          `json:"id"`
		Usage   *tokenUsage     `json:"usage"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.ID, m.Usage = raw.ID, raw.Usage
	if len(raw.Content) == 0 || raw.Content[0] == '"' {
		return nil
	}
	return json.Unmarshal(raw.Content, &m.Content)
}

// toolResultText flattens a tool_result content value to text.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// summarizeToolUse gives a one-line description of a tool call for the activity row.
func summarizeToolUse(name string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	str := func(k string) string { v, _ := in[k].(string); return v }
	switch name {
	case "Bash":
		if d := str("description"); d != "" {
			return truncate(d+": "+str("command"), 200)
		}
		return truncate(str("command"), 200)
	case "Read", "Write", "Edit":
		return str("file_path")
	case "Glob", "Grep":
		return truncate(str("pattern"), 200)
	case "WebSearch":
		return truncate(str("query"), 200)
	case "WebFetch":
		return truncate(str("url"), 200)
	case "Task", "Agent":
		return truncate(strings.TrimSpace(str("subagent_type")+": "+str("description")), 200)
	case "Skill":
		return str("skill")
	}
	return truncate(string(input), 200)
}
