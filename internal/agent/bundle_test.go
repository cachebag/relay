package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
	"github.com/cachebag/relay/internal/tarball"
)

// testBundle builds a gzipped tar from name -> content.
func testBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o755, Size: int64(len(content))})
		tw.Write([]byte(content))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// A bundle job: the files are fetched as this node, unpacked into the job
// folder, the entry runs there with the job's arguments, and whatever it
// wrote to out/ goes back with the result. The folder is gone afterwards.
func TestBundleJobRunsEntryAndReturnsOutput(t *testing.T) {
	f := newFakeScheduler(t)
	data := testBundle(t, map[string]string{"job.bin": "binary", "data.csv": "1,2\n"})
	f.setBundle(data, http.StatusOK)
	f.setNext(serveJob(store.Job{ID: "job-1", Entry: "job.bin", Command: []string{"--fast"}, BundleBytes: int64(len(data))}))

	var gotArgv []string
	var sawData bool
	run := func(ctx context.Context, argv []string, dir string) Result {
		gotArgv = argv
		_, err := os.Stat(filepath.Join(dir, "data.csv"))
		sawData = err == nil
		os.WriteFile(filepath.Join(dir, outputDir, "result.txt"), []byte("done\n"), 0o644)
		return Result{Stdout: "ok\n"}
	}
	a := newTestAgent(f, "node-1", run)
	tickAndWait(a)

	if f.bundleNodes == nil || f.bundleNodes[0] != "node-1" {
		t.Fatalf("bundle fetched as %v, want node-1", f.bundleNodes)
	}
	wantEntry := filepath.Join(f.cacheDir, jobsDir, "job-1", "job.bin")
	if len(gotArgv) != 2 || gotArgv[0] != wantEntry || gotArgv[1] != "--fast" {
		t.Fatalf("argv = %q, want [%s --fast]", gotArgv, wantEntry)
	}
	if !sawData {
		t.Fatal("data.csv was not unpacked before the job ran")
	}

	results := f.posted()
	if len(results) != 1 || results[0].body["exit_code"] != float64(0) {
		t.Fatalf("posted %+v", results)
	}
	files, err := tarball.List(bytes.NewReader(results[0].output))
	if err != nil || len(files) != 1 || files[0].Path != "result.txt" || files[0].Bytes != 5 {
		t.Fatalf("output files = %+v, %v", files, err)
	}
	if _, err := os.Stat(filepath.Join(f.cacheDir, jobsDir, "job-1")); !os.IsNotExist(err) {
		t.Errorf("job folder still there after the result was accepted: %v", err)
	}
}

// A job with no files still gets an out/ folder, and a result with nothing
// in out/ is sent as plain JSON.
func TestPlainJobHasEmptyOutDir(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"x"}}))
	var hadOut bool
	run := func(ctx context.Context, argv []string, dir string) Result {
		info, err := os.Stat(filepath.Join(dir, outputDir))
		hadOut = err == nil && info.IsDir()
		return Result{}
	}
	tickAndWait(newTestAgent(f, "node-1", run))
	if !hadOut {
		t.Fatal("out/ was not created")
	}
	if results := f.posted(); len(results) != 1 || results[0].output != nil {
		t.Fatalf("posted %+v, want one plain result", results)
	}
}

// When the scheduler can't serve the bundle right now, the job is left for
// the next poll instead of being failed.
func TestBundleFetchFailureIsRetried(t *testing.T) {
	f := newFakeScheduler(t)
	f.setBundle(nil, http.StatusInternalServerError)
	f.setNext(serveJob(store.Job{ID: "job-1", Entry: "run.sh", BundleBytes: 10}))
	runner := &FakeRunner{}
	a := newTestAgent(f, "node-1", runner.run)

	tickAndWait(a)
	if runner.count() != 0 || len(f.posted()) != 0 {
		t.Fatalf("ran %d, posted %d; want nothing", runner.count(), len(f.posted()))
	}
	if a.busy.Load() {
		t.Fatal("agent still busy, it would never ask for the job again")
	}

	// A permanent refusal fails the job with the reason.
	f.setBundle(nil, http.StatusNotFound)
	tickAndWait(a)
	results := f.posted()
	if len(results) != 1 || results[0].body["exit_code"] != float64(-1) || !strings.Contains(results[0].body["stderr"].(string), "can't get job files") {
		t.Fatalf("posted %+v", results)
	}
}

// An out/ folder over the limit is left behind with a note on stderr.
func TestOutputOverLimitIsNotReturned(t *testing.T) {
	old := maxOutputBundleBytes
	maxOutputBundleBytes = 100
	t.Cleanup(func() { maxOutputBundleBytes = old })

	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"x"}}))
	run := func(ctx context.Context, argv []string, dir string) Result {
		os.WriteFile(filepath.Join(dir, outputDir, "big.bin"), bytes.Repeat([]byte("abcdefgh"), 4096), 0o644)
		return Result{}
	}
	tickAndWait(newTestAgent(f, "node-1", run))
	results := f.posted()
	if len(results) != 1 || results[0].output != nil || !strings.Contains(results[0].body["stderr"].(string), "over the 0 MB limit") {
		t.Fatalf("posted %+v", results)
	}
}

// The entry's file type picks the interpreter, and a missing entry name
// falls back to the OS's run file.
func TestEntryArgv(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"run.sh", "run.ps1", "crunch.py", "task.cmd", "prog"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	p := func(name string) string { return filepath.Join(dir, name) }
	cases := []struct {
		entry, goos string
		want        []string
	}{
		{"run.sh", "linux", []string{"sh", p("run.sh"), "a"}},
		{"crunch.py", "darwin", []string{"python3", p("crunch.py"), "a"}},
		{"crunch.py", "windows", []string{"python", p("crunch.py"), "a"}},
		{"run.ps1", "windows", []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", p("run.ps1"), "a"}},
		{"task.cmd", "windows", []string{"cmd", "/c", p("task.cmd"), "a"}},
		{"prog", "linux", []string{p("prog"), "a"}},
		{"", "linux", []string{"sh", p("run.sh"), "a"}},
		{"", "windows", []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", p("run.ps1"), "a"}},
	}
	for _, c := range cases {
		got, err := entryArgv(c.entry, dir, []string{"a"}, c.goos)
		if err != nil || strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("entryArgv(%q, %s) = %q, %v; want %q", c.entry, c.goos, got, err, c.want)
		}
	}
	if _, err := entryArgv("missing.sh", dir, nil, "linux"); err == nil || !strings.Contains(err.Error(), "not in the job files") {
		t.Errorf("missing entry err = %v", err)
	}
	if _, err := entryArgv("", t.TempDir(), nil, "windows"); err == nil || !strings.Contains(err.Error(), "run.ps1 or run.cmd") {
		t.Errorf("no default entry err = %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(p("prog")); info.Mode()&0o100 == 0 {
			t.Error("prog was not made executable")
		}
	}
}
