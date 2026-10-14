package credit

import (
	"context"
	"log"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// ReasonOnline marks a ledger entry paid for a machine's online time.
const ReasonOnline = "online"

// payEvery is how many unpaid minutes a node builds up before they are written
// to the ledger. Writing a row every minute for every node adds up fast.
const payEvery = 10

// meter is the online time a node has built up but not been paid for yet.
type meter struct {
	pool   string
	owner  string
	last   time.Time
	unpaid time.Duration
}

// Earner pays owners 1 credit for every minute their machine is online.
// Unpaid time is kept in memory and lost if relayd restarts, which is fine.
// Tick must be called from a single goroutine.
type Earner struct {
	credits store.CreditStore
	meters  map[string]*meter
}

func NewEarner(credits store.CreditStore) *Earner {
	return &Earner{credits: credits, meters: make(map[string]*meter)}
}

// Tick adds the time since the last tick to every node in online, pays
// owners in blocks of payEvery minutes, and pays out the whole minutes left
// over for any node that has gone offline since the last tick.
func (e *Earner) Tick(ctx context.Context, now time.Time, online []store.Node) error {
	seen := make(map[string]bool, len(online))

	for _, n := range online {
		// Nobody to pay. Agents from before owners existed don't send one.
		if n.Owner == "" {
			continue
		}
		seen[n.ID] = true

		m, ok := e.meters[n.ID]

		// The machine moved pool or changed owner, so settle up with the
		// old owner and start counting again for the new one.
		if ok && (m.pool != n.PoolID || m.owner != n.Owner) {
			if err := e.payRemainder(ctx, now, n.ID, m); err != nil {
				return err
			}
			ok = false
		}
		if !ok {
			e.meters[n.ID] = &meter{pool: n.PoolID, owner: n.Owner, last: now}
			continue
		}

		m.unpaid += now.Sub(m.last)
		m.last = now
		for m.unpaid >= payEvery*time.Minute {
			if err := e.pay(ctx, now, m, payEvery); err != nil {
				return err
			}
			m.unpaid -= payEvery * time.Minute
		}
	}

	for id, m := range e.meters {
		if seen[id] {
			continue
		}
		if err := e.payRemainder(ctx, now, id, m); err != nil {
			return err
		}
	}
	return nil
}

// payRemainder pays the whole minutes a node has left and forgets it.
func (e *Earner) payRemainder(ctx context.Context, now time.Time, id string, m *meter) error {
	if minutes := int(m.unpaid / time.Minute); minutes > 0 {
		if err := e.pay(ctx, now, m, minutes); err != nil {
			return err
		}
	}
	delete(e.meters, id)
	return nil
}

func (e *Earner) pay(ctx context.Context, now time.Time, m *meter, minutes int) error {
	err := e.credits.Add(ctx, store.CreditEntry{
		ID:        store.NewID(),
		PoolID:    m.pool,
		Account:   m.owner,
		Delta:     minutes,
		Reason:    ReasonOnline,
		CreatedAt: now.UTC(),
	})
	if err != nil {
		return err
	}
	log.Printf("paid %s %d credits in pool %s for online time", m.owner, minutes, m.pool)
	return nil
}
