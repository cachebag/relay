package main

import (
	"bytes"
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

		// 16 GiB expressed in bytes.
		MemBytes: 16 * 1024 * 1024 * 1024,
	}

	scheduler := "http://localhost:8080"

	// Prints the startup message into the test buffer instead
	// of printing it to the terminal.
	printStartup(&testOutput, node, scheduler)

	expected := fmt.Sprintf(
		"relay-agent %s: test-machine (windows/amd64, 8 CPUs, 16.0 GB) joining http://localhost:8080\n",
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
