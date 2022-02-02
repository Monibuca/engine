package common

import (
	"context"
	"runtime"
	"time"

	"github.com/Monibuca/engine/v4/util"
)

type AVRing[T RawSlice] struct {
	RingBuffer[AVFrame[T]]
	ctx  context.Context
	Poll time.Duration
}

func (r *AVRing[T]) Init(ctx context.Context, n int) {
	r.ctx = ctx
	r.RingBuffer.Init(n)
}

func (r AVRing[T]) SubRing(rr *util.Ring[AVFrame[T]]) *AVRing[T] {
	r.Ring = rr
	return &r
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

func (r *AVRing[T]) Read() *AVFrame[T] {
	item := &r.Value
	for r.ctx.Err() == nil && !item.canRead {
		r.wait()
	}
	return item
}

func (r *AVRing[T]) TryRead() *AVFrame[T] {
	item := &r.Value
	if r.ctx.Err() == nil && !item.canRead {
		return nil
	}
	return item
}
