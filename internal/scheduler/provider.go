package scheduler

import (
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

// providerRuntime is scheduler-goroutine-confined state for one provider. It
// enforces at most one in-flight check and a start-to-start minimum spacing
// across different monitors. It is intentionally in-memory and resets on
// restart; startup staggering remains the restart mitigation.
type providerRuntime struct {
	inFlight     bool
	lastDispatch time.Time
}

func (p *providerRuntime) dispatch(now time.Time) {
	p.inFlight = true
	p.lastDispatch = now
}

func (p *providerRuntime) complete() {
	p.inFlight = false
}

func (p *providerRuntime) nextSlot(spacing time.Duration) (time.Time, bool) {
	if spacing <= 0 || p.lastDispatch.IsZero() {
		return time.Time{}, false
	}
	return p.lastDispatch.Add(spacing), true
}

func providerState(states map[provider.ID]*providerRuntime, id provider.ID) *providerRuntime {
	if state, ok := states[id]; ok {
		return state
	}
	state := &providerRuntime{}
	states[id] = state
	return state
}
