package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Marks a re-exec of the test binary as the helper process that
// the real-runner tests run as their "command". Using the test binary itself
// avoids depending on echo or sh, which Windows doesn't have.
const helperEnv = "RELAY_AGENT_TEST_HELPER"

// Checks if the helper environment variable is set and runs the `helperMain` function if it is.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(helperMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// Performs its arguments in order:
//
// out:TEXT       write TEXT to stdout
// err:TEXT       write TEXT to stderr
// outn:N         write N bytes to stdout
// errn:N         write N bytes to stderr
// exit:N         exit with status N
// sleep:N        sleep for N milliseconds
// spawnchild:P   spawn a child that writes a marker at path P
// markafter:P    wait two seconds, then write a marker at path P
func helperMain(args []string) int {
	for _, arg := range args {
		op, val, _ := strings.Cut(arg, ":")
		switch op {
		case "out":
			fmt.Fprint(os.Stdout, val)

		case "err":
			fmt.Fprint(os.Stderr, val)

		// Handle output truncation for large streams.
		case "outn", "errn":
			n, _ := strconv.Atoi(val)
			f := os.Stdout
			if op == "errn" {
				f = os.Stderr
			}
			f.WriteString(strings.Repeat("x", n))

		case "exit":
			n, _ := strconv.Atoi(val)
			return n

		case "sleep":
			n, _ := strconv.Atoi(val)
			time.Sleep(time.Duration(n) * time.Millisecond)

		case "spawnchild":
			// A child that writes a marker two seconds later. Its parent
			// stays alive, letting timeout tests verify tree termination.
			child := exec.Command(os.Args[0], "markafter:"+val)
			if err := child.Start(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 99
			}

			_ = child.Process.Release()
			fmt.Fprintln(os.Stdout, "spawned")
			time.Sleep(time.Minute)

		case "markafter":
			time.Sleep(2 * time.Second)

			if err := os.WriteFile(
				val,
				[]byte("child survived"),
				0600,
			); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 99
			}
		}
	}

	return 0
}

// Runs the real runner against the helper process.
func runHelper(t *testing.T, args ...string) Result {
	t.Helper()
	t.Setenv(helperEnv, "1")

	return RunCommand(
		context.Background(),
		append([]string{os.Args[0]}, args...),
	)
}

// Verifies that stdout and stderr are captured separately
// by the RunCommand function.
func TestStreamsCapturedSeparately(t *testing.T) {
	res := runHelper(t, "out:to stdout", "err:to stderr")

	if res.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", res.ExitCode)
	}

	if res.Stdout != "to stdout" {
		t.Errorf(
			"stdout = %q, want %q",
			res.Stdout,
			"to stdout",
		)
	}

	if res.Stderr != "to stderr" {
		t.Errorf(
			"stderr = %q, want %q",
			res.Stderr,
			"to stderr",
		)
	}
}

// Tests that `RunCommand` correctly reports the exit code of the executed command.
func TestRunCommandReportsExitCode(t *testing.T) {
	res := runHelper(t, "out:partial", "err:boom", "exit:7")

	if res.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", res.ExitCode)
	}

	if res.Stdout != "partial" || res.Stderr != "boom" {
		t.Errorf(
			"output = %q / %q, want it kept on a non-zero exit",
			res.Stdout,
			res.Stderr,
		)
	}
}

// Tests that `RunCommand` correctly handles a missing executable.
func TestRunCommandMissingExecutable(t *testing.T) {
	const name = "relay-no-such-executable"

	res := RunCommand(
		context.Background(),
		[]string{name, "arg"},
	)

	if res.ExitCode != -1 {
		t.Errorf("exit code = %d, want -1", res.ExitCode)
	}

	if !strings.Contains(res.Stderr, name) {
		t.Errorf(
			"stderr = %q, want the error text naming %q",
			res.Stderr,
			name,
		)
	}

	if res.Stdout != "" {
		t.Errorf("stdout = %q, want empty", res.Stdout)
	}
}

// Tests that `RunCommand` correctly handles an empty command.
func TestRunCommandEmptyCommand(t *testing.T) {
	res := RunCommand(context.Background(), nil)

	if res.ExitCode != -1 || res.Stderr == "" {
		t.Errorf(
			"got %+v, want exit code -1 with an error in stderr",
			res,
		)
	}
}

// Tests that `RunCommand` correctly truncates output that exceeds the maximum allowed length.
func TestRunCommandTruncatesOutput(t *testing.T) {
	const over = maxOutput + 100

	// Tests that `RunCommand` correctly truncates stdout when it exceeds the maximum allowed length.
	t.Run("stdout", func(t *testing.T) {
		res := runHelper(
			t,
			fmt.Sprintf("outn:%d", over),
			"err:small",
		)

		want := strings.Repeat("x", maxOutput) + truncatedMarker

		if res.Stdout != want {
			t.Errorf(
				"stdout is %d bytes (suffix %q), want first %d bytes plus marker",
				len(res.Stdout),
				tail(res.Stdout),
				maxOutput,
			)
		}

		if res.Stderr != "small" {
			t.Errorf(
				"stderr = %q, want it untouched",
				res.Stderr,
			)
		}
	})

	// Tests that `RunCommand` correctly truncates stderr when it exceeds the maximum allowed length.
	t.Run("stderr", func(t *testing.T) {
		res := runHelper(
			t,
			fmt.Sprintf("errn:%d", over),
			"out:small",
		)

		want := strings.Repeat("x", maxOutput) + truncatedMarker

		if res.Stderr != want {
			t.Errorf(
				"stderr is %d bytes (suffix %q), want first %d bytes plus marker",
				len(res.Stderr),
				tail(res.Stderr),
				maxOutput,
			)
		}

		if res.Stdout != "small" {
			t.Errorf(
				"stdout = %q, want it untouched",
				res.Stdout,
			)
		}
	})

	// Tests that `RunCommand` does not truncate output that is exactly at the maximum allowed length.
	t.Run("exactly at the cap is not truncated", func(t *testing.T) {
		res := runHelper(
			t,
			fmt.Sprintf("outn:%d", maxOutput),
		)

		if len(res.Stdout) != maxOutput ||
			strings.Contains(res.Stdout, "truncated") {
			t.Errorf(
				"stdout is %d bytes, want exactly %d with no marker",
				len(res.Stdout),
				maxOutput,
			)
		}
	})
}

// tail returns the last 30 characters of the given string, or the entire string if it is shorter than 30 characters.
func tail(s string) string {
	if len(s) > 30 {
		return s[len(s)-30:]
	}

	return s
}
