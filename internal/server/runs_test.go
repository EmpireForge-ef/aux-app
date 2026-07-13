package server

import (
	"context"
	"testing"
	"time"

	"github.com/EmpireForge-ef/aux-app/internal/ai"
)

// collect streams a run from `from` and returns the events it received. It runs
// the run to completion (or ctx cancellation).
func collect(t *testing.T, rn *run, from int) []ai.Event {
	t.Helper()
	var got []ai.Event
	err := rn.stream(context.Background(), from, func(ev ai.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	return got
}

func TestRunReplayAndResume(t *testing.T) {
	rn := newRun(func() {})
	rn.append(ai.Event{Type: "user", Text: "hi"})
	rn.append(ai.Event{Type: "text", Text: "one"})
	rn.append(ai.Event{Type: "text", Text: "two"})
	rn.append(ai.Event{Type: "done"})
	rn.finish()

	// Full replay from the start.
	if got := collect(t, rn, 0); len(got) != 4 || got[0].Type != "user" || got[3].Type != "done" {
		t.Fatalf("full replay = %+v", got)
	}
	// Resume from the middle: only the tail is delivered.
	if got := collect(t, rn, 2); len(got) != 2 || got[0].Text != "two" {
		t.Fatalf("resume from 2 = %+v", got)
	}
	// A from past the end yields nothing but still returns.
	if got := collect(t, rn, 99); len(got) != 0 {
		t.Fatalf("resume past end = %+v", got)
	}
}

func TestRunResolvedConfirmFlaggedOnReplay(t *testing.T) {
	rn := newRun(func() {})
	rn.append(ai.Event{Type: "confirm", ConfirmID: "c1"})
	rn.append(ai.Event{Type: "tool_result"})
	rn.markResolved("c1")
	rn.finish()

	got := collect(t, rn, 0)
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %+v", got)
	}
	if got[0].Type != "confirm" || !got[0].Resolved {
		t.Errorf("replayed confirm should be flagged resolved: %+v", got[0])
	}
}

func TestRunLiveStreamThenFinish(t *testing.T) {
	rn := newRun(func() {})
	events := make(chan ai.Event, 8)
	go func() {
		_ = rn.stream(context.Background(), 0, func(ev ai.Event) error {
			events <- ev
			return nil
		})
		close(events)
	}()

	rn.append(ai.Event{Type: "text", Text: "live"})
	select {
	case ev := <-events:
		if ev.Text != "live" {
			t.Fatalf("got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a live event")
	}

	rn.finish()
	// The stream goroutine must return, closing the channel.
	select {
	case _, open := <-events:
		if open {
			// drain any trailing event, then expect close
			<-events
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not return after finish")
	}
}

func TestRunStreamStopsOnContextCancel(t *testing.T) {
	rn := newRun(func() {})
	rn.append(ai.Event{Type: "text", Text: "a"})
	// Not finished: a blocking stream should unblock when ctx is cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- rn.stream(ctx, 0, func(ai.Event) error { return nil })
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a context error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop on cancel")
	}
}
