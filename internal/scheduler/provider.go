package scheduler

import (
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

// providerRuntime is scheduler-goroutine-confined state for one provider. It
// enforces at most one in-flight check and a start-to-start minimum spacing
// across different monitors, and mirrors the durable adaptive/cooldown state
// loaded from SQLite. lastDispatch intentionally resets on restart; startup
// distribution is the restart mitigation for in-memory spacing, while the
// adaptive penalty and hard cooldown are durable.
type providerRuntime struct {
	inFlight     bool
	lastDispatch time.Time
	limit        domain.ProviderRateLimit
}

func (p *providerRuntime) dispatch(now time.Time) {
	p.inFlight = true
	p.lastDispatch = now
}

func (p *providerRuntime) complete() {
	p.inFlight = false
}

func (p *providerRuntime) setLimit(limit domain.ProviderRateLimit) {
	p.limit = limit
	if p.limit.Scope == "" {
		p.limit.Scope = domain.ProviderRateLimitScopeProvider
	}
}

func (p *providerRuntime) clearLimit() {
	p.limit = domain.ProviderRateLimit{}
}

// nextSlot returns the earliest time both the start-to-start spacing and the
// provider cooldown permit another dispatch.
func (p *providerRuntime) nextSlot(spacing time.Duration) (time.Time, bool) {
	var slot time.Time
	if spacing > 0 && !p.lastDispatch.IsZero() {
		slot = p.lastDispatch.Add(spacing)
	}
	if p.limit.CooldownUntil != nil {
		cooldown := p.limit.CooldownUntil.UTC()
		if slot.IsZero() || cooldown.After(slot) {
			slot = cooldown
		}
	}
	if slot.IsZero() {
		return time.Time{}, false
	}
	return slot, true
}

func providerState(states map[provider.ID]*providerRuntime, id provider.ID) *providerRuntime {
	if state, ok := states[id]; ok {
		return state
	}
	state := &providerRuntime{}
	states[id] = state
	return state
}

// applyProviderRateLimits mirrors durable state into scheduler-confined runtime
// state. Multiple rows for one provider are combined conservatively: the
// strictest adaptive interval and cooldown win. Rows that disappear reset the
// penalty but preserve in-flight/spacing state owned by this process.
func (s *Scheduler) applyProviderRateLimits(states map[provider.ID]*providerRuntime, limits []domain.ProviderRateLimit) {
	strictest := make(map[provider.ID]domain.ProviderRateLimit, len(limits))
	for _, limit := range limits {
		if limit.ProviderID == "" {
			continue
		}
		current, ok := strictest[limit.ProviderID]
		if !ok {
			strictest[limit.ProviderID] = limit
			continue
		}
		if limit.AdaptiveInterval > current.AdaptiveInterval {
			current.AdaptiveInterval = limit.AdaptiveInterval
		}
		if limit.Consecutive429s > current.Consecutive429s {
			current.Consecutive429s = limit.Consecutive429s
		}
		if limit.CooldownUntil != nil && (current.CooldownUntil == nil || limit.CooldownUntil.After(*current.CooldownUntil)) {
			current.CooldownUntil = limit.CooldownUntil
		}
		strictest[limit.ProviderID] = current
	}
	for id, state := range states {
		if limit, ok := strictest[id]; ok {
			state.setLimit(limit)
			continue
		}
		state.clearLimit()
	}
	for id, limit := range strictest {
		providerState(states, id).setLimit(limit)
	}
}
