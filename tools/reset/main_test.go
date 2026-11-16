package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// machine lays out a config and cache folder with everything relay leaves
// behind, and returns their paths.
func machine(t *testing.T) (config, cache string) {
	t.Helper()
	root := t.TempDir()
	config = filepath.Join(root, "config", "relay")
	cache = filepath.Join(root, "cache", "relay")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(config, "bin", "relayd"), "binary")
	write(filepath.Join(config, "tsnet", "tailscaled.state"), "identity")
	write(filepath.Join(config, "relay.db"), "pools")
	write(filepath.Join(config, "relay.db-wal"), "wal")
	write(filepath.Join(config, "relayd.log"), "log")
	write(filepath.Join(config, profileFile), `{"current":"p1","logins":[{"pool_id":"p1"}],"server":{"admin_token":"at","ts_authkey":"tskey-auth-k"}}`)
	write(filepath.Join(cache, "node-id"), "n1\n")
	write(filepath.Join(cache, "agents", "p1", "jobs", "j1", "out.txt"), "job")
	return config, cache
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// Tests that a default reset keeps bin/, tsnet/ and only the auth key.
func TestResetKeepsBinTsnetAndAuthKey(t *testing.T) {
	config, cache := machine(t)

	if _, err := reset(config, cache, true); err != nil {
		t.Fatal(err)
	}

	if got, want := names(t, config), []string{"bin", profileFile, "tsnet"}; !slices.Equal(got, want) {
		t.Fatalf("config folder = %v, want %v", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(config, "tsnet", "tailscaled.state")); string(b) != "identity" {
		t.Fatal("tsnet was changed")
	}
	var p map[string]any
	data, _ := os.ReadFile(filepath.Join(config, profileFile))
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if len(p["logins"].([]any)) != 0 || p["current"] != nil {
		t.Fatalf("logins survived: %s", data)
	}
	if srv := p["server"].(map[string]any); len(srv) != 1 || srv["ts_authkey"] != "tskey-auth-k" {
		t.Fatalf("server settings = %v, want only the auth key", srv)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("cache folder (node ID, job folders) should be gone")
	}
}

// Tests that -tailscale also forgets tsnet/ and the auth key.
func TestResetTailscaleForgetsIdentity(t *testing.T) {
	config, cache := machine(t)

	if _, err := reset(config, cache, false); err != nil {
		t.Fatal(err)
	}
	if got := names(t, config); !slices.Equal(got, []string{"bin"}) {
		t.Fatalf("config folder = %v, want only bin", got)
	}
}

// Tests that a machine with no relay folders is not an error.
func TestResetOnFreshMachine(t *testing.T) {
	root := t.TempDir()
	removed, err := reset(filepath.Join(root, "none"), filepath.Join(root, "nocache"), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed %v from a fresh machine", removed)
	}
}

// Tests that a stale or garbage pid file is skipped, not an error.
func TestStopProcessesSkipsStalePIDs(t *testing.T) {
	config := t.TempDir()
	os.WriteFile(filepath.Join(config, "relayd.pid"), []byte("999999999\n"), 0o600)
	os.WriteFile(filepath.Join(config, "relay-agent.p1.pid"), []byte("not a pid"), 0o600)
	os.WriteFile(filepath.Join(config, "relay-agent.p2.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)

	if stopped := stopProcesses(config); len(stopped) != 0 {
		t.Fatalf("stopped %v, want nothing", stopped)
	}
}

// Tests that a process named in a pid file is actually stopped.
func TestStopProcessesKillsRelayProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("checks the command line through /proc")
	}
	// A sleeping child whose command line contains "relay", like relayd.
	cmd := exec.Command("bash", "-c", "exec -a relay-agent sleep 30")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start a child process:", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	config := t.TempDir()
	os.WriteFile(filepath.Join(config, "relay-agent.p1.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)

	stopped := stopProcesses(config)
	if len(stopped) != 1 || !strings.Contains(stopped[0], "relay-agent.p1") {
		t.Fatalf("stopped = %v", stopped)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process was not stopped")
	}
}

// Tests the confirmation prompt.
func TestConfirm(t *testing.T) {
	cases := map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false}
	for in, want := range cases {
		var out bytes.Buffer
		if got := confirm(strings.NewReader(in), &out, "/c", "/k", true); got != want {
			t.Errorf("confirm(%q) = %v, want %v", in, got, want)
		}
		if !strings.Contains(out.String(), "tsnet/") {
			t.Errorf("prompt doesn't say what is kept: %q", out.String())
		}
	}
}
