package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
)


Import (
	"bytes"
	"strings"
	"testing"

"github.com/cachebag/relay/internal/store"
)

func TestStartupSaysConnecting(t *testing.T) {
	var out bytes.Buffer
	printStartup(&out, store.Node{Hostname: "m", Owner: "akrm"}, "https://relay.example")

	if !strings.Contains(out.String(), "connecting to https://relay.example as akrm") {
		t.Errorf("startup line should say connecting to, got %q", out.String())
	}
	if strings.Contains(out.String(), "joining") {
		t.Errorf("startup line still says joining: %q", out.String())
	}
}