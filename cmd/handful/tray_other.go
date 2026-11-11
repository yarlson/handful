//go:build !darwin

package main

import (
	"context"
	"errors"
)

func runWithTray(context.Context, func(context.Context) error) error {
	return errors.New("-tray needs macOS: it shows an icon in the macOS menu bar")
}
