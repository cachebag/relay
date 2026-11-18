package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// NFR-P4 and DB-06: with 50 online nodes and 500 queued jobs in a real
// database, a scheduling round takes under a second and no job is lost,
// duplicated or placed on a node it doesn't fit.
func TestSchedulesFiftyNodesAndFiveHundredJobsInUnderASecond(t *testing.T) {
	const (
		nodeCount = 50
		jobCount  = 500
		rounds    = 10
		limit     = time.Second
	)
	ctx := context.Background()
	reg, err := store.NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	jobs, err := store.NewSQLiteJobs(reg.DB())
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	pools := []string{"pool-a", "pool-b"}
	systems := []string{"linux", "windows", "darwin"}
	nodes := map[string]store.Node{}
	for i := range nodeCount {
		n := store.Node{
			ID:       fmt.Sprintf("node-%02d", i),
			Hostname: fmt.Sprintf("host-%02d", i),
			OS:       systems[i%len(systems)],
			Arch:     "amd64",
			CPUs:     4,
			MemBytes: 8 << 30,
			PoolID:   pools[i%len(pools)],
			LastSeen: now,
		}
		nodes[n.ID] = n
		if err := reg.Upsert(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	for i := range jobCount {
		j := store.Job{
			ID:        fmt.Sprintf("job-%03d", i),
			Command:   []string{"true"},
			PoolID:    pools[i%len(pools)],
			Submitter: "load",
			Status:    store.JobQueued,
			CreatedAt: now.Add(time.Duration(i) * time.Millisecond),
		}
		// Every third job runs anywhere; the rest ask for an OS.
		if i%3 != 0 {
			j.Requires.OS = systems[i%len(systems)]
		}
		if err := jobs.Create(ctx, j); err != nil {
			t.Fatal(err)
		}
	}

	s := New(reg, jobs, nil)
	s.started = now.Add(-store.NodeTimeout)
	var slowest time.Duration
	for round := range rounds {
		start := time.Now()
		if err := s.Tick(ctx, now); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		took := time.Since(start)
		slowest = max(slowest, took)
		if took >= limit {
			t.Errorf("round %d took %s, the limit is %s", round, took, limit)
		}
	}
	t.Logf("slowest of %d rounds: %s", rounds, slowest)

	all, err := jobs.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	placed := 0
	for _, j := range all {
		if seen[j.ID] {
			t.Errorf("%s is stored twice", j.ID)
		}
		seen[j.ID] = true
		if j.NodeID == "" {
			if j.Status != store.JobQueued {
				t.Errorf("%s has no node but is %s", j.ID, j.Status)
			}
			continue
		}
		placed++
		n, ok := nodes[j.NodeID]
		switch {
		case !ok:
			t.Errorf("%s is on %s, which is not a node", j.ID, j.NodeID)
		case n.PoolID != j.PoolID:
			t.Errorf("%s of %s is on a node of %s", j.ID, j.PoolID, n.PoolID)
		case j.Requires.OS != "" && n.OS != j.Requires.OS:
			t.Errorf("%s needs %s and is on a %s node", j.ID, j.Requires.OS, n.OS)
		}
	}
	if len(seen) != jobCount {
		t.Errorf("%d jobs stored, want %d", len(seen), jobCount)
	}
	if placed == 0 {
		t.Error("no job was placed on any node")
	}
}
