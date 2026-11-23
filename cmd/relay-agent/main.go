package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"github.com/cachebag/relay/internal/agent"
	"github.com/cachebag/relay/internal/store"
)

// Package level variable that stores relay-agent's version number.
var Version = "0.0.0"

// Prints relay-agent's version number.
func printVersion(w io.Writer) {
	logger := log.New(w, "", 0)
	logger.Println("relay-agent", Version)
}

// Prints a summary of the machine and scheduler when relay-agent starts.
// Memory is converted from bytes to gigabytes to make it easier to read.
func printStartup(w io.Writer, n store.Node, scheduler string) {
	logger := log.New(w, "", 0)

	// Converts the node's total memory from bytes to gigabytes.
	memGB := float64(n.MemBytes) / (1024 * 1024 * 1024)

	logger.Printf(
		"relay-agent %s: %s (%s/%s, %d CPUs, %.1f GB) connecting to %s as %s",
		Version,
		n.Hostname,
		n.OS,
		n.Arch,
		n.CPUs,
		memGB,
		scheduler,
		n.Owner,
	)
}

// reportExit prints the reason for a known terminal condition and returns
// its exit code. Administrative removal is a normal shutdown (0), while
// a rejected token remains an error (1).
func reportExit(w io.Writer, err error) (code int, handled bool) {
	switch {
	case errors.Is(err, agent.ErrRemoved):
		fmt.Fprintln(w, agent.RemovedExitMsg)
		return 0, true
	case errors.Is(err, agent.ErrBadToken):
		fmt.Fprintln(w, agent.RejectedExitMsg)
		return 1, true
	default:
		return 0, false
	}
}

// Returns RELAY_SCHEDULER if set, otherwise uses the default scheduler URL.
func schedulerDefault() string {
	if scheduler := os.Getenv("RELAY_SCHEDULER"); scheduler != "" {
		return scheduler
	}
	return "http://localhost:8080"
}

// Returns RELAY_POOL_TOKEN if set, otherwise returns an empty string.
func poolTokenDefault() string {
	return os.Getenv("RELAY_POOL_TOKEN")
}

// The job cache can be overridden without changing the shared node identity.
func cacheDirFlag(fs *flag.FlagSet) *string {
	return fs.String(
		"cache-dir",
		os.Getenv("RELAY_AGENT_DIR"),
		"directory for job folders and unreported results (or set RELAY_AGENT_DIR)",
	)
}

// Returns RELAY_OWNER if set, otherwise the name of the user running the
// agent, or an empty string if that can't be found.
func ownerDefault() string {
	if owner := os.Getenv("RELAY_OWNER"); owner != "" {
		return owner
	}
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return stripDomain(u.Username)
}

