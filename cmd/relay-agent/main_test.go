package main

import (
	"bytes"
	"strings"
	"testing"
)

// Tests if program prints the version. Any version number will pass.
func TestPrintVersion(t *testing.T) {
	var testOutput bytes.Buffer

	printVersion(&testOutput)

	if actual := testOutput.String(); !strings.HasPrefix(actual, "relay-agent ") {
		t.Errorf("expected output to start with %q, actual: %q", "relay-agent ", actual)
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
