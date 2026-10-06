// Command resetdb is a developer-only tool that deletes the local relayd
// SQLite database so you can start from a clean slate (no nodes, pools, or
// jobs).
//
// It lives under tools/ rather than cmd/ so it is never built into a release
// or exposed through the relay CLI. Run it from the repo root with:
//
//	go run ./tools/resetdb
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
	"time"
)

// SQLite in WAL mode keeps state in these sidecar files next to the database.
var sidecars = []string{"", "-wal", "-shm"}

// Removes the database at path and its WAL/SHM sidecars. Returns the files
// that were actually deleted; missing files are not an error.
func reset(path string) ([]string, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return nil, fmt.Errorf("%s is a directory, not a database", path)
	}

	var removed []string
	for _, suffix := range sidecars {
		p := path + suffix
		if err := os.Remove(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return removed, err
		}
		removed = append(removed, p)
	}
	return removed, nil
}

// Reports whether something is listening on localhost at port, which most
// likely means relayd still has the database open.
func listening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Asks the user to confirm on in/out. Anything other than "y" or "yes" is a no.
func confirm(in io.Reader, out io.Writer, path string) bool {
	fmt.Fprintf(out, "This will permanently delete %s (all nodes, pools, and jobs). Continue? [y/N] ", path)
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func main() {
	db := flag.String("db", "relay.db", "path to the relayd SQLite database")
	port := flag.Int("port", 8080, "relayd port to check before deleting")
	yes := flag.Bool("y", false, "skip the confirmation prompt")
	force := flag.Bool("force", false, "delete even if relayd appears to be running")
	flag.Parse()

	if !*force && listening(*port) {
		fmt.Fprintf(os.Stderr, "resetdb: something is listening on :%d; stop relayd first (or pass -force)\n", *port)
		os.Exit(1)
	}

	if !*yes && !confirm(os.Stdin, os.Stdout, *db) {
		fmt.Println("aborted")
		return
	}

	removed, err := reset(*db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resetdb:", err)
		os.Exit(1)
	}
	if len(removed) == 0 {
		fmt.Printf("nothing to delete, %s does not exist\n", *db)
		return
	}
	for _, p := range removed {
		fmt.Println("deleted", p)
	}
}
