package main

import (
	"bytes"
	"testing"
)

// Tests if program prints correct version (version 0.0.0)
func TestPrintVersion(t *testing.T) {
	var testOutput bytes.Buffer

	printVersion(&testOutput)

	actual := testOutput.String()
	expected := "relayd 0.0.0\n"

	if actual != expected {
		t.Errorf("expected: %q, actual: %q", expected, actual)
	}
}
