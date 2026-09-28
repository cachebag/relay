package main

import (
	"bytes"
	"testing"
)

// Tests if program prints the version. Any version number will pass.
func TestPrintVersion(t *testing.T) {
	var testOutput bytes.Buffer

	printVersion(&testOutput)

	actual := testOutput.String()[:6]
	expected := "relayd"

	if actual != expected {
		t.Errorf("expected: %q, actual: %q", expected, actual)
	}
}

// Tests that validPort accepts 0-65535 and rejects everything else.
func TestValidPort(t *testing.T) {
	for _, tc := range []struct {
		port int
		ok   bool
	}{{-1, false}, {0, true}, {1, true}, {8080, true}, {65535, true}, {65536, false}} {
		if err := validPort(tc.port); (err == nil) != tc.ok {
			t.Errorf("validPort(%d): got err=%v, want ok=%v", tc.port, err, tc.ok)
		}
	}
}
