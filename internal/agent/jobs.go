package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"

	"github.com/cachebag/relay/internal/store"
)

// errStale means the scheduler answered 409: the job was reassigned (or
// already finished) since this node received it, so the result is not wanted
// and resending it will never succeed.
var errStale = errors.New("result is stale: job was reassigned")

// errRejected means the scheduler refused the result with some other 4xx
// (the job no longer exists, the result is too large, ...). Like errStale,
// resending it will never succeed.
var errRejected = errors.New("result rejected by scheduler")

// Result is what a finished command produced.
type Result struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	// err is set instead of a real result when the job never ran because
	// its files couldn't be fetched yet. It stays out of the JSON.
	err error
}

// resultBody is the body of POST /jobs/{id}/result. NodeID and Attempt echo
// the job as it was handed to this node, which is how the scheduler spots a
// result for work that has since moved to another node.
type resultBody struct {
	NodeID  string `json:"node_id"`
	Attempt int    `json:"attempt"`
	Result
}

// Next asks the scheduler for the next job assigned to this node. It returns
// nil, nil when there is nothing to do (HTTP 204).
func (a *Agent) Next(ctx context.Context) (*store.Job, error) {

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		a.schedulerURL+"/nodes/"+url.PathEscape(a.node.ID)+"/jobs/next",
		nil)
	if err != nil {
		return nil, err
	}

	// The scheduler only hands out a node's jobs to its own pool.
	req.Header.Set("Authorization", "Bearer "+a.poolToken)

	// Sends the HTTP request to the scheduler.
	resp, err := a.client.Do(req)
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
	case http.StatusForbidden:
		return nil, fmt.Errorf("%w: scheduler returned %d", ErrBadToken, resp.StatusCode)
	case http.StatusOK:
		var j store.Job
		if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
			return nil, fmt.Errorf("decoding job: %w", err)
		}
		return &j, nil
	default:
		return nil, fmt.Errorf("scheduler returned %d", resp.StatusCode)
	}
}

// Report sends a finished job's result to the scheduler. With outputPath,
// the tar of the job's out/ folder goes along in a multipart form; if that
// file is gone the result is sent without it.
func (a *Agent) Report(
	ctx context.Context,
	job store.Job,
	rb resultBody,
	outputPath string) error {

	// Marshal the job result into JSON format.
	body, err := json.Marshal(resultBody{
		NodeID:  rb.NodeID,
		Attempt: rb.Attempt,
		Result:  rb.Result,
	})
	if err != nil {
		return err
	}

	var output *os.File
	if outputPath != "" {
		if output, err = os.Open(outputPath); err != nil {
			log.Printf("output files for job %s are gone, sending the result without them: %v", job.ID, err)
		} else {
			defer output.Close()
		}
	}

	client := a.client
	var reqBody io.Reader = bytes.NewReader(body)
	contentType := "application/json"
	if output != nil {
		var form bytes.Buffer
		mw := multipart.NewWriter(&form)
		if err := mw.WriteField("result", string(body)); err != nil {
			return err
		}
		fw, err := mw.CreateFormFile("output", "out.tgz")
		if err != nil {
			return err
		}
		if _, err := io.Copy(fw, output); err != nil {
			return err
		}
		if err := mw.Close(); err != nil {
			return err
		}
		reqBody = &form
		contentType = mw.FormDataContentType()
		client = a.transferClient()
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.schedulerURL+"/jobs/"+url.PathEscape(job.ID)+"/result",
		reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)

	// Results are only accepted from the job's own pool.
	req.Header.Set("Authorization", "Bearer "+a.poolToken)

	// Sends the HTTP request to the scheduler.
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	// Ensures the response body is closed to allow the TCP connection
	// to be reused.
	defer resp.Body.Close()

	// Check the response status code to determine if the report was successful.
	switch code := resp.StatusCode; {
	case code == http.StatusNoContent:
		return nil
	case code == http.StatusConflict:
		return errStale
	case code == http.StatusForbidden:
		return fmt.Errorf("%w: scheduler returned %d", ErrBadToken, code)
	case code >= 400 && code < 500:
		return fmt.Errorf("%w: scheduler returned %d", errRejected, code)
	default:
		return fmt.Errorf("scheduler returned %d", code)
	}
}
