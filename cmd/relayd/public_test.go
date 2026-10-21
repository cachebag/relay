package main

import (
	"path/filepath"
	"runtime"
	"testing"
)

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
