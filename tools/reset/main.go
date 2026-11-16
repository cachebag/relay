// Command reset is a developer-only tool that puts this machine's relay
// back to a clean slate: it stops relayd and every relay-agent the CLI
// started, deletes pools, logins, tokens, logs and job folders, and gives
// the machine a new node ID so no old pool can hold it.
//
// It keeps the downloaded binaries (bin/) and, unless -tailscale is given,
// the Tailscale identity (tsnet/) and the saved Tailscale auth key, so
// `relay scheduler start --public` comes back on the same public address
// with no DNS or certificate wait.
//
// It lives under tools/ rather than cmd/ so it is never built into a release
// or exposed through the relay CLI. Run it from the repo root with:
//
//	go run ./tools/reset
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// What the CLI writes to the config folder that survives a reset.
var keepAlways = []string{"bin"}
var keepTailscale = []string{"bin", "tsnet"}

// The CLI's saved settings file, cli/relay/profile.py FILENAME.
const profileFile = "cli.json"

// configDir is the CLI's config folder (cli/relay/profile.py config_dir):
// ~/.config/relay, ~/Library/Application Support/relay or %AppData%\relay.
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "relay"), nil
}

// cacheDir holds the agents' job folders and the machine's node ID
// (internal/agent defaultCacheDir and the CLI's per-pool agent folders).
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "relay"), nil
}

// pidFiles lists relayd.pid and every relay-agent*.pid in the config folder.
func pidFiles(config string) []string {
	var out []string
	if _, err := os.Stat(filepath.Join(config, "relayd.pid")); err == nil {
		out = append(out, filepath.Join(config, "relayd.pid"))
	}
	agents, _ := filepath.Glob(filepath.Join(config, "relay-agent*.pid"))
	return append(out, agents...)
}

// looksLikeRelay guards against killing an unrelated process that reused a
// stale PID. Only Linux can check cheaply; elsewhere it trusts the pid file.
func looksLikeRelay(pid int) bool {
	if runtime.GOOS != "linux" {
		return true
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false // not running
	}
	return strings.Contains(string(cmdline), "relay")
}

// stopProcesses kills the processes named in the pid files. Returns a line
// per process it stopped; stale or unreadable pid files are skipped.
func stopProcesses(config string) []string {
	var stopped []string
	for _, path := range pidFiles(config) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 || pid == os.Getpid() || !looksLikeRelay(pid) {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		// Kill works on every OS (TerminateProcess on Windows), unlike the
		// signal-0 liveness check that trips the CLI on Windows.
		if err := proc.Kill(); err == nil {
			stopped = append(stopped, fmt.Sprintf("stopped %s (PID %d)", strings.TrimSuffix(filepath.Base(path), ".pid"), pid))
		}
	}
	return stopped
}

// savedAuthKey reads the Tailscale auth key from the CLI's settings file.
func savedAuthKey(config string) string {
	data, err := os.ReadFile(filepath.Join(config, profileFile))
	if err != nil {
		return ""
	}
	var p struct {
		Server struct {
			TSAuthKey string `json:"ts_authkey"`
		} `json:"server"`
	}
	if json.Unmarshal(data, &p) != nil {
		return ""
	}
	return p.Server.TSAuthKey
}

// writeAuthKey writes a settings file with no logins and only the key, in
// the shape cli/relay/profile.py reads, owner-only like the CLI writes it.
func writeAuthKey(config, key string) error {
	data, err := json.MarshalIndent(map[string]any{
		"current": nil,
		"logins":  []any{},
		"server":  map[string]string{"ts_authkey": key},
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(config, profileFile), data, 0o600)
}

// reset wipes config (except the kept entries) and the whole cache folder.
// Returns the paths it removed. Missing folders are not an error.
func reset(config, cache string, keepTS bool) ([]string, error) {
	keep := keepAlways
	key := ""
	if keepTS {
		keep = keepTailscale
		key = savedAuthKey(config)
	}

	var removed []string
	var errs []error
	entries, err := os.ReadDir(config)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if contains(keep, e.Name()) {
			continue
		}
		p := filepath.Join(config, e.Name())
		if err := os.RemoveAll(p); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, p)
	}
	if _, err := os.Stat(cache); err == nil {
		if err := os.RemoveAll(cache); err != nil {
			errs = append(errs, err)
		} else {
			removed = append(removed, cache)
		}
	}
	if key != "" {
		if err := writeAuthKey(config, key); err != nil {
			errs = append(errs, err)
		}
	}
	return removed, errors.Join(errs...)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Reports whether something is listening on localhost at port.
func listening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Asks the user to confirm on in/out. Anything other than "y" or "yes" is a no.
func confirm(in io.Reader, out io.Writer, config, cache string, keepTS bool) bool {
	kept := "bin/, tsnet/ and the Tailscale key"
	if !keepTS {
		kept = "bin/ only"
	}
	fmt.Fprintf(out, "This stops relayd and every relay-agent, then permanently deletes pools, logins, tokens, logs and job folders in\n  %s\n  %s\n(keeps %s). Continue? [y/N] ", config, cache, kept)
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func main() {
	yes := flag.Bool("y", false, "skip the confirmation prompt")
	tailscale := flag.Bool("tailscale", false, "also forget the Tailscale identity and auth key (new public address)")
	port := flag.Int("port", 8080, "relayd port to check is free after stopping")
	flag.Parse()

	config, err := configDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset:", err)
		os.Exit(1)
	}
	cache, err := cacheDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset:", err)
		os.Exit(1)
	}

	if !*yes && !confirm(os.Stdin, os.Stdout, config, cache, !*tailscale) {
		fmt.Println("aborted")
		return
	}

	for _, line := range stopProcesses(config) {
		fmt.Println(line)
	}
	// Give relayd a moment to let go of relay.db (Windows won't delete an
	// open file).
	for i := 0; i < 20 && listening(*port); i++ {
		time.Sleep(150 * time.Millisecond)
	}
	if listening(*port) {
		fmt.Fprintf(os.Stderr, "reset: something is still listening on :%d; if it's relayd, stop it (taskkill /F /IM relayd.exe on Windows) and run reset again\n", *port)
	}

	removed, err := reset(config, cache, !*tailscale)
	for _, p := range removed {
		fmt.Println("deleted", p)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset: some files could not be deleted:", err)
		os.Exit(1)
	}
	if *tailscale {
		fmt.Println("relay reset done (kept bin/)")
	} else {
		fmt.Println("relay reset done (kept bin/, tsnet/ and the Tailscale key)")
	}
	fmt.Println("next: relay scheduler start --public, then relay pool create <name>")
}
