package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/cachebag/relay/internal/store"
	"github.com/cachebag/relay/internal/tarball"
)

// MaxBundleBytes is store.MaxBundleBytes; bigger uploads get 413 with the
// limit in the message.
const MaxBundleBytes = store.MaxBundleBytes

// maxJSONPartBytes caps the JSON part of a multipart upload, which only
// holds the command line and a few fields.
const maxJSONPartBytes = 1 << 20

// errBundleTooBig is answered as 413.
var errBundleTooBig = fmt.Errorf("files must be %d MB or less", MaxBundleBytes>>20)

// isMultipart reports whether the request body is a multipart form, which
// is how a job's files travel alongside its JSON.
func isMultipart(r *http.Request) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && ct == "multipart/form-data"
}

// readJSONWithFile reads either a plain JSON body into v, or a multipart
// form whose jsonField part is the JSON and whose fileField part is a
// gzipped tar. The file is nil when there isn't one. The caller closes it.
// A file over MaxBundleBytes is reported as errBundleTooBig; the file is
// never read into memory whole.
func readJSONWithFile(w http.ResponseWriter, r *http.Request, jsonField, fileField string, v any) (multipart.File, error) {
	if !isMultipart(r) {
		return nil, json.NewDecoder(r.Body).Decode(v)
	}
	// One extra MB leaves room for the JSON part and the form boundaries.
	r.Body = http.MaxBytesReader(w, r.Body, MaxBundleBytes+maxJSONPartBytes)
	// Parts over 32 MB are spooled to a temp file instead of memory.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, errBundleTooBig
		}
		return nil, fmt.Errorf("invalid multipart form: %w", err)
	}
	raw := r.FormValue(jsonField)
	if raw == "" {
		return nil, fmt.Errorf("missing %s part", jsonField)
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return nil, err
	}
	f, hdr, err := r.FormFile(fileField)
	if errors.Is(err, http.ErrMissingFile) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if hdr.Size > MaxBundleBytes {
		f.Close()
		return nil, errBundleTooBig
	}
	return f, nil
}

// validEntry accepts a relative slash path inside the bundle, like run.sh or
// bin/crunch.py, and refuses anything that could point outside it.
func validEntry(entry string) bool {
	if entry == "" {
		return true
	}
	clean := path.Clean(entry)
	return clean == entry && !path.IsAbs(entry) && clean != "." && clean != ".." &&
		!strings.HasPrefix(clean, "../") && !strings.ContainsRune(entry, '\\')
}

// listBundle reads the files in an uploaded bundle and rewinds it, so it can
// be checked before it is stored.
func listBundle(f io.ReadSeeker) ([]tarball.File, error) {
	files, err := tarball.List(f)
	if err != nil {
		return nil, err
	}
	_, err = f.Seek(0, io.SeekStart)
	return files, err
}

// hasFile reports whether name is one of the regular files in files.
func hasFile(files []tarball.File, name string) bool {
	for _, f := range files {
		if f.Path == name {
			return true
		}
	}
	return false
}

// defaultEntries are what the agent runs when the submitter named no entry,
// whichever one its OS can run.
var defaultEntries = []string{"run.sh", "run.ps1", "run.cmd"}

// hasDefaultEntry reports whether the bundle has at least one of them.
func hasDefaultEntry(files []tarball.File) bool {
	for _, name := range defaultEntries {
		if hasFile(files, name) {
			return true
		}
	}
	return false
}

// serveBundle sends a stored bundle as a gzipped tar download.
func serveBundle(w http.ResponseWriter, r *http.Request, bundles *store.Bundles, id, kind string) {
	f, size, err := bundles.Open(id, kind)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "job has no files", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if r.Method != http.MethodHead {
		io.Copy(w, f)
	}
}

// toOutputFiles converts a tar listing into what the job record stores.
func toOutputFiles(files []tarball.File) []store.OutputFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]store.OutputFile, len(files))
	for i, f := range files {
		out[i] = store.OutputFile{Path: f.Path, Bytes: f.Bytes}
	}
	return out
}
