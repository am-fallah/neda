package observability

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

const rateLimitEventBuffer = 128

// EventWriter persists best-effort provider rate-limit transition events. It
// never blocks the scheduler or outcome worker and never rolls back monitoring
// state.
type EventWriter struct {
	store   *store.Store
	logger  *slog.Logger
	events  chan store.ProviderRateLimitEvent
	dropped atomic.Uint64
}

// NewEventWriter returns a bounded writer. A nil store disables persistence but
// still allows non-blocking Record calls to be dropped.
func NewEventWriter(st *store.Store, logger *slog.Logger) *EventWriter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventWriter{
		store:  st,
		logger: logger,
		events: make(chan store.ProviderRateLimitEvent, rateLimitEventBuffer),
	}
}

// RecordRateLimitEvent enqueues one event without blocking.
func (w *EventWriter) RecordRateLimitEvent(event store.ProviderRateLimitEvent) {
	if w == nil {
		return
	}
	select {
	case w.events <- event:
	default:
		w.dropped.Add(1)
	}
}

// Run consumes events until the context is canceled. Each write has a short
// timeout; failures are logged with a sanitized category and dropped.
func (w *EventWriter) Run(ctx context.Context) error {
	if w == nil || w.store == nil {
		return nil
	}
	if ctx == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-w.events:
			writeCtx, cancel := context.WithTimeout(ctx, time.Second)
			err := w.store.InsertProviderRateLimitEvent(writeCtx, event)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					w.logger.Warn("observability rate-limit event persistence failed", "code", "rate_limit_event_failed")
				}
			}
		}
	}
}

// Dropped returns the number of events dropped because the bounded queue was
// full since this writer started.
func (w *EventWriter) Dropped() uint64 {
	if w == nil {
		return 0
	}
	return w.dropped.Load()
}
