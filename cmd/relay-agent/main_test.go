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
