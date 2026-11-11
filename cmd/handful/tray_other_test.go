//go:build !darwin

package main

import (
	"context"
	"strings"
	"testing"
)

func TestTrayRefusesToRunOutsideMacOS(t *testing.T) {
	served := false
	err := runWithTray(t.Context(), func(context.Context) error { served = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "needs macOS") || served {
		t.Fatalf("runWithTray = %v, served %v; want an error that names macOS and no server", err, served)
	}
}
