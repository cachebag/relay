package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchedulerLogAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relayd.log")
	if err := os.WriteFile(path, []byte("previous start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openSchedulerLog(path)
	if err != nil {
		t.Fatal(err)
	}
	logger := log.New(file, "", 0)
	logger.Print("relayd listening on :8080")
	fmt.Fprintln(file, "relayd public address: https://relay.example")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "previous start\nrelayd listening on :8080\nrelayd public address: https://relay.example\n"
	if string(data) != want {
		t.Fatalf("log = %q, want %q", data, want)
	}
}

func TestSavedAuthKey(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		invalid          bool
	}{
		{"saved key", `{"server":{"ts_authkey":"tskey-auth-saved"},"logins":[]}`, "tskey-auth-saved", false},
		{"no server", `{"logins":[]}`, "", false},
		{"no key", `{"server":{"address":"http://localhost:8080"}}`, "", false},
		{"malformed", `{"server":{"ts_authkey":"secret"`, "", true},
		{"wrong key type", `{"server":{"ts_authkey":123}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cli.json")
			if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := savedAuthKey(path)
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("savedAuthKey = %q, %v; want %q, invalid=%v", got, err, tc.want, tc.invalid)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("error exposed a credential")
			}
		})
	}
	if got, err := savedAuthKey(filepath.Join(t.TempDir(), "missing.json")); err != nil || got != "" {
		t.Fatalf("missing profile = %q, %v", got, err)
	}
}

func TestSchedulerAuthKeyEnvironmentWins(t *testing.T) {
	t.Setenv("TS_AUTHKEY", "tskey-auth-environment")
	if got, err := schedulerAuthKey(t.TempDir()); err != nil || got != "tskey-auth-environment" {
		t.Fatalf("schedulerAuthKey = %q, %v", got, err)
	}
}

func TestStartupTaskUsesItsInstalledProfile(t *testing.T) {
	t.Setenv("TS_AUTHKEY", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli.json"), []byte(`{"server":{"ts_authkey":"tskey-auth-saved"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := schedulerAuthKey(dir)
	if err != nil || key != "tskey-auth-saved" {
		t.Fatalf("schedulerAuthKey = %q, %v", key, err)
	}
	cfg, err := newPublicConfig("relay", key, dir)
	if err != nil || cfg.Dir != filepath.Join(dir, "tsnet") {
		t.Fatalf("public state directory = %q, %v", cfg.Dir, err)
	}
}

func TestServiceMainWritesTokenToLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relayd.log")
	for range 2 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestServiceMainProcess$")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "RELAY_TEST_SERVICE_LOG="+path)
		if out, err := cmd.CombinedOutput(); err != nil || len(out) != 0 {
			t.Fatalf("service process: %v; unexpected console output: %s", err, out)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "server admin token:") != 2 {
		t.Fatal("scheduler starts did not append their admin tokens to the log")
	}
}

func TestServiceMainProcess(t *testing.T) {
	path := os.Getenv("RELAY_TEST_SERVICE_LOG")
	if path == "" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("relayd", flag.ExitOnError)
	os.Args = []string{"relayd", "--log-file", path, "--new-admin-token"}
	main()
	os.Exit(0)
}
