package server

import (
	"context"
	"sync"

	"github.com/EmpireForge-ef/aux-app/internal/ai"
)

// run is a single in-flight chat turn that keeps executing on the server even
// after the client that started it disconnects — e.g. a phone browser is
// backgrounded when the user switches apps. Events are buffered so a
// reconnecting client can replay from where it left off, and cancel lets a
// Stop button abort the turn.
type run struct {
	mu       sync.Mutex
	cond     *sync.Cond
	events   []ai.Event
	resolved map[string]bool // confirm IDs already answered
	done     bool
	cancel   context.CancelFunc
}

func newRun(cancel context.CancelFunc) *run {
	r := &run{resolved: map[string]bool{}, cancel: cancel}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// append records an event and wakes any streaming clients.
func (r *run) append(ev ai.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.cond.Broadcast()
	r.mu.Unlock()
}

// finish marks the turn complete and wakes streaming clients so they return.
func (r *run) finish() {
	r.mu.Lock()
	r.done = true
	r.cond.Broadcast()
	r.mu.Unlock()
}

// markResolved records that a confirmation has been answered (or timed out),
// so that a client replaying the buffer from the start does not re-open the
// dialog for an action that already ran.
func (r *run) markResolved(confirmID string) {
	r.mu.Lock()
	r.resolved[confirmID] = true
	r.mu.Unlock()
}

// stream sends buffered and live events starting at index `from` until the run
// finishes or ctx is cancelled (the client disconnected). It never mutates the
// run, so many clients can stream the same turn concurrently. Resolved
// confirmations are flagged so a from-scratch replay renders them inertly
// instead of re-prompting; the event count stays 1:1 with buffer indices so
// clients can track their position by counting.
func (r *run) stream(ctx context.Context, from int, send func(ai.Event) error) error {
	// sync.Cond can't select on ctx, so wake the waiter when ctx is cancelled.
	stop := context.AfterFunc(ctx, func() {
		r.mu.Lock()
		r.cond.Broadcast()
		r.mu.Unlock()
	})
	defer stop()

	i := from
	if i < 0 {
		i = 0
	}
	for {
		r.mu.Lock()
		for i >= len(r.events) && !r.done && ctx.Err() == nil {
			r.cond.Wait()
		}
		n := len(r.events) - i
		if n < 0 {
			n = 0
		}
		batch := make([]ai.Event, 0, n)
		for ; i < len(r.events); i++ {
			ev := r.events[i]
			if ev.Type == "confirm" && r.resolved[ev.ConfirmID] {
				ev.Resolved = true
			}
			batch = append(batch, ev)
		}
		done := r.done
		r.mu.Unlock()

		for _, ev := range batch {
			if err := send(ev); err != nil {
				return err // client went away
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if done {
			return nil
		}
	}
}
