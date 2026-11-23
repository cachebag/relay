package agent

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

func TestSandboxArgv(t *testing.T) {
	s := &Sandbox{bwrap: "/usr/bin/bwrap", root: []string{"--symlink", "usr/bin", "/bin"}}
	got := s.Argv([]string{"sh", "run.sh", "10"}, "/tmp/relay/jobs/j1")
	want := []string{
		"/usr/bin/bwrap",
		"--unshare-all", "--die-with-parent", "--new-session",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/etc", "/etc",
		"--symlink", "usr/bin", "/bin",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--bind", "/tmp/relay/jobs/j1", "/tmp/relay/jobs/j1",
		"--chdir", "/tmp/relay/jobs/j1",
		"--",
		"sh", "run.sh", "10",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Argv:\n got %q\nwant %q", got, want)
	}
}

func TestSandboxArgvWithLimits(t *testing.T) {
	s := &Sandbox{
		bwrap:      "/usr/bin/bwrap",
		limits:     Limits{CPUs: 2, MemBytes: 1 << 30},
		systemdRun: "/usr/bin/systemd-run",
		taskset:    "/usr/bin/taskset",
	}
	got := s.Argv([]string{"true"}, "/j")
	wantPrefix := []string{
		"/usr/bin/systemd-run", "--user", "--scope", "--quiet", "--collect",
		"-p", "MemoryMax=1073741824", "-p", "MemorySwapMax=0",
		"/usr/bin/taskset", "-c", "0-1",
		"/usr/bin/bwrap",
	}
	if !slices.Equal(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("Argv with systemd:\n got %q\nwant prefix %q", got, wantPrefix)
	}

	s.systemdRun, s.prlimit, s.taskset = "", "/usr/bin/prlimit", ""
	got = s.Argv([]string{"true"}, "/j")
	wantPrefix = []string{"/usr/bin/prlimit", "--data=1073741824", "--", "/usr/bin/bwrap"}
	if !slices.Equal(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("Argv with prlimit:\n got %q\nwant prefix %q", got, wantPrefix)
	}
}

func TestDefaultLimits(t *testing.T) {
	got := DefaultLimits(store.Node{CPUs: 16, MemBytes: 24 << 30})
	if got.CPUs != 8 || got.MemBytes != 12<<30 {
		t.Fatalf("DefaultLimits(16 CPUs, 24 GB) = %+v, want 8 CPUs and 12 GB", got)
	}
	if got := DefaultLimits(store.Node{CPUs: 1}); got.CPUs != 1 {
		t.Fatalf("a 1-CPU node must still give the job 1 CPU, got %d", got.CPUs)
	}
}

func TestExplainKill(t *testing.T) {
	l := Limits{MemBytes: 2 << 30}
	got := explainKill(Result{ExitCode: 137}, false, l)
	if got.ExitCode != 137 || !strings.Contains(got.Stderr, "memory limit of 2.0 GB") {
		t.Fatalf("killed job: got %+v", got)
	}
	for _, c := range []struct {
		name     string
		res      Result
		canceled bool
		l        Limits
	}{
		{"normal failure", Result{ExitCode: 1}, false, l},
		{"deadline", Result{ExitCode: 137}, true, l},
		{"no memory cap", Result{ExitCode: 137}, false, Limits{}},
	} {
		if got := explainKill(c.res, c.canceled, c.l); got.Stderr != "" {
			t.Errorf("%s: should add nothing, got %q", c.name, got.Stderr)
		}
	}
}

func TestSandboxEnforcesCPULimit(t *testing.T) {
	s, err := FindSandbox()
	if s == nil || err != nil || runtime.NumCPU() < 2 {
		t.Skip("needs a working sandbox and at least 2 CPUs")
	}
	if _, err := exec.LookPath("taskset"); err != nil {
		t.Skip("taskset not installed")
	}
	s.SetLimits(Limits{CPUs: 1})
	dir := t.TempDir()
	res := s.Run(context.Background(), []string{"nproc"}, dir)
	if strings.TrimSpace(res.Stdout) != "1" {
		t.Fatalf("nproc in a 1-CPU sandbox = %q (stderr %q), want 1", res.Stdout, res.Stderr)
	}
}

func TestUseSandboxMarksTheNode(t *testing.T) {
	a := newAgent("http://scheduler", "token", store.Node{ID: "n1"}, t.TempDir())
	if a.node.Sandbox {
		t.Fatal("a new agent reports a sandbox before it has one")
	}
	a.UseSandbox(&Sandbox{bwrap: "bwrap"})
	if !a.node.Sandbox {
		t.Fatal("UseSandbox did not mark the node")
	}
}

// sandbox is this machine's real sandbox; the test is skipped without one.
// CI sets RELAY_REQUIRE_SANDBOX on Linux, so a runner that loses bwrap fails
// instead of quietly skipping every isolation test.
func sandbox(t *testing.T) *Sandbox {
	t.Helper()
	s, err := FindSandbox()
	if err != nil && os.Getenv("RELAY_REQUIRE_SANDBOX") != "" {
		t.Fatalf("RELAY_REQUIRE_SANDBOX is set and there is no usable sandbox: %v", err)
	}
	if err != nil {
		t.Skipf("no usable sandbox: %v", err)
	}
	if s == nil {
		t.Skip("sandboxing is Linux only")
	}
	return s
}

func runSandboxed(t *testing.T, s *Sandbox, dir string, script string) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return s.Run(ctx, []string{"sh", "-c", script}, dir)
}

