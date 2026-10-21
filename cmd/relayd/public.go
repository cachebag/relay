package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// errNoAuthKey is returned by publicConfig when --public is set but
// TS_AUTHKEY is empty. main prints it and exits 2.
var errNoAuthKey = errors.New(
	"--public needs a Tailscale auth key: set TS_AUTHKEY to a reusable key from " +
		"https://login.tailscale.com/admin/settings/keys")

// publicConfig is everything the embedded Tailscale node needs.
type publicConfig struct {
	Name    string // hostname on the tailnet, becomes https://<name>.<tailnet>.ts.net
	AuthKey string // from TS_AUTHKEY
	Dir     string // where tsnet keeps its state so a restart keeps the same address
}

// Builds the config for --public from the flag and the environment.
// Returns errNoAuthKey if TS_AUTHKEY is unset or empty.
func newPublicConfig(name, authKey string) (publicConfig, error) {
	if authKey == "" {
		return publicConfig{}, errNoAuthKey
	}
	dir, err := stateDir()
	if err != nil {
		return publicConfig{}, err
	}
	return publicConfig{Name: name, AuthKey: authKey, Dir: dir}, nil
}

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
