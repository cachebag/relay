package main

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

// Verifies that --public without TS_AUTHKEY is refused with the message
// that says what to set.
func TestPublicConfigNeedsAuthKey(t *testing.T) {
	_, err := newPublicConfig("relay", "")
	if !errors.Is(err, errNoAuthKey) {
		t.Fatalf("got %v, want errNoAuthKey", err)
	}
	cfg, err := newPublicConfig("relay", "tskey-auth-test")
	if err != nil {
		t.Fatalf("with a key: %v", err)
	}
	if cfg.Name != "relay" || cfg.AuthKey != "tskey-auth-test" || cfg.Dir == "" {
		t.Errorf("config = %+v, want name, key and a state dir", cfg)
	}
}

// Verifies that the Tailscale state directory is relay/tsnet under the
// per-user config directory for the current OS.
func TestStateDir(t *testing.T) {
	var want string
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
		want = filepath.Join(`C:\Users\test\AppData\Roaming`, "relay", "tsnet")
	case "darwin":
		t.Setenv("HOME", "/Users/test")
		want = "/Users/test/Library/Application Support/relay/tsnet"
	default:
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
		want = "/tmp/xdg/relay/tsnet"
	}
	got, err := stateDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("stateDir() = %q, want %q", got, want)
	}
}
