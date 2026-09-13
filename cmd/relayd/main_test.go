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
