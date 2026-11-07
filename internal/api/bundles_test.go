package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
	"github.com/cachebag/relay/internal/tarball"
)

// tgz builds a gzipped tar from name -> content.
func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(content))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// postMultipart sends a multipart form with one JSON part and one file part.
func postMultipart(handler http.Handler, path, token, jsonField string, body any, fileField string, file []byte) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	raw, _ := json.Marshal(body)
	mw.WriteField(jsonField, string(raw))
	if file != nil {
		fw, _ := mw.CreateFormFile(fileField, "files.tgz")
		fw.Write(file)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// newBundleHandler is a handler with a bundles folder, the test pool, a
// second pool, and node-1 registered in the test pool.
func newBundleHandler(t *testing.T) (http.Handler, store.JobStore, *store.Bundles) {
	t.Helper()
	ctx := context.Background()
	pools := store.NewMemPools()
	createTestPool(t, pools)
	if err := pools.Create(ctx, store.Pool{ID: "other", Name: "Other", Token: "other-token", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	reg := store.NewMem()
	reg.Upsert(ctx, store.Node{ID: "node-1", Hostname: "node-1", PoolID: "test-pool", LastSeen: time.Now().UTC()})
	jobs := store.NewMemJobs()
	bundles, err := store.NewBundles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(reg, pools, jobs, store.NewMemCredits(), testServer(), WithBundles(bundles))
	return h, jobs, bundles
}

// A job with files is stored with its bundle, served only to the node it
// is on, and hidden from other pools.
func TestJobBundleUploadAndDownload(t *testing.T) {
	h, jobs, _ := newBundleHandler(t)
	data := tgz(t, map[string]string{"run.sh": "#!/bin/sh\necho hi\n", "data.csv": "1,2\n"})

	rec := postMultipart(h, "/jobs", "test-token", "job",
		map[string]any{"command": []string{"--fast"}, "submitter": "alice", "entry": "run.sh"}, "bundle", data)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &created)

	j, _ := jobs.Get(context.Background(), created.ID)
	if j.Entry != "run.sh" || j.BundleBytes != int64(len(data)) || strings.Join(j.Command, " ") != "--fast" {
		t.Fatalf("stored job = %+v", j)
	}

	// GET /jobs/{id} shows the size; the bundle itself needs the node.
	rec = getJSON(h, "/jobs/"+j.ID)
	if !strings.Contains(rec.Body.String(), `"bundle_bytes":`+strconv.Itoa(len(data))) || !strings.Contains(rec.Body.String(), `"entry":"run.sh"`) {
		t.Fatalf("GET job = %s", rec.Body)
	}

	if rec := getJSON(h, "/jobs/"+j.ID+"/bundle?node=node-1"); rec.Code != http.StatusForbidden {
		t.Fatalf("download while queued: %d, want 403", rec.Code)
	}
	j.Status = store.JobAssigned
	j.NodeID = "node-1"
	j.Attempts = 1
	jobs.Update(context.Background(), j)

	if rec := getJSON(h, "/jobs/"+j.ID+"/bundle?node=node-2"); rec.Code != http.StatusForbidden {
		t.Fatalf("download from another node: %d, want 403", rec.Code)
	}
	if rec := getAs(h, "/jobs/"+j.ID+"/bundle?node=node-1", "other-token"); rec.Code != http.StatusNotFound {
		t.Fatalf("download with another pool's token: %d, want 404", rec.Code)
	}
	rec = getJSON(h, "/jobs/"+j.ID+"/bundle?node=node-1")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), data) || rec.Header().Get("Content-Type") != "application/gzip" {
		t.Fatalf("download: %d %s %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
}

// Bad uploads are refused with a reason.
func TestJobBundleRejected(t *testing.T) {
	h, _, _ := newBundleHandler(t)
	ok := tgz(t, map[string]string{"task.py": "print(1)\n"})

	cases := []struct {
		name string
		body map[string]any
		file []byte
		want int
		msg  string
	}{
		{"no entry and no run.*", map[string]any{"submitter": "a"}, ok, 400, "no run.sh"},
		{"entry not in bundle", map[string]any{"submitter": "a", "entry": "run.sh"}, ok, 400, "not in the bundle"},
		{"entry escapes", map[string]any{"submitter": "a", "entry": "../x"}, ok, 400, "relative path"},
		{"entry without bundle", map[string]any{"submitter": "a", "command": []string{"x"}, "entry": "run.sh"}, nil, 400, "needs a bundle"},
		{"not a tarball", map[string]any{"submitter": "a", "entry": "x"}, []byte("hello"), 400, "gzipped tar"},
		{"no command and no files", map[string]any{"submitter": "a"}, nil, 400, "command is required"},
	}
	for _, c := range cases {
		rec := postMultipart(h, "/jobs", "test-token", "job", c.body, "bundle", c.file)
		if rec.Code != c.want || !strings.Contains(rec.Body.String(), c.msg) {
			t.Errorf("%s: %d %q, want %d containing %q", c.name, rec.Code, rec.Body.String(), c.want, c.msg)
		}
	}

	// Too big: just over the limit.
	big := bytes.Repeat([]byte("x"), MaxBundleBytes+1)
	rec := postMultipart(h, "/jobs", "test-token", "job", map[string]any{"submitter": "a", "entry": "x"}, "bundle", big)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "50 MB") {
		t.Fatalf("51 MB upload: %d %s", rec.Code, rec.Body)
	}

	// A scheduler without a bundles folder says so.
	plain, _ := newJobHandler(t)
	rec = postMultipart(plain, "/jobs", "test-token", "job", map[string]any{"submitter": "a", "entry": "task.py"}, "bundle", ok)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("upload without bundles: %d, want 503", rec.Code)
	}

	// The JSON form still works exactly as before.
	if rec := submitTestJob(h, []string{"echo", "hi"}, "alice", ""); rec.Code != http.StatusCreated {
		t.Fatalf("plain JSON submit: %d %s", rec.Code, rec.Body)
	}
}

