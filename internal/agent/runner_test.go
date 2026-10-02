package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Re-runs the test binary as a stand-in for the docker CLI, driven
// by FAKE_DOCKER_MODE. 'calls' records the arguments of every invocation.
func fakeDocker(mode string, calls *[][]string) func(context.Context, ...string) *exec.Cmd {

	// Protects access to the 'calls' slice.
	var mu sync.Mutex

	// Return a function that simulates the docker command
	// based on the specified mode.
	return func(ctx context.Context, args ...string) *exec.Cmd {

		// Lock the mutex to safely record the invocation.
		mu.Lock()
		// Record the current invocation's arguments.
		*calls = append(*calls, slices.Clone(args))

		// Unlock the mutex after recording the invocation.
		mu.Unlock()

		// Determine the mode to use for the fake docker command.
		// If the first argument is not "run", default to "ok".
		m := mode
		if args[0] != "run" {
			m = "ok"
		}
		// Create the command that will simulate the docker execution based on
		// the determined mode and set the environment variables to indicate
		// the fake docker helper and mode.
		c := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=TestHelperProcess")
		c.Env = append(
			os.Environ(),
			"FAKE_DOCKER_HELPER=1",
			"FAKE_DOCKER_MODE="+m)
		return c
	}
}

// 'TestHelperProcess' is a helper process used to simulate the behavior of the docker CLI
// for testing purposes. It is controlled by the FAKE_DOCKER_MODE environment variable.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("FAKE_DOCKER_HELPER") != "1" {
		return
	}

	// Ensures that the test binary is only run as the fake docker helper.
	switch os.Getenv("FAKE_DOCKER_MODE") {

	// Mode "ok" simulates a successful docker run with output on stdout and stderr.
	case "ok":
		os.Stdout.WriteString("hello")
		os.Stderr.WriteString("warn")
		os.Exit(0)

	// Mode "fail" simulates a docker run that exits with a non-zero status.
	case "fail":
		os.Stderr.WriteString("boom")
		os.Exit(3)

	// Mode "hang" simulates a docker run that hangs indefinitely.
	case "hang":
		time.Sleep(time.Minute)

	// Mode "big" simulates a docker run that produces a large amount of output.
	case "big":
		os.Stdout.WriteString(strings.Repeat("x", maxOutputBytes+100))
		os.Exit(0)
	}
	os.Exit(0)
}

// Verifies that the DockerRunner constructs the correct command-line arguments for a given job.
func TestRunArgs(t *testing.T) {

	// Initialize the DockerRunner with the default image and no network.
	d := NewDockerRunner("alpine:3.20", "")

	// Construct the command-line arguments for the job.
	got := d.runArgs(store.Job{ID: "j1", Command: []string{"echo", "hi there", "--flag"}})

	// Define the expected command-line arguments for the job.
	want := []string{"run", "--rm", "--name", "relay-job-j1", "--network", "none",
		"alpine:3.20", "echo", "hi there", "--flag"}

	// Compare the actual command-line arguments with the expected ones.
	if !slices.Equal(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}

	// Update the network setting and verify the command-line arguments reflect the change.
	d.Network = "bridge"
	if got := d.runArgs(store.Job{ID: "j1", Command: []string{"true"}}); got[5] != "bridge" {
		t.Fatalf("network = %q, want bridge", got[5])
	}
}

