package ticker

import (
	"time"
)

// Ticker fires at wall-clock aligned boundaries (e.g. :00/:15/:30/:45) and
// invokes Fire with the [from, to] interval that just closed. It catches up
// any boundaries missed while the machine slept.
type Ticker struct {
	step   time.Duration
	fire   func(from, to time.Time)
	paused func() bool
	stop   chan struct{}
	last   time.Time // last boundary already emitted
}

// New builds a ticker with the given interval in minutes. fire is called for
// each closed interval; paused is polled so time keeps aligning while paused
// but no interval is emitted.
func New(intervalMinutes int, fire func(from, to time.Time), paused func() bool) *Ticker {
	if intervalMinutes <= 0 {
		intervalMinutes = 15
	}
	return &Ticker{
		step:   time.Duration(intervalMinutes) * time.Minute,
		fire:   fire,
		paused: paused,
		stop:   make(chan struct{}),
	}
}

// floor rounds t down to the nearest interval boundary in local wall-clock time.
func (t *Ticker) floor(at time.Time) time.Time {
	at = at.Local()
	midnight := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	elapsed := at.Sub(midnight)
	n := elapsed / t.step
	return midnight.Add(n * t.step)
}

// Start begins the loop in a goroutine.
func (t *Ticker) Start() {
	t.last = t.floor(time.Now())
	go t.loop()
}

// Stop halts the loop.
func (t *Ticker) Stop() { close(t.stop) }

func (t *Ticker) loop() {
	for {
		next := t.last.Add(t.step)
		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-t.stop:
			timer.Stop()
			return
		case <-timer.C:
		}

		// Emit every boundary between last+step and now (catch up after sleep).
		now := t.floor(time.Now())
		for b := t.last.Add(t.step); !b.After(now); b = b.Add(t.step) {
			if t.paused == nil || !t.paused() {
				t.fire(b.Add(-t.step), b)
			}
		}
		if now.After(t.last) {
			t.last = now
		}
	}
}
