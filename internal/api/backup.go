package api

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cachebag/relay/internal/store"
)

// serveDatabaseBackup writes an online backup of the live database and
// streams the resulting file to the client.
func serveDatabaseBackup(w http.ResponseWriter, r *http.Request, db *store.SQLite) {
	// SQLite creates the destination with its own file permissions. Keep it
	// inside an owner-only directory so the database never becomes public.
	dir, err := os.MkdirTemp("", "relay-backup-*")
	if err != nil {
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}

	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("could not remove backup temp directory %s: %v", dir, err)
		}
	}()
	path := filepath.Join(dir, "relay.db")

	if err := db.Backup(r.Context(), path); err != nil {
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "backup failed", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("could not close backup temp file %s: %v", path, err)
		}
	}()

	info, err := f.Stat()
	if err != nil {
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
}
