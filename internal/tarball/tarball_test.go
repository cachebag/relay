package tarball

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A folder packs, lists and unpacks back to the same files.
func TestRoundTrip(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "out", "deep"), 0o755)
	os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(src, "out", "deep", "a.csv"), []byte("1,2\n"), 0o644)

	var buf bytes.Buffer
	if err := Pack(&buf, src); err != nil {
		t.Fatal(err)
	}

	files, err := List(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != (File{"out/deep/a.csv", 4}) || files[1] != (File{"run.sh", 10}) {
		t.Fatalf("listed %+v", files)
	}

	dst := t.TempDir()
	if err := Unpack(bytes.NewReader(buf.Bytes()), dst, 1<<20); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "out", "deep", "a.csv"))
	if err != nil || string(got) != "1,2\n" {
		t.Fatalf("a.csv = %q, %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(dst, "run.sh")); err != nil || (info.Mode()&0o100 == 0 && !isWindows()) {
		t.Fatalf("run.sh lost its mode: %v %v", info, err)
	}
}

func isWindows() bool { return os.PathSeparator == '\\' }

// rawTar builds a gzipped tar with one file at the given entry name.
func rawTar(t *testing.T, name string, size int) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: int64(size)}); err != nil {
		t.Fatal(err)
	}
	tw.Write(bytes.Repeat([]byte("x"), size))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// Entries that would escape the folder are refused by both List and Unpack.
func TestBadPathsRefused(t *testing.T) {
	for _, name := range []string{"../evil", "/etc/passwd", "a/../../b", "..", `C:\x`} {
		data := rawTar(t, name, 1)
		if _, err := List(bytes.NewReader(data)); !errors.Is(err, ErrBadPath) {
			t.Errorf("List(%q) err = %v, want ErrBadPath", name, err)
		}
		if err := Unpack(bytes.NewReader(data), t.TempDir(), 1<<20); !errors.Is(err, ErrBadPath) {
			t.Errorf("Unpack(%q) err = %v, want ErrBadPath", name, err)
		}
	}
	// "./x" and "x/" are fine and normalise.
	files, err := List(bytes.NewReader(rawTar(t, "./sub/x", 1)))
	if err != nil || len(files) != 1 || files[0].Path != "sub/x" {
		t.Fatalf("List(./sub/x) = %+v, %v", files, err)
	}
}

// Unpack stops at the limit instead of filling the disk.
func TestUnpackLimit(t *testing.T) {
	data := rawTar(t, "big", 2000)
	if err := Unpack(bytes.NewReader(data), t.TempDir(), 1000); !errors.Is(err, ErrTooBig) {
		t.Fatalf("err = %v, want ErrTooBig", err)
	}
	if err := Unpack(bytes.NewReader(data), t.TempDir(), 2000); err != nil {
		t.Fatalf("err = %v, want nil at exactly the limit", err)
	}
}

// Something that isn't a gzipped tar is reported, not crashed on.
func TestNotATarball(t *testing.T) {
	if _, err := List(bytes.NewReader([]byte("hello"))); err == nil {
		t.Fatal("List accepted plain text")
	}
}
