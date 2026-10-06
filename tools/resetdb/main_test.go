package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

// Tests that reset removes a real database and its WAL/SHM sidecars.
func TestResetRemovesDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.db")

	reg, err := store.NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	reg.Close()

	removed, err := reset(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) == 0 {
		t.Fatal("expected at least the database file to be removed")
	}
	for _, suffix := range sidecars {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Errorf("%s still exists", path+suffix)
		}
	}
}

// Tests that reset is a no-op when there is no database.
func TestResetMissing(t *testing.T) {
	removed, err := reset(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil || len(removed) != 0 {
		t.Errorf("got removed=%v err=%v, want nothing removed and no error", removed, err)
	}
}

// Tests that reset refuses to touch a directory.
func TestResetDirectory(t *testing.T) {
	if _, err := reset(t.TempDir()); err == nil {
		t.Error("expected an error for a directory path")
	}
}

// Tests that only "y" or "yes" confirm.
func TestConfirm(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{{"y\n", true}, {"YES\n", true}, {"n\n", false}, {"\n", false}, {"", false}} {
		var out bytes.Buffer
		if got := confirm(strings.NewReader(tc.input), &out, "relay.db"); got != tc.want {
			t.Errorf("confirm(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
