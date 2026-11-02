package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

func TestJobTimeoutRoundTrip(t *testing.T) {
	pools := store.NewMemPools()
	createTestPool(t, pools)

	handler := NewHandler(
		store.NewMem(),
		pools,
		store.NewMemJobs(),
		store.NewMemCredits(),
		testServer(),
	)

	tests := []struct {
		name    string
		field   string
		seconds int
	}{
		{"default", "", store.DefaultJobTimeoutSeconds},
		{"one second", `,"timeout_seconds":1`, 1},
		{"custom", `,"timeout_seconds":600`, 600},
		{"maximum", `,"timeout_seconds":86400`, store.MaxJobTimeoutSeconds},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"command":["echo","hi"],"submitter":"alice"` +
				tc.field + `}`

			req := httptest.NewRequest(
				http.MethodPost,
				"/jobs",
				strings.NewReader(body),
			)
			req.Header.Set("Authorization", "Bearer test-token")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusCreated {
				t.Fatalf("POST /jobs = %d: %s",
					rec.Code, rec.Body.String())
			}

			var created struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}

			got := getJSON(handler, "/jobs/"+created.ID)
			if got.Code != http.StatusOK {
				t.Fatalf("GET job = %d: %s",
					got.Code, got.Body.String())
			}

			var job store.Job
			if err := json.Unmarshal(got.Body.Bytes(), &job); err != nil {
				t.Fatal(err)
			}

			if job.TimeoutSeconds != tc.seconds {
				t.Fatalf("timeout_seconds = %d, want %d",
					job.TimeoutSeconds, tc.seconds)
			}
		})
	}
}

func TestJobTimeoutRejectsInvalidValues(t *testing.T) {
	pools := store.NewMemPools()
	createTestPool(t, pools)

	jobs := store.NewMemJobs()

	handler := NewHandler(
		store.NewMem(),
		pools,
		jobs,
		store.NewMemCredits(),
		testServer(),
	)

	tests := []struct {
		name  string
		value string
	}{
		{"zero", "0"},
		{"negative", "-1"},
		{"above maximum", "86401"},
		{"fractional", "1.5"},
		{"string", `"600"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"command":["echo","hi"],"submitter":"alice","timeout_seconds":` +
				tc.value + `}`

			req := httptest.NewRequest(
				http.MethodPost,
				"/jobs",
				strings.NewReader(body),
			)
			req.Header.Set("Authorization", "Bearer test-token")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /jobs = %d, want 400: %s",
					rec.Code, rec.Body.String())
			}
		})
	}

	listed, err := jobs.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("invalid submissions created %d jobs", len(listed))
	}
}
