// Package shellenv reads the environment of the user's login shell. launchd
// starts a process with only a minimal PATH, so without it neither Claude Code
// nor the tools that bots run are found.
package shellenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// marker separates what the shell's startup files print from the environment.
const marker = "__YARGPT_ENV__"

// Load runs shell as an interactive login shell and returns the environment
// that it exports.
func Load(ctx context.Context, shell string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, shell, "-ilc", "printf '%s\\0' "+marker+"; env -0")
	// A startup file can leave a process that keeps stdout open after the
	// shell is killed; stop waiting for it.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run login shell %s: %w", shell, err)
	}
	return parse(out)
}

// parse reads the NUL-separated NAME=value pairs that follow the marker.
func parse(out []byte) (map[string]string, error) {
	_, rest, found := bytes.Cut(out, []byte(marker+"\x00"))
	if !found {
		return nil, errors.New("the login shell printed no environment")
	}
	env := map[string]string{}
	for pair := range bytes.SplitSeq(rest, []byte{0}) {
		name, value, ok := bytes.Cut(pair, []byte("="))
		if !ok || len(name) == 0 {
			continue
		}
		env[string(name)] = string(value)
	}
	return env, nil
}
