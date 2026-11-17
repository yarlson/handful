package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"handful/internal/move"
)

const (
	envConfigDir = "CLAUDE_CONFIG_DIR"
	// envSecureStorageDir names the Claude Code folder whose login a run uses;
	// set to "", it is ~/.claude. Unset, it follows CLAUDE_CONFIG_DIR, and a
	// bot's own folder holds no login.
	envSecureStorageDir = "CLAUDE_SECURESTORAGE_CONFIG_DIR"
)

// botConfigEnv creates the Claude Code folder of bot botID, copies Claude
// session sessionID, run in work folder dir, into it from the user's Claude
// Code folder once, and returns the variables that run claude on the bot's
// folder with the user's login. env is the server's environment.
func (r *Runner) botConfigEnv(botID, dir, sessionID string, env []string) ([]string, error) {
	config := r.svc.BotConfigDir(botID)
	if err := os.MkdirAll(config, 0o700); err != nil {
		return nil, fmt.Errorf("create the Claude Code folder of bot %s: %w", botID, err)
	}
	if user := userClaudeDir(env); user != "" {
		if err := move.CopyTranscript(user, config, dir, sessionID); err != nil {
			return nil, fmt.Errorf("copy Claude session %s into the Claude Code folder of bot %s: %w", sessionID, botID, err)
		}
	}
	vars := []string{envConfigDir + "=" + config}
	if _, set := envValue(env, envSecureStorageDir); !set {
		userConfig, _ := envValue(env, envConfigDir)
		vars = append(vars, envSecureStorageDir+"="+userConfig)
	}
	return vars, nil
}

// userClaudeDir is the Claude Code folder that the user's own claude uses
// with environment env, or "" when env names none.
func userClaudeDir(env []string) string {
	if dir, _ := envValue(env, envConfigDir); dir != "" {
		return dir
	}
	if home, _ := envValue(env, "HOME"); home != "" {
		return filepath.Join(home, ".claude")
	}
	return ""
}

// envValue returns the value of variable name in env, and whether env sets
// it. Like a process, it takes the last one.
func envValue(env []string, name string) (string, bool) {
	for _, kv := range slices.Backward(env) {
		if value, ok := strings.CutPrefix(kv, name+"="); ok {
			return value, true
		}
	}
	return "", false
}

// authenticationFailed is the error of an assistant event when Claude Code
// has no valid login, for example "Not logged in · Please run /login".
const authenticationFailed = "authentication_failed"

// notLoggedInHint starts the error of a run that failed for want of a login:
// Claude Code's own text asks for /login, which the chat cannot run.
func notLoggedInHint(authFailed bool) string {
	if !authFailed {
		return ""
	}
	return "This bot is not logged in to Claude Code. Run claude in a terminal, log in with /login, then send the message again. Claude Code said: "
}
