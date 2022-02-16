package common

import (
	"context"
	"runtime"
	"time"

)

type AVRing[T RawSlice] struct {
	RingBuffer[AVFrame[T]]
	Poll time.Duration
}

func (r *AVRing[T]) Step() *AVFrame[T] {
	last := &r.Value
	current := r.MoveNext()
	current.SeqInTrack = r.MoveCount
	current.canRead = false
	current.Reset()
	last.canRead = true
	return current
}

func (r *AVRing[T]) wait() {
	if r.Poll == 0 {
		runtime.Gosched()
	} else {
		time.Sleep(r.Poll)
	}
}

func (r *AVRing[T]) Read(ctx context.Context) (item *AVFrame[T]) {
	for item = &r.Value; ctx.Err() == nil && !item.canRead; r.wait() {
	}
	return
}

func (r *AVRing[T]) TryRead(ctx context.Context) (item *AVFrame[T]) {
	if item = &r.Value; ctx.Err() == nil && !item.canRead {
		return nil
	}
	return
}
