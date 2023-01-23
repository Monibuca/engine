package common

import (
	"context"
	"runtime"
	"time"
)


type AVRing struct {
	RingBuffer[AVFrame]
	Poll time.Duration
}

func (r *AVRing) Step() *AVFrame {
	current := r.RingBuffer.MoveNext()
	current.canRead = false
	current.Reset()
	current.Sequence = r.MoveCount
	r.LastValue.canRead = true
	return current
}

func (r *AVRing) wait() {
	if r.Poll == 0 {
		runtime.Gosched()
	} else {
		time.Sleep(r.Poll)
	}
}

func (r *AVRing) Read(ctx context.Context) (item *AVFrame) {
	for item = &r.RingBuffer.Value; ctx.Err() == nil && !item.canRead; r.wait() {
	}
	return
}

func (r *AVRing) TryRead() (item *AVFrame) {
	if item = &r.RingBuffer.Value; item.canRead {
		return
	}
	return nil
}
