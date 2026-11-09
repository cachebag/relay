package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MaxBundleBytes caps a job's input bundle and its output bundle, as
// gzipped tars. The CLI refuses a bigger folder before uploading, relayd
// answers 413, and the agent leaves bigger out/ folders behind.
const MaxBundleBytes = 50 << 20

// Bundle kinds: the files a submitter sent with a job, and the files the
// job wrote to out/ that came back with its result.
const (
	BundleInput  = "in"
	BundleOutput = "out"
)

// Bundles keeps job bundles as files in one folder, <id>.tgz for the input
// and <id>.out.tgz for the output, beside relay.db. They stay out of SQLite
// so a 50 MB upload is a file copy, not a database write.
type Bundles struct {
	dir string
}

// NewBundles makes dir if needed and returns a store for it.
func NewBundles(dir string) (*Bundles, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Bundles{dir: dir}, nil
}

// Dir is the folder the bundles are in.
func (b *Bundles) Dir() string { return b.dir }

// path refuses any ID that isn't a plain name, so a job ID can never name a
// file outside the bundles folder.
func (b *Bundles) path(id, kind string) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\:`) {
		return "", fmt.Errorf("bad job id %q", id)
	}
	switch kind {
	case BundleInput:
		return filepath.Join(b.dir, id+".tgz"), nil
	case BundleOutput:
		return filepath.Join(b.dir, id+".out.tgz"), nil
	}
	return "", fmt.Errorf("bad bundle kind %q", kind)
}

// Put saves the bundle read from r and returns how many bytes it holds. It
// writes to a temp file first, so a failed upload never leaves a half
// bundle under the real name.
func (b *Bundles) Put(id, kind string, r io.Reader) (int64, error) {
	dst, err := b.path(id, kind)
	if err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(b.dir, "upload-*")
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return 0, err
	}
	return n, nil
}

// Open returns the bundle for reading and its size, or ErrNotFound.
func (b *Bundles) Open(id, kind string) (*os.File, int64, error) {
	p, err := b.path(id, kind)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// Delete removes both of a job's bundles. Missing ones are not an error.
func (b *Bundles) Delete(id string) error {
	for _, kind := range []string{BundleInput, BundleOutput} {
		p, err := b.path(id, kind)
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
