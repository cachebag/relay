package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Next asks the scheduler for the next job assigned to this node. It returns
// nil, nil when there is nothing to do (HTTP 204).
func Next(
	ctx context.Context,
	client *http.Client,
	schedulerURL string,
	nodeID string) (*store.Job, error) {

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		schedulerURL+"/nodes/"+url.PathEscape(nodeID)+"/jobs/next",
		nil)
	if err != nil {
		return nil, err
	}

	// Sends the HTTP request to the scheduler.
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	// Ensures the response body is closed to allow the TCP connection
	// to be reused.
	defer resp.Body.Close()

	// Handle the response based on the status code. HTTP 204 means
	// no job is available.
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
		var j store.Job
		if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
			return nil, err
		}
		return &j, nil
	default:
		return nil, fmt.Errorf("scheduler returned %d", resp.StatusCode)
	}
}

// Report sends a finished job's result to the scheduler.
func Report(
	ctx context.Context,
	client *http.Client,
	schedulerURL string,
	jobID string,
	r Result) error {

	// Marshal the job result into JSON format.
	body, err := json.Marshal(struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}{r.ExitCode, r.Stdout, r.Stderr})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		schedulerURL+"/jobs/"+url.PathEscape(jobID)+"/result",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Sends the HTTP request to the scheduler.
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	// Ensures the response body is closed to allow the TCP connection
	// to be reused.
	defer resp.Body.Close()

	// Check the response status code to determine if the report was successful.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("scheduler returned %d", resp.StatusCode)
	}
	return nil
}

// Polls the scheduler for jobs assigned to nodeID, runs each one with
// runner and reports the result. Jobs run one at a time. A poll or report that
// fails is logged and retried on the next tick: the scheduler keeps handing
// out a running job until its result is stored, so nothing is lost.
func RunJobs(
	ctx context.Context,
	schedulerURL string,
	nodeID string,
	runner Runner,
	interval time.Duration) error {

	client := &http.Client{}
	ticker := time.NewTicker(interval)

	// Stops the ticker when the function exits to prevent resource leaks.
	defer ticker.Stop()

	for {
		// Finish all queued jobs before sleeping.
		for {
			ran, err := runOne(ctx, client, schedulerURL, nodeID, runner)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("job loop: %v", err)
			}
			if !ran || err != nil {
				break
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// runOne fetches and runs a single job. ran is false when no job was waiting.
func runOne(
	ctx context.Context,
	client *http.Client,
	schedulerURL string,
	nodeID string,
	runner Runner) (ran bool, err error) {

	// Request the next job from the scheduler.
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	j, err := Next(pollCtx, client, schedulerURL, nodeID)
	cancel()
	if err != nil {
		return false, fmt.Errorf("fetch job: %w", err)
	}
	if j == nil {
		return false, nil
	}

	// Run the job using the runner.
	res, err := runner.Run(ctx, *j)
	if err != nil {
		// Not reported, so the job stays running and is handed out again.
		return true, fmt.Errorf("run job %s: %w", j.ID, err)
	}

	log.Printf("running job %s: %v", j.ID, j.Command)

	// Report the result of the job to the scheduler.
	reportCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := Report(reportCtx, client, schedulerURL, j.ID, res); err != nil {
		return true, fmt.Errorf("report job %s: %w", j.ID, err)
	}
	log.Printf("job %s finished with exit code %d", j.ID, res.ExitCode)
	return true, nil
}
