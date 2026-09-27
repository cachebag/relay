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

	verifyPort(&testOutput, new(int))

	// Tests an invalid port number less than 1.
	invalidPort := -1
	verifyPort(&testOutput, &invalidPort)
	if testOutput.String() == "" {
		t.Errorf("expected error message for invalid port, but got none")
	}
	testOutput.Reset()

	// Tests a valid port number.
	validPort := 8080
	verifyPort(&testOutput, &validPort)
	if testOutput.String() != "" {
		t.Errorf("expected no error message for valid port, but got one")
	}
	testOutput.Reset()

	// Tests an invalid port number greater than 65535.
	invalidPort = 70000
	verifyPort(&testOutput, &invalidPort)
	if testOutput.String() == "" {
		t.Errorf("expected error message for invalid port, but got none")
	}
	testOutput.Reset()

}
