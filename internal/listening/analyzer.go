package listening

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Analyzer periodically distils the raw listening data into a durable "learned
// profile" via the model. Driven by a closure so it doesn't depend on the ai
// package.
type Analyzer struct {
	store    *Store
	analyze  func(ctx context.Context, data string) (string, error)
	interval time.Duration
	minPlays int
}

// NewAnalyzer builds an analyzer. interval <= 0 defaults to weekly.
func NewAnalyzer(store *Store, analyze func(context.Context, string) (string, error), interval time.Duration) *Analyzer {
	if interval <= 0 {
		interval = 7 * 24 * time.Hour
	}
	return &Analyzer{store: store, analyze: analyze, interval: interval, minPlays: 20}
}

// Run distils on the configured interval until ctx is cancelled. It runs a
// first pass shortly after startup unless a recent profile already exists (so
// restarts don't trigger redundant analyses).
func (a *Analyzer) Run(ctx context.Context) {
	initial := 2 * time.Minute
	if gen := a.store.ProfileGeneratedAt(); !gen.IsZero() {
		if remaining := a.interval - time.Since(gen); remaining > initial {
			initial = remaining
		}
	}
	timer := time.NewTimer(initial)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if _, err := a.AnalyzeOnce(ctx); err != nil {
				slog.Warn("profile analysis failed", "err", err)
			}
			timer.Reset(a.interval)
		}
	}
}

// AnalyzeOnce runs the distillation immediately and stores the result. It
// returns an error (not fatal) when there isn't enough data yet.
func (a *Analyzer) AnalyzeOnce(ctx context.Context) (string, error) {
	if n := a.store.TotalPlays(); n < a.minPlays {
		return "", fmt.Errorf("not enough listening data yet (%d plays, need at least %d)", n, a.minPlays)
	}
	summary, err := a.analyze(ctx, a.store.AnalysisInput())
	if err != nil {
		return "", err
	}
	if summary == "" {
		return "", fmt.Errorf("analysis produced no summary")
	}
	a.store.SetLearnedProfile(summary)
	slog.Info("updated learned profile", "length", len(summary))
	return summary, nil
}
