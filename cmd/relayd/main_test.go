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

// Tests if the verifyPort function correctly validates port numbers.
func TestPortVerification(t *testing.T) {
	var testOutput bytes.Buffer

	// Testing allowing the os to choose the port by specifying 0.
	validPort := 0
	verifyPort(&testOutput, validPort)
	if testOutput.String() != "" {
		t.Errorf("expected no error message for valid port, but got one")
	}
	testOutput.Reset()

	// Tests a valid number on the lower boundary.
	validPort = 1
	verifyPort(&testOutput, validPort)
	if testOutput.String() != "" {
		t.Errorf("expected no error message for valid port, but got one")
	}
	testOutput.Reset()

	// Tests a valid port number.
	validPort = 8080
	verifyPort(&testOutput, validPort)
	if testOutput.String() != "" {
		t.Errorf("expected no error message for valid port, but got one")
	}
	testOutput.Reset()

	// Tests a valid number on the upper boundary.
	validPort = 65535
	verifyPort(&testOutput, validPort)
	if testOutput.String() != "" {
		t.Errorf("expected no error message for valid port, but got one")
	}
	testOutput.Reset()

}
