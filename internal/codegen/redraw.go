package codegen

import "time"

const compositorFrameInterval = time.Second / 60

type redrawLifecycle struct {
	timer *time.Timer
	ch    <-chan time.Time
}

func (r *redrawLifecycle) request() <-chan time.Time {
	if r.timer == nil {
		r.timer = time.NewTimer(compositorFrameInterval)
		r.ch = r.timer.C
	}
	return r.ch
}

func (r *redrawLifecycle) fired() {
	r.timer = nil
	r.ch = nil
}

func (r *redrawLifecycle) cancel() {
	if r.timer != nil {
		if !r.timer.Stop() {
			select {
			case <-r.timer.C:
			default:
			}
		}
	}
	r.timer = nil
	r.ch = nil
}
