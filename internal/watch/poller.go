package watch

import (
	"context"
	"time"
)

// Prober reports whether the watched condition currently holds.
type Prober func(ctx context.Context) bool

// RunPoller implements the spec's triggering semantics (§4.2) for
// polling watch types: the current state is emitted immediately on
// start (initial check), afterwards only state transitions emit, and a
// transition must hold for StabilityThreshold before it is believed
// (flap suppression). Blocks until ctx is cancelled.
func RunPoller(ctx context.Context, cfg Config, posType, negType, source string, probe Prober, emit Emitter) {
	cfg = cfg.WithDefaults()
	emitState := func(up bool) {
		t := negType
		if up {
			t = posType
		}
		emit(t, source, nil)
	}

	current := probe(ctx)
	emitState(current)

	var pending *bool
	var pendingSince time.Time
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		got := probe(ctx)
		if got == current {
			pending = nil // observed state matches belief; discard any pending flap
			continue
		}
		now := time.Now()
		if pending == nil || *pending != got {
			v := got
			pending, pendingSince = &v, now
		}
		if now.Sub(pendingSince) >= cfg.StabilityThreshold {
			current = got
			pending = nil
			emitState(current)
		}
	}
}
