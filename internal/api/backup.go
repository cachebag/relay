package api

import (
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/cachebag/relay/internal/store"
)

// serveDatabaseBackup writes an online backup of the live database and
// streams the resulting file to the client.
func serveDatabaseBackup(w http.ResponseWriter, r *http.Request, db *store.SQLite) {
	var tmp *os.File
	tmp, err := os.CreateTemp(os.TempDir(), "relay-backup-*")
	if err != nil {
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}

	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(path)
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}

	os.Remove(path)

	if err := db.Backup(r.Context(), path); err != nil {
		os.Remove(path)
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		os.Remove(path)
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(path)
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if r.Method != http.MethodHead {
		if _, err := io.Copy(w, f); err != nil {
			log.Printf("could not stream backup file %s: %v", path, err)
		}
	}

	if err := f.Close(); err != nil {
		os.Remove(path)
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("could not remove backup temp file %s: %v", path, err)
	}
}