// Windows usernames come back as DOMAIN\name, so keep only the name.
func stripDomain(username string) string {
	if i := strings.LastIndex(username, `\`); i >= 0 {
		return username[i+1:]
	}
	return username
}

// Adds the --owner flag to fs. The flag wins over RELAY_OWNER, which wins
// over the OS username.
func ownerFlag(fs *flag.FlagSet) *string {
	return fs.String(
		"owner",
		ownerDefault(),
		"who earns credits for this machine (or set RELAY_OWNER; defaults to your username)",
	)
}

// Prints command-line usage information and an example showing how to join a Relay pool.
func printUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(w, "Usage: relay-agent [options]")
	fmt.Fprintln(w, "       relay-agent leave [--scheduler URL] [--pool-token TOKEN]")
	fmt.Fprintln(w, "       relay-agent uninstall [--scheduler URL --pool-token TOKEN]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Example:")
	fmt.Fprintln(
		w,
		"  relay-agent --scheduler http://localhost:8080 --pool-token <token>",
	)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")

	// PrintDefaults writes to the FlagSet's configured output, so temporarily direct it to the provided writer.
	originalOutput := fs.Output()
	fs.SetOutput(w)
	defer fs.SetOutput(originalOutput)

	fs.PrintDefaults()
}

func main() {
	// `relay-agent leave` takes this machine out of its pool. Anything else
	// runs the agent as before.
	if len(os.Args) > 1 && os.Args[1] == "leave" {
		os.Exit(runLeave(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "uninstall" {
		os.Exit(runUninstall(os.Args[2:], os.Stdout, os.Stderr))
	}

	// Creation of version flag.
	version := flag.Bool(
		"version",
		false,
		"Prints what version of relay-agent is being used",
	)

	// Pointer to command-line 'scheduler' flag.
	scheduler := flag.String(
		"scheduler",
		schedulerDefault(),
		"scheduler base URL",
	)

	// Pointer to command-line 'pool-token' flag.
	poolToken := flag.String(
		"pool-token",
		poolTokenDefault(),
		"join token for the pool (or set RELAY_POOL_TOKEN)",
	)
	// Pointer to command-line 'owner' flag.
	owner := ownerFlag(flag.CommandLine)
	cacheDir := cacheDirFlag(flag.CommandLine)

	// Leaves each job's folder in place after it finishes, for debugging.
	keepWorkdir := flag.Bool(
		"keep-workdir",
		false,
		"keep each job's folder after it finishes and print where it is",
	)

	// For a pool whose jobs need the network or files outside their folder.
	noSandbox := flag.Bool(
		"no-sandbox",
		false,
		"on Linux, run jobs as your own user even when bubblewrap is installed",
	)

	jobCPUs := flag.Int(
		"job-cpus",
		0,
		"CPUs each sandboxed job may use (0 = half of this machine, -1 = no limit)",
	)
	jobMemoryMB := flag.Int(
		"job-memory-mb",
		0,
		"memory in MB each sandboxed job may use (0 = half of this machine, -1 = no limit)",
	)

	// Replaces the default help output with Relay-specific usage information and an example showing how to join a pool.
	flag.Usage = func() {
		printUsage(flag.CommandLine.Output(), flag.CommandLine)
	}

	// Makes the 'flag' package inspect command-line arguments.
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "relay-agent: unknown command %q; the commands are leave and uninstall, run relay-agent -h for options\n", flag.Arg(0))
		os.Exit(2)
	}

	// Prints the version and exits if the --version flag is passed.
	if *version {
		printVersion(os.Stdout)
		return
	}

	// Throws error and exits if pool token is not provided.
	if *poolToken == "" {
		fmt.Fprintln(
			os.Stderr,
			"relay-agent: pool token is required; use --pool-token or set RELAY_POOL_TOKEN",
		)
		os.Exit(2)
	}

	// Throws error and exits if no owner could be found.
	if *owner == "" {
		fmt.Fprintln(
			os.Stderr,
			"relay-agent: owner is required; use --owner or set RELAY_OWNER",
		)
		os.Exit(2)
	}

	// Collects information about the machine running relay-agent.
	node, err := agent.Collect()
	if err != nil {
		log.Fatal(err)
	}
	node.Owner = *owner

	// Displays useful machine and scheduler information when the agent starts.
	printStartup(os.Stdout, node, *scheduler)

	// Cancels the context on Ctrl-C or SIGTERM, which kills a running job
	// and lets Run wait for it before the agent exits.
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// Every 10 seconds, sends a heartbeat to the scheduler, then fetches,
	// runs and reports the node's next job.
	var a *agent.Agent
	if *cacheDir != "" {
		a = agent.NewWithCacheDir(*scheduler, *poolToken, node, *cacheDir)
	} else {
		a = agent.New(*scheduler, *poolToken, node)
	}
	if *keepWorkdir {
		a.KeepWorkdir()
	}
	switch sandbox, err := agent.FindSandbox(); {
	case *noSandbox:
		log.Print("--no-sandbox: jobs will run without a sandbox")
	case err != nil:
		log.Printf("%v, jobs will run without a sandbox", err)
	case sandbox != nil:
		limits := agent.DefaultLimits(node)
		if *jobCPUs != 0 {
			limits.CPUs = max(*jobCPUs, 0)
		}
		if *jobMemoryMB != 0 {
			limits.MemBytes = int64(max(*jobMemoryMB, 0)) << 20
		}
		how := sandbox.SetLimits(limits)
		a.UseSandbox(sandbox)
		log.Print("jobs run in a bubblewrap sandbox: their own folder, no other files of yours and no network")
		log.Print(how)
	}
	if err := a.Run(ctx, 10*time.Second); err != nil && ctx.Err() == nil {
		if code, handled := reportExit(os.Stderr, err); handled {
			if code != 0 {
				os.Exit(code)
			}
			return
		}
		log.Fatal(err)
	}
}
