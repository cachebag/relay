package store

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A job's input and output bundles round-trip on disk and go away together.
func TestBundlesRoundTrip(t *testing.T) {
	b, err := NewBundles(filepath.Join(t.TempDir(), "bundles"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := b.Put("job-1", BundleInput, strings.NewReader("input"))
	if err != nil || n != 5 {
		t.Fatalf("Put input = %d, %v", n, err)
	}
	if _, err := b.Put("job-1", BundleOutput, strings.NewReader("output!")); err != nil {
		t.Fatal(err)
	}

	f, size, err := b.Open("job-1", BundleOutput)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	if size != 7 || string(data) != "output!" {
		t.Fatalf("Open output = %q (%d)", data, size)
	}

	if _, _, err := b.Open("job-2", BundleInput); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing bundle err = %v, want ErrNotFound", err)
	}

	if err := b.Delete("job-1"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{BundleInput, BundleOutput} {
		if _, _, err := b.Open("job-1", kind); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s still there after Delete: %v", kind, err)
		}
	}
	if err := b.Delete("job-1"); err != nil {
		t.Errorf("second Delete = %v, want nil", err)
	}
}

// An ID that could name a file elsewhere is refused.
func TestBundlesRefuseBadID(t *testing.T) {
	b, _ := NewBundles(t.TempDir())
	for _, id := range []string{"", "..", "../x", `a\b`, "a/b"} {
		if _, err := b.Put(id, BundleInput, strings.NewReader("x")); err == nil {
			t.Errorf("Put(%q) accepted", id)
		}
	}
}

// The bundle fields on a job survive both stores.
func TestJobBundleFieldsRoundTrip(t *testing.T) {
	sq, err := NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sq.Close()
	sqlJobs, err := NewSQLiteJobs(sq.DB())
	if err != nil {
		t.Fatal(err)
	}
	for name, js := range map[string]JobStore{"mem": NewMemJobs(), "sqlite": sqlJobs} {
		ctx := context.Background()
		j := Job{ID: "j1", Command: []string{"--fast"}, Entry: "run.sh", BundleBytes: 1234, Status: JobQueued, CreatedAt: time.Now().UTC()}
		if err := js.Create(ctx, j); err != nil {
			t.Fatal(name, err)
		}
		got, _ := js.Get(ctx, "j1")
		if got.Entry != "run.sh" || got.BundleBytes != 1234 || got.OutputBytes != 0 || got.OutputFiles != nil {
			t.Fatalf("%s: after create %+v", name, got)
		}
		got.OutputBytes = 99
		got.OutputFiles = []OutputFile{{Path: "out/a.csv", Bytes: 10}}
		if err := js.Update(ctx, got); err != nil {
			t.Fatal(name, err)
		}
		got.OutputFiles[0].Path = "changed by caller"
		list, _ := js.List(ctx)
		if len(list) != 1 || list[0].OutputBytes != 99 || len(list[0].OutputFiles) != 1 || list[0].OutputFiles[0] != (OutputFile{"out/a.csv", 10}) {
			t.Fatalf("%s: after update %+v", name, list)
		}
	}
}
