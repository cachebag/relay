package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/scheduler"
	"github.com/cachebag/relay/internal/store"
)

// Credit priority must survive assignment and agent pickup, and waiting jobs
// must stay queued until the node finishes its current job.
func TestCreditPrioritySurvivesNodePickup(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Second)
			var reg store.Registry = store.NewMem()
			var pools store.PoolStore = store.NewMemPools()
			var jobs store.JobStore = store.NewMemJobs()
			var credits store.CreditStore = store.NewMemCredits()
			if backend == "sqlite" {
				db, err := store.NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Close() })
				reg = db
				pools, err = store.NewSQLitePools(db.DB())
				if err != nil {
					t.Fatal(err)
				}
				jobs, err = store.NewSQLiteJobs(db.DB())
				if err != nil {
					t.Fatal(err)
				}
				credits, err = store.NewSQLiteCredits(db.DB())
				if err != nil {
					t.Fatal(err)
				}
			}

			pool := createTestPool(t, pools)
			if err := reg.Upsert(ctx, store.Node{ID: "only-node", PoolID: pool.ID, LastSeen: now}); err != nil {
				t.Fatal(err)
			}
			for _, j := range []store.Job{
				{ID: "older-low", PoolID: pool.ID, Submitter: "low", Command: []string{"echo", "low"}, Status: store.JobQueued, CreatedAt: now.Add(-time.Minute), TimeoutSeconds: 3600},
				{ID: "newer-high", PoolID: pool.ID, Submitter: "high", Command: []string{"echo", "high"}, Status: store.JobQueued, CreatedAt: now, TimeoutSeconds: 3600},
			} {
				if err := jobs.Create(ctx, j); err != nil {
					t.Fatal(err)
				}
			}
			for _, e := range []store.CreditEntry{
				{ID: "low-credit", PoolID: pool.ID, Account: "low", Delta: -5, CreatedAt: now},
				{ID: "high-credit", PoolID: pool.ID, Account: "high", Delta: 10, CreatedAt: now},
			} {
				if err := credits.Add(ctx, e); err != nil {
					t.Fatal(err)
				}
			}

			handler := NewHandler(reg, pools, jobs, credits, testServer())
			checkJob := func(id string, status store.JobStatus, wantPosition int) {
				t.Helper()
				rec := getJSON(handler, "/jobs/"+id)
				if rec.Code != http.StatusOK {
					t.Fatalf("GET job %s: %d %s", id, rec.Code, rec.Body.String())
				}
				var got struct {
					store.Job
					Position *int `json:"position"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Status != status {
					t.Fatalf("job %s status = %s, want %s", id, got.Status, status)
				}
				if status == store.JobQueued {
					if got.Position == nil || *got.Position != wantPosition {
						t.Fatalf("job %s position = %v, want %d", id, got.Position, wantPosition)
					}
					if got.NodeID != "" || got.Attempts != 0 {
						t.Fatalf("waiting job %s was placed: node=%q attempts=%d", id, got.NodeID, got.Attempts)
					}
				} else if got.Position != nil {
					t.Fatalf("job %s has a position while %s", id, status)
				}
			}
			pickup := func(wantID string) {
				t.Helper()
				rec := getJSON(handler, "/nodes/only-node/jobs/next")
				if rec.Code != http.StatusOK {
					t.Fatalf("pickup: %d %s", rec.Code, rec.Body.String())
				}
				var got store.Job
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.ID != wantID || got.Status != store.JobRunning || got.Attempts != 1 {
					t.Fatalf("pickup = %s (%s, attempt %d), want %s (running, attempt 1)", got.ID, got.Status, got.Attempts, wantID)
				}
			}
			s := scheduler.New(reg, jobs, nil, credits)
			tick := func() {
				t.Helper()
				if err := s.Tick(ctx, now); err != nil {
					t.Fatal(err)
				}
			}

			checkJob("older-low", store.JobQueued, 1)
			tick()
			// A second tick before pickup must not stack jobs on the node.
			tick()
			pickup("newer-high")
			checkJob("newer-high", store.JobRunning, 0)
			checkJob("older-low", store.JobQueued, 0)
			// Running work also occupies the node, and a repeated pickup
			// must return the same running job.
			tick()
			pickup("newer-high")
			checkJob("older-low", store.JobQueued, 0)

			if code := postResult(handler, "newer-high", `{"node_id":"only-node","attempt":1,"exit_code":0}`); code != http.StatusNoContent {
				t.Fatalf("finish high-priority job: status %d", code)
			}
			tick()
			checkJob("older-low", store.JobAssigned, 0)
			pickup("older-low")
		})
	}
}
