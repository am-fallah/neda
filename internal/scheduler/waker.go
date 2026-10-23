package scheduler

// Waker is the edge-triggered signal used to wake the scheduler after a
// committed monitor or settings change. It coalesces bursts: a pending wake is
// never lost, but multiple calls before the scheduler drains it collapse into
// one wake-up.
type Waker struct {
	ch chan struct{}
}

// NewWaker constructs a waker with room for one pending signal.
func NewWaker() *Waker {
	return &Waker{ch: make(chan struct{}, 1)}
}

// C returns the receive side for Config.Wake. A nil waker returns a nil channel,
// which is permanently disabled in a select.
func (w *Waker) C() <-chan struct{} {
	if w == nil {
		return nil
	}
	return w.ch
}

// Wake requests a scheduler re-evaluation without blocking.
func (w *Waker) Wake() {
	if w == nil {
		return
	}
	select {
	case w.ch <- struct{}{}:
	default:
	}
}
