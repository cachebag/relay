package main

import (
	"bytes"
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

// Tests if program prints the version. Any version number will pass.
func TestPrintVersion(t *testing.T) {
	var testOutput bytes.Buffer

	printVersion(&testOutput)

	if actual := testOutput.String(); !strings.HasPrefix(actual, "relay-agent ") {
		t.Errorf(
			"expected output to start with %q, actual: %q",
			"relay-agent ",
			actual,
		)
	}
}

// Tests that the startup message contains the relay-agent version,
// machine information, and scheduler URL.
func TestPrintStartup(t *testing.T) {
	var testOutput bytes.Buffer

	// Creates predictable node information so the exact startup
	// message can be verified.
	node := store.Node{
		Hostname: "test-machine",
		OS:       "windows",
		Arch:     "amd64",
		CPUs:     8,
		Owner:    "akrm",

		// 16 GiB expressed in bytes.
		MemBytes: 16 * 1024 * 1024 * 1024,
	}

	scheduler := "http://localhost:8080"

	// Prints the startup message into the test buffer instead
	// of printing it to the terminal.
	printStartup(&testOutput, node, scheduler)

	expected := fmt.Sprintf(
		"relay-agent %s: test-machine (windows/amd64, 8 CPUs, 16.0 GB) connecting to http://localhost:8080 as akrm\n",
		Version,
	)

	actual := testOutput.String()

	if actual != expected {
		t.Errorf(
			"expected: %q, actual: %q",
			expected,
			actual,
		)
	}
}

// Tests that RELAY_SCHEDULER is used as the scheduler default.
func TestSchedulerDefaultFromEnv(t *testing.T) {
	t.Setenv("RELAY_SCHEDULER", "http://x:1")

	actual := schedulerDefault()
	expected := "http://x:1"

	if actual != expected {
		t.Errorf("expected: %q, actual: %q", expected, actual)
	}
}

// Tests that localhost:8080 is used when RELAY_SCHEDULER is not set.
func TestSchedulerDefaultFallback(t *testing.T) {
	t.Setenv("RELAY_SCHEDULER", "")

	actual := schedulerDefault()
	expected := "http://localhost:8080"

	if actual != expected {
		t.Errorf("expected: %q, actual: %q", expected, actual)
	}
}

// Tests that RELAY_POOL_TOKEN is used as the pool token default.
func TestPoolTokenDefaultFromEnv(t *testing.T) {
	t.Setenv("RELAY_POOL_TOKEN", "test-token")

	actual := poolTokenDefault()
	expected := "test-token"

	if actual != expected {
		t.Errorf("expected: %q, actual: %q", expected, actual)
	}
}

// Tests that an empty string is returned when RELAY_POOL_TOKEN is not set.
func TestPoolTokenDefaultFallback(t *testing.T) {
	t.Setenv("RELAY_POOL_TOKEN", "")

	actual := poolTokenDefault()
	expected := ""

	if actual != expected {
		t.Errorf("expected: %q, actual: %q", expected, actual)
	}
}

// The explicit cache flag overrides RELAY_AGENT_DIR, leaving the default
// empty so standalone agents continue using their existing cache layout.
func TestCacheDirFlag(t *testing.T) {
	t.Setenv("RELAY_AGENT_DIR", "/cache/from-env")
	fs := flag.NewFlagSet("relay-agent", flag.ContinueOnError)
	cacheDir := cacheDirFlag(fs)
	if *cacheDir != "/cache/from-env" {
		t.Fatalf("flag default = %q, want environment value", *cacheDir)
	}
	if err := fs.Parse([]string{"--cache-dir", "/cache/from-flag"}); err != nil {
		t.Fatal(err)
	}
	if *cacheDir != "/cache/from-flag" {
		t.Fatalf("flag value = %q, want explicit override", *cacheDir)
	}

	t.Setenv("RELAY_AGENT_DIR", "")
	fallback := flag.NewFlagSet("relay-agent", flag.ContinueOnError)
	if dir := *cacheDirFlag(fallback); dir != "" {
		t.Errorf("default = %q, want empty for legacy cache behavior", dir)
	}
}

// Tests that the help output includes an example showing
// how to join a Relay pool.
func TestPrintUsage(t *testing.T) {
	var testOutput bytes.Buffer

	// Creates a separate FlagSet so the test does not modify
	// the command-line flags used by relay-agent.
	testFlags := flag.NewFlagSet("relay-agent", flag.ContinueOnError)

	testFlags.String(
		"scheduler",
		"http://localhost:8080",
		"scheduler base URL",
	)

	testFlags.String(
		"pool-token",
		"",
		"join token for the pool (or set RELAY_POOL_TOKEN)",
	)

	testFlags.Bool(
		"version",
		false,
		"Prints what version of relay-agent is being used",
	)

	printUsage(&testOutput, testFlags)

	output := testOutput.String()

	// Help should explain the basic command format.
	if !strings.Contains(output, "Usage: relay-agent [options]") {
		t.Errorf(
			"expected usage header in output, actual:\n%s",
			output,
		)
	}

	// Help should contain an example showing how to join a pool.
	expectedExample :=
		"relay-agent --scheduler http://localhost:8080 --pool-token <token>"

	if !strings.Contains(output, expectedExample) {
		t.Errorf(
			"expected example %q in output, actual:\n%s",
			expectedExample,
			output,
		)
	}
}

// Tests that --owner wins over RELAY_OWNER, and RELAY_OWNER is used
// when the flag isn't given.
func TestOwnerFlag(t *testing.T) {
	t.Setenv("RELAY_OWNER", "from-env")

	parse := func(args ...string) string {
		fs := flag.NewFlagSet("relay-agent", flag.ContinueOnError)
		owner := ownerFlag(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatalf("parse: %v", err)
		}
		return *owner
	}

	if actual := parse(); actual != "from-env" {
		t.Errorf("expected: %q, actual: %q", "from-env", actual)
	}
	if actual := parse("--owner", "from-flag"); actual != "from-flag" {
		t.Errorf("expected: %q, actual: %q", "from-flag", actual)
	}
}

// Tests that the OS username is used when RELAY_OWNER is not set.
func TestOwnerDefaultFallback(t *testing.T) {
	t.Setenv("RELAY_OWNER", "")

	if actual := ownerDefault(); actual == "" {
		t.Error("expected the OS username, got an empty string")
	}
}

// Tests that the DOMAIN\ part of a Windows username is dropped.
func TestStripDomain(t *testing.T) {
	for in, expected := range map[string]string{
		`CORP\akrm`: "akrm",
		"akrm":      "akrm",
	} {
		if actual := stripDomain(in); actual != expected {
			t.Errorf("stripDomain(%q): expected %q, actual %q", in, expected, actual)
		}
	}
}
