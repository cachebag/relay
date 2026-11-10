// Package tarball packs, lists and unpacks the gzipped tar files that carry a
// job's input files to a node and its output files back.
package tarball

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// File is one regular file inside a tarball.
type File struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// ErrBadPath means an entry would land outside the folder it is unpacked
// into, so the whole tarball is refused.
var ErrBadPath = errors.New("tarball entry has a bad path")

// ErrTooBig means unpacking would write more than the caller allowed.
var ErrTooBig = errors.New("tarball unpacks to more than the limit")

// cleanPath turns a tar entry name into a relative slash path with no "..",
// or reports it as bad. "./a/b/" becomes "a/b".
func cleanPath(name string) (string, bool) {
	name = strings.TrimPrefix(name, "./")
	p := path.Clean(name)
	if p == "." || p == "" || path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsRune(p, '\\') {
		return "", false
	}
	// A drive letter would make this absolute on Windows.
	if len(p) > 1 && p[1] == ':' {
		return "", false
	}
	return p, true
}

// Pack writes the contents of root to w as a gzipped tar. Paths inside the
// tarball are relative to root with forward slashes, so "root/out/a.csv"
// becomes "out/a.csv". Symlinks and anything that isn't a plain file or
// folder are skipped.
func Pack(w io.Writer, root string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: 0o755, ModTime: info.ModTime()})
		case info.Mode().IsRegular():
			hdr := &tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime()}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.CopyN(tw, f, info.Size())
			return err
		default:
			return nil
		}
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// List reads a gzipped tar and returns its regular files, in order. It
// refuses a tarball with an entry that would unpack outside its folder.
func List(r io.Reader) ([]File, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("not a gzipped tar: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := []File{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}
		p, ok := cleanPath(hdr.Name)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrBadPath, hdr.Name)
		}
		if hdr.Typeflag == tar.TypeReg {
			files = append(files, File{Path: p, Bytes: hdr.Size})
		}
	}
}

// Unpack extracts a gzipped tar into dir, which must already exist. Only
// plain files and folders are written; symlinks and devices are skipped.
// It stops with ErrTooBig once more than limit bytes of file content have
// been written, so a tiny tarball can't fill the disk.
func Unpack(r io.Reader, dir string, limit int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("not a gzipped tar: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var written int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}
		p, ok := cleanPath(hdr.Name)
		if !ok {
			return fmt.Errorf("%w: %q", ErrBadPath, hdr.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if written += hdr.Size; written > limit {
				return ErrTooBig
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// Keep the file's own permissions (so run.sh stays executable)
			// but make sure the agent can always read and write it.
			mode := fs.FileMode(hdr.Mode).Perm() | 0o600
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, hdr.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}
