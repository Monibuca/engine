package track

import (
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

type RingReader[T any, F util.IDataFrame[T]] struct {
	*util.Ring[F]
	Count int // 读取的帧数
}

func (r *RingReader[T, F]) StartRead(ring *util.Ring[F]) (err error) {
	r.Ring = ring
	if r.Value.IsDiscarded() {
		return ErrDiscard
	}
	if r.Value.IsWriting() {
		// t := time.Now()
		r.Value.Wait()
		// log.Info("wait", time.Since(t))
	}
	r.Count++
	r.Value.ReaderEnter()
	return
}

func (r *RingReader[T, F]) TryRead() (f F, err error) {
	if r.Count > 0 {
		preValue := r.Value
		if preValue.IsDiscarded() {
			preValue.ReaderLeave()
			err = ErrDiscard
			return
		}
		if r.Next().Value.IsWriting() {
			return
		}
		defer preValue.ReaderLeave()
		r.Ring = r.Next()
	} else {
		if r.Value.IsWriting() {
			return
		}
	}
	if r.Value.IsDiscarded() {
		err = ErrDiscard
		return
	}
	r.Count++
	f = r.Value
	r.Value.ReaderEnter()
	return
}

func (r *RingReader[T, F]) ReadNext() (err error) {
	return r.Read(r.Next())
}

func (r *RingReader[T, F]) Read(ring *util.Ring[F]) (err error) {
	preValue := r.Value
	defer preValue.ReaderLeave()
	if preValue.IsDiscarded() {
		return ErrDiscard
	}
	return r.StartRead(ring)
}

type DataReader[T any] struct {
	RingReader[T, *common.DataFrame[T]]
}