func TestSandboxKeepsJobInItsFolder(t *testing.T) {
	s := sandbox(t)
	dir := t.TempDir()

	// A file of the contributor's, in the home folder and readable by the
	// agent's own user.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := os.CreateTemp(home, ".relay-sandbox-test-")
	if err != nil {
		t.Skipf("can't write a probe file in %s: %v", home, err)
	}
	t.Cleanup(func() { os.Remove(secret.Name()) })
	if _, err := secret.WriteString("contributor-secret"); err != nil {
		t.Fatal(err)
	}
	secret.Close()

	// Another job's folder, beside this one.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "data"), []byte("other-job"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("own folder is writable", func(t *testing.T) {
		res := runSandboxed(t, s, dir, "echo hi > out.txt && cat out.txt && pwd")
		if res.ExitCode != 0 || !strings.HasPrefix(res.Stdout, "hi\n"+dir) {
			t.Fatalf("exit %d, stdout %q, stderr %q", res.ExitCode, res.Stdout, res.Stderr)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "out.txt")); string(got) != "hi\n" {
			t.Fatalf("out.txt on the host = %q", got)
		}
	})

	for name, path := range map[string]string{
		"home file":        secret.Name(),
		"other job's file": filepath.Join(other, "data"),
	} {
		t.Run(name+" can't be read", func(t *testing.T) {
			res := runSandboxed(t, s, dir, "cat "+path)
			if res.ExitCode == 0 || strings.Contains(res.Stdout, "secret") || strings.Contains(res.Stdout, "other-job") {
				t.Fatalf("exit %d, stdout %q", res.ExitCode, res.Stdout)
			}
		})
		t.Run(name+" can't be changed", func(t *testing.T) {
			before, _ := os.ReadFile(path)
			runSandboxed(t, s, dir, "echo changed > "+path)
			if after, _ := os.ReadFile(path); string(after) != string(before) {
				t.Fatalf("%s changed from %q to %q", path, before, after)
			}
		})
	}

	t.Run("system files are read-only", func(t *testing.T) {
		if res := runSandboxed(t, s, dir, "echo x > /etc/relay-sandbox-test"); res.ExitCode == 0 {
			t.Fatal("a job wrote to /etc")
		}
		if _, err := os.Stat("/etc/relay-sandbox-test"); err == nil {
			os.Remove("/etc/relay-sandbox-test")
			t.Fatal("the write to /etc reached the host")
		}
	})

	t.Run("a later job still runs", func(t *testing.T) {
		if res := runSandboxed(t, s, dir, "echo ok"); res.ExitCode != 0 || res.Stdout != "ok\n" {
			t.Fatalf("exit %d, stdout %q, stderr %q", res.ExitCode, res.Stdout, res.Stderr)
		}
	})
}

func TestSandboxHasNoNetwork(t *testing.T) {
	s := sandbox(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("needs curl as the probe")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	reached := make(chan struct{}, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			reached <- struct{}{}
			conn.Close()
		}
	}()
	url := "http://" + ln.Addr().String() + "/"

	// The same probe outside the sandbox reaches the listener, so a failure
	// inside is the sandbox and not a broken probe.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	RunCommand(ctx, []string{"curl", "--max-time", "2", "-s", url}, t.TempDir())
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("the probe can't reach the listener even outside the sandbox")
	}

	res := runSandboxed(t, s, t.TempDir(), "curl --max-time 2 -s "+url)
	if res.ExitCode == 0 {
		t.Fatalf("curl succeeded inside the sandbox: %q", res.Stdout)
	}
	select {
	case <-reached:
		t.Fatal("a sandboxed job reached the network")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSandboxReportsAMissingCommandAsNotStarted(t *testing.T) {
	s := sandbox(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res := s.Run(ctx, []string{"relay-no-such-command"}, t.TempDir())
	if res.ExitCode != -1 || !strings.Contains(res.Stderr, "relay-no-such-command") {
		t.Fatalf("exit %d, stderr %q", res.ExitCode, res.Stderr)
	}
}

func TestSandboxStopsAJobAtItsDeadline(t *testing.T) {
	s := sandbox(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	res := s.Run(ctx, []string{"sh", "-c", "sleep 30 & sleep 30"}, t.TempDir())
	if took := time.Since(start); took > 6*time.Second {
		t.Fatalf("the job ran %s past a 2 s deadline", took)
	}
	if res.ExitCode != -1 {
		t.Fatalf("exit %d, want -1", res.ExitCode)
	}
}

// The whole path a real job takes: the agent makes the job's folder, runs
// the command through the sandbox, and packs what it left in out/.
func TestAgentRunsAJobInTheSandbox(t *testing.T) {
	s := sandbox(t)
	a := newAgent("http://scheduler", "token", store.Node{ID: "n1"}, t.TempDir())
	a.UseSandbox(s)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	earlier, err := a.makeWorkdir("earlier")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(earlier, "data"), []byte("other-job"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir, err := a.makeWorkdir("j1")
	if err != nil {
		t.Fatal(err)
	}
	script := `echo "home=$HOME" && echo result > out/result.txt && cat ` + filepath.Join(earlier, "data")
	res := a.runInWorkdir(ctx, store.Job{ID: "j1", Command: []string{"sh", "-c", script}}, dir)

	if !strings.Contains(res.Stdout, "home="+dir) {
		t.Fatalf("HOME is not the job's folder: stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
	if res.ExitCode == 0 || strings.Contains(res.Stdout, "other-job") {
		t.Fatalf("the job read an earlier job's folder: exit %d, stdout %q", res.ExitCode, res.Stdout)
	}
	path, size, err := packOutput(dir)
	if err != nil || path == "" || size == 0 {
		t.Fatalf("out/ was not packed: path %q, size %d, err %v", path, size, err)
	}
}
