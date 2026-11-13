package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/cachebag/relay/internal/api"
	"github.com/cachebag/relay/internal/credit"
	"github.com/cachebag/relay/internal/scheduler"
	"github.com/cachebag/relay/internal/store"
)

// Package level variable that stores relayd's version number.
var Version = "0.0.0"

// Prints relayd's version number
func printVersion(w io.Writer) {
	logger := log.New(w, "", 0)
	logger.Println("relayd", Version)
}

// Returns an error if the port is outside 0-65535. 0 lets the OS pick a free port.
func validPort(p int) error {
	if p < 0 || p > 65535 {
		return fmt.Errorf("invalid port %d, must be between 0 and 65535", p)
	}
	return nil
}

func main() {
	// Creation of version flag
	version := flag.Bool(
		"version",
		false,
		"Prints what version of relayd is being used")

	// Creation of port flag
	port := flag.Int(
		"port",
		8080,
		"Port for relayd to listen on",
	)

	// Replaces the server admin token and exits. This is how a lost token
	// is recovered. It needs relay.db, so only whoever runs relayd can do it.
	newAdminToken := flag.Bool(
		"new-admin-token",
		false,
		"Replace the server admin token, print the new one, and exit",
	)

	// Public mode: an embedded Tailscale node serves the same handler over
	// Funnel, so contributors anywhere can reach relayd without the admin
	// opening a port. Uses TS_AUTHKEY or the CLI's saved key.
	public := flag.Bool(
		"public",
		false,
		"Also serve on a public https address through Tailscale Funnel (uses TS_AUTHKEY or the saved key)",
	)
	name := flag.String(
		"name",
		"relay",
		"Hostname of the Tailscale node used by --public",
	)
	logPath := flag.String(
		"log-file",
		"",
		"Append scheduler output to this file (used by the Windows startup task)",
	)
	configDir := flag.String(
		"config-dir",
		"",
		"Relay directory for saved settings and Tailscale state (default: per-user config)",
	)

	// Makes the 'flag' package inspect cli arguments.
	flag.Parse()
	var output io.Writer = os.Stdout
	var diagnostics io.Writer = os.Stderr
	if *logPath != "" {
		file, err := openSchedulerLog(*logPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "relayd:", err)
			os.Exit(2)
		}
		defer file.Close()
		log.SetOutput(file)
		output, diagnostics = file, file
	}

	var pub publicConfig
	if *public {
		var err error
		key, err := schedulerAuthKey(*configDir)
		if err == nil {
			pub, err = newPublicConfig(*name, key, *configDir)
		}
		if err != nil {
			fmt.Fprintln(diagnostics, "relayd:", err)
			os.Exit(2)
		}
	}

	if err := validPort(*port); err != nil {
		fmt.Fprintln(diagnostics, "relayd:", err)
		os.Exit(2)
	}

	// Tells the printVersion function to print relayd's version number
	// if the --version flag is passed while the program is running.
	if *version {
		printVersion(output)
		return
	}

	// Opens the SQLite database used by nodes, pools, and jobs.
	reg, err := store.NewSQLite("relay.db")
	if err != nil {
		log.Fatal(err)
	}
	defer reg.Close()

	// Creates a SQLite-backed store for pools using the shared database.
	pools, err := store.NewSQLitePools(reg.DB())
	if err != nil {
		log.Fatal(err)
	}

	// Creates a SQLite-backed store for jobs using the shared database.
	jobs, err := store.NewSQLiteJobs(reg.DB())
	if err != nil {
		log.Fatal(err)
	}

	// Creates a SQLite-backed credit ledger using the shared database.
	credits, err := store.NewSQLiteCredits(reg.DB())
	if err != nil {
		log.Fatal(err)
	}

	// The server admin token lives in the same database. On first start
	// one is made and printed; later starts stay quiet.
	server, err := store.NewSQLiteServer(reg.DB())
	if err != nil {
		log.Fatal(err)
	}
	if _, err := ensureAdminToken(context.Background(), server, output, *newAdminToken); err != nil {
		log.Fatal(err)
	}
	if *newAdminToken {
		return
	}

	// Starts listening on the specified port.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("relayd listening on :%d", ln.Addr().(*net.TCPAddr).Port)

	// Starts the scheduler in the background.
	go scheduler.New(reg, jobs, credit.NewEarner(credits)).Run(context.Background(), 2*time.Second)

	var publicURL atomic.Value
	publicURL.Store("")

	// Job files live beside relay.db, one gzipped tar per job in and out.
	bundles, err := store.NewBundles("bundles")
	if err != nil {
		log.Fatal(err)
	}

	handler := api.NewHandler(reg, pools, jobs, credits, server,
		api.WithPublicURL(func() string { return publicURL.Load().(string) }),
		api.WithBundles(bundles),
		api.WithDatabase(reg))

	// With --public, join the tailnet and serve the same handler on the
	// Funnel listener too. A failure here is fatal on purpose: silently
	// running local-only would leave the admin handing out an address
	// nobody can reach.
	if *public {
		pln, url, err := listenPublic(context.Background(), pub)
		if err != nil {
			log.Fatal(err)
		}
		publicURL.Store(url)
		fmt.Fprintf(output, "relayd public address: %s\n", url)
		go func() {
			if err := http.Serve(pln, handler); err != nil {
				log.Fatal(err)
			}
		}()
	}

	// Saves any error as a variable called err, and if an error occurs
	// it will print the error and stop the program.
	if err := http.Serve(ln, handler); err != nil {
		log.Fatal(err)
	}
}