// Tests the Run method of DockerRunner under various scenarios.
// It covers successful execution, non-zero exit codes, agent shutdown, and output capping.
func TestDockerRunnerRun(t *testing.T) {
	job := store.Job{ID: "j1", Command: []string{"echo", "hi"}}

	// Test the successful execution of a job.
	t.Run("success", func(t *testing.T) {
		var calls [][]string

		// Initialize the DockerRunner with the fake docker command.
		d := NewDockerRunner("img", "")
		d.command = fakeDocker("ok", &calls)

		// Run the job using the DockerRunner.
		res, err := d.Run(context.Background(), job)
		if err != nil {
			t.Fatal(err)
		}

		// Verify the result of the successful execution.
		if res.ExitCode != 0 || res.Stdout != "hello" || res.Stderr != "warn" {
			t.Fatalf("res = %+v", res)
		}
		// Verify that the docker command was invoked exactly once with the "run" argument.
		if len(calls) != 1 || calls[0][0] != "run" {
			t.Fatalf("calls = %v", calls)
		}
	})

	// Test the behavior when the docker command exits with a non-zero status.
	// Non-zero status means the job failed.
	t.Run("non-zero exit", func(t *testing.T) {
		var calls [][]string

		// Initialize the DockerRunner with the fake docker command.
		d := NewDockerRunner("img", "")
		d.command = fakeDocker("fail", &calls)

		// Run the job using the DockerRunner.
		res, err := d.Run(context.Background(), job)
		if err != nil {
			t.Fatal(err)
		}

		// Verify the result of the non-zero exit code.
		if res.ExitCode != 3 || res.Stderr != "boom" {
			t.Fatalf("res = %+v", res)
		}
	})

	// Test that an agent shutdown does not produce a job result.
	t.Run("agent shutdown is not a result", func(t *testing.T) {
		var calls [][]string

		// Initialize the DockerRunner with the fake docker command.
		d := NewDockerRunner("img", "")
		d.command = fakeDocker("hang", &calls)

		// Run the job with a context that will timeout quickly to simulate an agent shutdown.
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)

		// Ensure the context is canceled after the test to clean up resources.
		defer cancel()

		// Attempt to run the job and expect an error due to the agent shutdown.
		if _, err := d.Run(ctx, job); err == nil {
			t.Fatal("expected error on agent shutdown")
		}
	})

	// Test that the output of a job is capped to a maximum size.
	t.Run("output is capped", func(t *testing.T) {
		var calls [][]string

		// Initialize the DockerRunner with the fake docker command.
		d := NewDockerRunner("img", "")
		d.command = fakeDocker("big", &calls)

		// Run the job using the DockerRunner.
		res, err := d.Run(context.Background(), job)
		if err != nil {
			t.Fatal(err)
		}

		// Verify that the output contains the truncation notice.
		// An output containing the truncation notice is an output that has been capped.
		if !strings.HasSuffix(res.Stdout, "[output truncated]") || len(res.Stdout) > maxOutputBytes+64 {
			t.Fatalf("stdout len = %d", len(res.Stdout))
		}
	})

	// Test that the DockerRunner correctly handles the case when the Docker binary is missing.
	t.Run("docker missing", func(t *testing.T) {

		// Initialize the DockerRunner with a command that simulates a missing Docker binary.
		d := NewDockerRunner("img", "")
		d.command = func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "relay-no-such-binary")
		}

		// Run the job using the DockerRunner.
		res, err := d.Run(context.Background(), job)
		if err != nil {
			t.Fatal(err)
		}

		// Verify that the exit code indicates that the Docker binary could not be started.
		if res.ExitCode != exitNoStart {
			t.Fatalf("exit = %d, want %d", res.ExitCode, exitNoStart)
		}
	})

	// Test that an empty command fails the job.
	t.Run("empty command fails the job", func(t *testing.T) {

		// Initialize the DockerRunner with an empty command to simulate a failure.
		d := NewDockerRunner("img", "")

		// Run the job using the DockerRunner with an empty command and verify
		// the job fails as expected.
		res, err := d.Run(context.Background(), store.Job{ID: "j2"})
		if err != nil || res.ExitCode == 0 {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
	})
}

// 'stubRunner' is a simple implementation of a runner that returns a predefined result and error.
type stubRunner struct {
	res Result
	err error
}

// Run executes the stub runner and returns the predefined result and error.
func (s stubRunner) Run(context.Context, store.Job) (Result, error) { return s.res, s.err }

// TestRunJobsReportsResult verifies that 'RunJobs' correctly reports the result of a job to the server.
func TestRunJobsReportsResult(t *testing.T) {
	var mu sync.Mutex
	served := false
	var reported struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
	}

	// Channel to signal when the result has been reported.
	done := make(chan struct{})

	// Create a new HTTP test server to simulate the server behavior.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Lock the mutex and defer the unlock to ensure that access
		// to shared variables is synchronized and that the mutex
		// is released after handling the request.
		mu.Lock()

		// Defer the unlock to ensure the mutex is released after handling the request.
		defer mu.Unlock()

		// Handle the incoming HTTP request based on its URL path and method.
		switch {

		// Check the URL path and method to determine how to respond to the request.
		case r.URL.Path == "/nodes/node-1/jobs/next":

			// If a job has already been served, respond with no content.
			if served {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			served = true

			// Mark that a job has been served to ensure subsequent requests receive no content.
			json.NewEncoder(w).Encode(store.Job{ID: "j1", Command: []string{"echo"}})

		// Handle the result submission for the job.
		case r.URL.Path == "/jobs/j1/result" && r.Method == http.MethodPost:

			// Decode the reported result from the request body
			// and respond with no content to acknowledge
			// the result submission.
			json.NewDecoder(r.Body).Decode(&reported)
			w.WriteHeader(http.StatusNoContent)
			close(done)
		default:
			http.NotFound(w, r)
		}
	}))

	// Ensure the test server closes when the test finishes.
	defer srv.Close()

	// Create a context with cancellation to control the lifetime of the job runner.
	ctx, cancel := context.WithCancel(context.Background())

	// Ensure the context is canceled when the test finishes to clean up any running jobs.
	defer cancel()

	// Start the job runner in a separate goroutine to allow it to run concurrently with the test.
	go RunJobs(ctx, srv.URL, "node-1", stubRunner{res: Result{ExitCode: 7, Stdout: "out"}}, 10*time.Millisecond)

	// Wait for the result to be reported or for the timeout to occur.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("result was never reported")
	}
	// Acquire the mutex to safely check the reported result.
	mu.Lock()
	defer mu.Unlock()

	// Check that the reported result matches the expected values.
	if reported.ExitCode != 7 || reported.Stdout != "out" {
		t.Fatalf("reported = %+v", reported)
	}
}

// Verifies that the Next function correctly handles the case when no job is available.
func TestNextNoJob(t *testing.T) {

	// Create a test server that always responds with no content to simulate no available jobs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Ensure the test server closes when the test finishes.
	defer srv.Close()

	// Attempt to fetch the next job, which should return no job available.
	j, err := Next(context.Background(), srv.Client(), srv.URL, "n")
	if err != nil || j != nil {
		t.Fatalf("j = %v, err = %v", j, err)
	}
}
