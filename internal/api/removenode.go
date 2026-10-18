package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// handleRemoveNode serves DELETE /pools/{id}/nodes/{node_id}. A pool admin
// uses it to take someone else's machine out of the pool, for example one
// that keeps failing jobs or whose owner left without running leave (FR-27).
//
// The machine's assigned and running jobs go back to the queue without
// using an attempt, the machine is deleted, and a tombstone stops its later
// heartbeats from registering it again (CM-07).
func handleRemoveNode(reg store.Registry, pools store.PoolStore, jobs store.JobStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolID := r.PathValue("id")
		nodeID := r.PathValue("node_id")

		// Same admin check as rotate-token. An unknown pool has no admin
		// token hash, so it fails the check and gets 401 like a wrong token.
		hash, err := pools.AdminTokenHash(r.Context(), poolID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		if !checkAdmin(r, hash) {
			http.Error(w, "invalid admin token", http.StatusUnauthorized)
			return
		}

		// Mem and SQLite both keep tombstones. Without them a removed
		// machine would just heartbeat its way back in.
		ts, ok := reg.(store.Tombstones)
		if !ok {
			http.Error(w, "this store can't remove machines", http.StatusInternalServerError)
			return
		}

		nodes, err := reg.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// The machine must be in this admin's pool. A machine in another
		// pool gets the same 404 as one that doesn't exist.
		found := false
		for _, n := range nodes {
			if n.ID == nodeID && n.PoolID == poolID {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "node not found in this pool", http.StatusNotFound)
			return
		}

		// Tombstone first, so a heartbeat that arrives while the jobs are
		// being requeued is already refused instead of re-registering the
		// machine. Retrying a removal that failed below is safe, because
		// adding the same tombstone twice is allowed.
		if err := ts.AddTombstone(r.Context(), store.Tombstone{
			PoolID:    poolID,
			NodeID:    nodeID,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		if err := requeueNodeJobs(r.Context(), jobs, nodeID, "removed by admin"); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// Already gone means another request removed it first, which is fine.
		if err := reg.Delete(r.Context(), nodeID); err != nil && !errors.Is(err, store.ErrNotFound) {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		log.Printf("admin removed node %s from pool %s", nodeID, poolID)

		w.WriteHeader(http.StatusNoContent)
	}
}

// requeueNodeJobs puts every assigned or running job on nodeID back in the
// queue and gives back the attempt the scheduler counted when it placed the
// job, since losing the machine isn't the job's fault. It does the same as
// the requeue in the leave handler (DELETE /nodes/{id}). That code is left
// as it is, so this is a separate copy.
func requeueNodeJobs(ctx context.Context, jobs store.JobStore, nodeID, why string) error {
	all, err := jobs.List(ctx)
	if err != nil {
		return err
	}
	for _, j := range all {
		if j.NodeID != nodeID || (j.Status != store.JobAssigned && j.Status != store.JobRunning) {
			continue
		}
		j.Status = store.JobQueued
		j.NodeID = ""

		// The clock for the next attempt starts when a node picks it up.
		j.StartedAt = nil

		if j.Attempts > 0 {
			j.Attempts--
		}
		if err := jobs.Update(ctx, j); err != nil {
			return err
		}
		log.Printf("node %s %s: job %s back in the queue (attempt %d)", nodeID, why, j.ID, j.Attempts)
	}
	return nil
}

// refuseRemoved answers 410 Gone and returns true if an admin removed nodeID
// from poolID. The heartbeat handler calls it before saving anything, so a
// removed machine's heartbeat changes nothing. A registry without
// tombstones never refuses.
func refuseRemoved(w http.ResponseWriter, r *http.Request, reg store.Registry, poolID, nodeID string) bool {
	ts, ok := reg.(store.Tombstones)
	if !ok {
		return false
	}
	removed, err := ts.HasTombstone(r.Context(), poolID, nodeID)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return true
	}
	if removed {
		http.Error(w, "removed from pool by admin", http.StatusGone)
		return true
	}
	return false
}