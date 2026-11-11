package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"tailscale.com/tsnet"
)

// errNoAuthKey is returned by publicConfig when --public is set but
// neither TS_AUTHKEY nor a saved CLI key is available. main prints it and exits 2.
var errNoAuthKey = errors.New(
	"--public needs a Tailscale auth key: set TS_AUTHKEY to a reusable key from " +
		"https://login.tailscale.com/admin/settings/keys")

// publicConfig is everything the embedded Tailscale node needs.
type publicConfig struct {
	Name    string // hostname on the tailnet, becomes https://<name>.<tailnet>.ts.net
	AuthKey string // from TS_AUTHKEY or the saved CLI settings
	Dir     string // where tsnet keeps its state so a restart keeps the same address
}

// Builds the config for --public from the hostname and resolved key.
// Returns errNoAuthKey if no key is available.
func newPublicConfig(name, authKey, configDir string) (publicConfig, error) {
	if authKey == "" {
		return publicConfig{}, errNoAuthKey
	}
	var dir string
	if configDir != "" {
		dir = filepath.Join(configDir, "tsnet")
	} else {
		var err error
		dir, err = stateDir()
		if err != nil {
			return publicConfig{}, err
		}
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

// Starts the embedded Tailscale node and opens a Funnel listener on :443.
// Returns the listener and the public https URL. This is the only place
// that touches tsnet, so everything else in --public can be tested
// without a tailnet.
func listenPublic(ctx context.Context, cfg publicConfig) (net.Listener, string, error) {
	srv := &tsnet.Server{
		Hostname: cfg.Name,
		AuthKey:  cfg.AuthKey,
		Dir:      cfg.Dir,
	}
	status, err := srv.Up(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("joining the tailnet: %w", err)
	}
	ln, err := srv.ListenFunnel("tcp", ":443")
	if err != nil {
		srv.Close()
		return nil, "", fmt.Errorf("opening a Funnel listener: %w", err)
	}
	// DNSName ends with a dot ("relay.tail1234.ts.net.").
	return ln, "https://" + strings.TrimSuffix(status.Self.DNSName, "."), nil
}
