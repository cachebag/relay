package agent

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
func sandbox(t *testing.T) *Sandbox {
	t.Helper()
	s, err := FindSandbox()
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
