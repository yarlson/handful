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
	// DataDir is Handful's data folder, whose own files the bot's file tools may not read.
	DataDir string
}

// isolatedSettings is the --settings value for every claude run. It turns
// off auto memory and the Co-Authored-By and "Generated with Claude Code"
// lines that Claude Code adds to commits and pull requests, since
// --setting-sources project keeps the user's own attribution setting out.
const isolatedSettings = `{"autoMemoryEnabled":false,"attribution":{"commit":"","pr":""}}`

// handfulSecrets are the files and folders of the data folder that hold Handful's secrets:
// its API key, the MCP configs of running bots with their per-run tokens, connector sign-ins
// and settings, and the database with every copy of it: yargpt.db and its WAL files, the
// move's backup folder, and handful-after-move.db and its stamped copies. Paths are relative
// to the data folder, in Claude Code's gitignore-style patterns; "*" stays in one folder.
var handfulSecrets = []string{"ui-key", "run/**", "oauth/**", "connectors.json", "*.db*", "backup/**"}

// turnSettings is the --settings value of a turn's run: isolatedSettings, plus rules that keep
// Claude Code's file tools (Read, and Grep and Glob, which follow Read's rules) out of
// handfulSecrets in dataDir. Bash, and the programs it runs, can still read them.
func turnSettings(dataDir string) (string, error) {
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
	})
	if err != nil {
		return "", fmt.Errorf("encode the run's settings: %w", err)
	}
	return string(raw), nil
}

// AutocompactWindow is the context size, in tokens, at which Claude Code compacts a session's
// conversation (--autocompact). Without it, Claude Code waits until the model's window is nearly full.
const AutocompactWindow = 200000

// args builds the claude CLI arguments; the prompt goes to stdin as a
// userMessage. --setting-sources project keeps the
// user's personal settings and hooks out of the bot (startSession keeps
// CLAUDE.md files out with an environment variable), and
// --strict-mcp-config keeps out every MCP server that is not in the
// MCPConfig file.
func (r claudeRun) args() ([]string, error) {
	agents, err := json.Marshal(executorAgents(r.BotName))
	if err != nil {
		return nil, fmt.Errorf("encode agents: %w", err)
	}
	settings, err := turnSettings(r.DataDir)
	if err != nil {
		return nil, err
	}
	allowed := append([]string{}, readOnlyTools...)
	for _, t := range []string{"send_to_user", "send_to_agent", "read_room", "recall_memory", "update_memory", "update_routine", "update_skill", "find_artifacts", "read_artifact", "update_artifact", "update_profile", "update_board", core.ToolCreateSetup} {
		allowed = append(allowed, handfulTool(t))
	}

	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--setting-sources", "project",
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

// compactArgs starts claude on a stored session to run Claude Code's /compact,
// which goes to stdin. The isolation flags match a normal turn; no tools or
// MCP servers are needed.
func compactArgs(sessionID, model, effort string) []string {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--setting-sources", "project",
		"--strict-mcp-config",
		"--settings", isolatedSettings,
		"--resume", sessionID,
	}
	return appendModelEffort(args, model, effort)
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
	Usage           *tokenUsage      `json:"usage"` // result: the turn's tokens
	Result          string           `json:"result"`
	IsError         bool             `json:"is_error"`
	Errors          json.RawMessage  `json:"errors"`
	Error           string           `json:"error"`            // assistant: kind of a failure, such as model_not_found
	APIError        string           `json:"api_error"`        // assistant, result: kind of an API error, such as usage_limit_reached
	APIErrorParams  *apiErrorParams  `json:"api_error_params"` // assistant
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
