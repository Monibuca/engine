package common

import (
	"context"
	"encoding/json"
	"runtime"
	"time"
)

type AVRing[T RawSlice] struct {
	RingBuffer[AVFrame[T]]
	Poll time.Duration
}

func (av *AVRing[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(av.PreValue())
}

func (r *AVRing[T]) Step() *AVFrame[T] {
	last := &r.RingBuffer.Value
	current := r.RingBuffer.MoveNext()
	current.Sequence = r.MoveCount
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
	for item = &r.RingBuffer.Value; ctx.Err() == nil && !item.canRead; r.wait() {
	}
	return
}

func (r *AVRing[T]) TryRead() (item *AVFrame[T]) {
	if item = &r.RingBuffer.Value; item.canRead {
		return
	}
	return nil
}
