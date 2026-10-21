package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Returns where the embedded Tailscale node keeps its state: relay/tsnet
// inside the OS's per-user config directory. That is ~/.config on Linux
// (or XDG_CONFIG_HOME), ~/Library/Application Support on macOS and
// %AppData% on Windows.
func stateDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no user config directory for Tailscale state: %w", err)
	}
	return filepath.Join(dir, "relay", "tsnet"), nil
}
