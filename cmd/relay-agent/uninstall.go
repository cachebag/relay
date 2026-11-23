package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cachebag/relay/internal/agent"
)

type uninstallDirs struct {
	cache    string
	temp     string
	config   string
	services []string
}

func defaultUninstallDirs() uninstallDirs {
	var d uninstallDirs
	if dir, err := os.UserCacheDir(); err == nil {
		d.cache = filepath.Join(dir, "relay")
	}
	d.temp = filepath.Join(os.TempDir(), "relay-jobs")
	if dir, err := os.UserConfigDir(); err == nil {
		d.config = filepath.Join(dir, "relay")
		switch runtime.GOOS {
		case "linux":
			d.services = append(d.services, filepath.Join(dir, "systemd", "user"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil && runtime.GOOS == "darwin" {
		d.services = append(d.services, filepath.Join(home, "Library", "LaunchAgents"))
	}
	return d
}

func runUninstall(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	scheduler := fs.String("scheduler", schedulerDefault(), "scheduler base URL, to leave the pool first")
	poolToken := fs.String("pool-token", poolTokenDefault(), "join token, to leave the pool first (or set RELAY_POOL_TOKEN)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "relay-agent uninstall: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	code := 0
	if *poolToken != "" {
		if c := leaveForUninstall(*scheduler, *poolToken, stdout, stderr); c != 0 {
			fmt.Fprintln(stderr, "relay-agent uninstall: continuing without leaving; a pool admin can remove this machine with relay node remove")
			code = 1
		}
	}

	exe, _ := os.Executable()
	if uninstall(defaultUninstallDirs(), os.Getpid(), stdout, stderr) != 0 {
		code = 1
	}

	if exe != "" {
		fmt.Fprintf(stdout, "to finish, delete the agent itself: %s\n", exe)
	}
	if code != 0 {
		fmt.Fprintln(stderr, "relay-agent uninstall: did not finish cleanly, remove what is listed above by hand")
	}
	return code
}

func leaveForUninstall(scheduler, token string, stdout, stderr io.Writer) int {
	node, err := agent.Collect()
	if err != nil {
		fmt.Fprintf(stderr, "relay-agent uninstall: %v\n", err)
		return 1
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintln(stderr, "relay-agent uninstall: no cache folder, so no saved node ID to leave with")
		return 1
	}
	data, err := os.ReadFile(filepath.Join(cache, "relay", "node-id"))
	id := strings.TrimSpace(string(data))
	if err != nil || id == "" {
		fmt.Fprintln(stderr, "relay-agent uninstall: this machine has no saved node ID, so it isn't in a pool from here")
		return 1
	}
	node.ID = id
	client := &http.Client{Timeout: 5 * time.Second}
	noClear := func() ([]string, error) { return nil, nil }
	return leave(context.Background(), client, scheduler, token, node, noClear, stdout, stderr)
}

func uninstall(d uninstallDirs, self int, stdout, stderr io.Writer) int {
	code := 0
	fail := func(path string, err error) {
		fmt.Fprintf(stderr, "couldn't remove %s: %v\n", path, err)
		code = 1
	}

	if d.config != "" {
		pids, _ := filepath.Glob(filepath.Join(d.config, "relay-agent*.pid"))
		for _, pidFile := range pids {
			data, err := os.ReadFile(pidFile)
			if err != nil {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || pid <= 0 || pid == self {
				continue
			}
			switch stopped, err := stopAgent(pid); {
			case err != nil:
				fmt.Fprintf(stderr, "couldn't stop the agent with PID %d: %v (run relay contribute stop --all)\n", pid, err)
				code = 1
			case stopped:
				fmt.Fprintf(stdout, "stopped the agent with PID %d\n", pid)
			}
		}
	}

	for _, dir := range d.services {
		entries, _ := filepath.Glob(filepath.Join(dir, "*relay-agent*"))
		for _, e := range entries {
			if err := os.Remove(e); err != nil && !errors.Is(err, fs.ErrNotExist) {
				fail(e, err)
				continue
			}
			fmt.Fprintf(stdout, "removed %s\n", e)
		}
	}

	for _, dir := range []string{d.cache, d.temp} {
		if dir == "" {
			continue
		}
		if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			fail(dir, err)
			continue
		}
		fmt.Fprintf(stdout, "removed %s\n", dir)
	}

	if d.config != "" {
		files, _ := filepath.Glob(filepath.Join(d.config, "relay-agent*"))
		for _, f := range files {
			if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
				fail(f, err)
				continue
			}
			fmt.Fprintf(stdout, "removed %s\n", f)
		}
	}
	return code
}

func stopAgent(pid int) (bool, error) {
	if runtime.GOOS != "linux" {
		return false, errors.New("can't confirm it is relay-agent on this OS")
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false, nil
	}
	if !strings.Contains(string(cmdline), "relay-agent") {
		return false, nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return false, err
	}
	for range 50 {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, errors.New("still running 5 seconds after SIGTERM")
}
