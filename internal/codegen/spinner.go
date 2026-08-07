package codegen

import "time"

type spinnerLifecycle struct {
	ticker *time.Ticker
	ch     <-chan time.Time
}

func (s *spinnerLifecycle) update(active bool) <-chan time.Time {
	if active && s.ticker == nil {
		s.ticker = time.NewTicker(110 * time.Millisecond)
		s.ch = s.ticker.C
	}
	if !active && s.ticker != nil {
		s.ticker.Stop()
		s.ticker = nil
		s.ch = nil
	}
	return s.ch
}
func (s *spinnerLifecycle) close() { s.update(false) }