// A result can carry the job's out/ folder, which is listed on the job and
// served back to the pool.
func TestJobResultWithOutputFiles(t *testing.T) {
	h, jobs, _ := newBundleHandler(t)
	ctx := context.Background()
	j := store.Job{ID: "job-1", Command: []string{"x"}, Status: store.JobRunning, NodeID: "node-1", PoolID: "test-pool", Attempts: 1, CreatedAt: time.Now().UTC()}
	if err := jobs.Create(ctx, j); err != nil {
		t.Fatal(err)
	}

	if rec := getJSON(h, "/jobs/job-1/output/files"); rec.Code != http.StatusNotFound {
		t.Fatalf("files before the result: %d, want 404", rec.Code)
	}

	out := tgz(t, map[string]string{"result.csv": "a,b\n", "plot.png": "PNG"})
	rec := postMultipart(h, "/jobs/job-1/result", "test-token", "result",
		map[string]any{"node_id": "node-1", "attempt": 1, "exit_code": 0, "stdout": "ok\n"}, "output", out)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("result: %d %s", rec.Code, rec.Body)
	}

	got, _ := jobs.Get(ctx, "job-1")
	if got.Status != store.JobDone || got.Stdout != "ok\n" || got.OutputBytes != int64(len(out)) || len(got.OutputFiles) != 2 {
		t.Fatalf("job after result = %+v", got)
	}
	rec = getJSON(h, "/jobs/job-1")
	if !strings.Contains(rec.Body.String(), `"output_files":[`) || !strings.Contains(rec.Body.String(), `"path":"result.csv","bytes":4`) {
		t.Fatalf("GET job = %s", rec.Body)
	}

	rec = getJSON(h, "/jobs/job-1/output/files")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), out) {
		t.Fatalf("output files: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	files, err := tarball.List(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || len(files) != 2 {
		t.Fatalf("served files = %+v, %v", files, err)
	}
	if rec := getAs(h, "/jobs/job-1/output/files", "other-token"); rec.Code != http.StatusNotFound {
		t.Fatalf("other pool: %d, want 404", rec.Code)
	}

	// A stale result with files is refused and stores nothing.
	rec = postMultipart(h, "/jobs/job-1/result", "test-token", "result",
		map[string]any{"node_id": "node-1", "attempt": 1, "exit_code": 1}, "output", tgz(t, map[string]string{"late": "x"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second result: %d, want 409", rec.Code)
	}
	if again, _ := jobs.Get(ctx, "job-1"); again.OutputBytes != int64(len(out)) {
		t.Fatal("stale result replaced the output files")
	}
}

// Results stay JSON-only for agents that send none.
func TestJobResultPlainJSONStillWorks(t *testing.T) {
	h, jobs, _ := newBundleHandler(t)
	j := store.Job{ID: "job-1", Command: []string{"x"}, Status: store.JobRunning, NodeID: "node-1", PoolID: "test-pool", Attempts: 1, CreatedAt: time.Now().UTC()}
	jobs.Create(context.Background(), j)
	if code := postResult(h, "job-1", `{"node_id":"node-1","attempt":1,"exit_code":0}`); code != http.StatusNoContent {
		t.Fatalf("plain result: %d", code)
	}
	got, _ := jobs.Get(context.Background(), "job-1")
	if got.OutputBytes != 0 || got.OutputFiles != nil {
		t.Fatalf("plain result stored output: %+v", got)
	}
}
