// Package ui embeds the built web UI (ui/dist) into the YarGPT binary.
package ui

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the built UI. It fails when dist holds no index.html.
func Assets() (fs.FS, error) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded web UI: %w", err)
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, fmt.Errorf("web UI is not built (no dist/index.html): %w", err)
	}
	return sub, nil
}
