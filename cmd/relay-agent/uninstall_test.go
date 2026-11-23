package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallRemovesAgentFiles(t *testing.T) {
	root := t.TempDir()
	d := uninstallDirs{
		cache:    filepath.Join(root, "cache", "relay"),
		temp:     filepath.Join(root, "tmp", "relay-jobs"),
		config:   filepath.Join(root, "config", "relay"),
		services: []string{filepath.Join(root, "systemd", "user")},
	}
	files := map[string]bool{
		filepath.Join(d.cache, "node-id"):                         true,
		filepath.Join(d.cache, "agents", "p1", "jobs", "j1", "x"): true,
		filepath.Join(d.temp, "j2", "y"):                          true,
		filepath.Join(d.config, "relay-agent.p1.log"):             true,
		filepath.Join(d.config, "relay-agent.p1.state.json"):      true,
		filepath.Join(d.config, "relay-agent.p1.pid"):             true,
		filepath.Join(d.services[0], "relay-agent.service"):       true,
		filepath.Join(d.config, "cli.json"):                       false,
		filepath.Join(d.config, "bin", "relayd"):                  false,
		filepath.Join(d.services[0], "relayd.service"):            false,
	}
	for p := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		content := "x"
		if strings.HasSuffix(p, ".pid") {
			content = "999999999"
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var out, errOut bytes.Buffer
	if code := uninstall(d, os.Getpid(), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, errOut.String())
	}
	for p, gone := range files {
		_, err := os.Stat(p)
		if gone && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s should be removed", p)
		}
		if !gone && err != nil {
			t.Errorf("%s should stay: %v", p, err)
		}
	}
	for _, want := range []string{"removed " + d.cache, "removed " + d.temp, "relay-agent.p1.log"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestUninstallNothingThere(t *testing.T) {
	root := t.TempDir()
	d := uninstallDirs{cache: filepath.Join(root, "a"), temp: filepath.Join(root, "b"), config: filepath.Join(root, "c")}
	var out, errOut bytes.Buffer
	if code := uninstall(d, os.Getpid(), &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}

func TestRunUninstallRejectsExtraArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runUninstall([]string{"now"}, &out, &errOut); code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
}
