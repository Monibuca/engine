package track

import (
	"context"
	"runtime"
	"time"

	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

const (
	READSTATE_INIT = iota
	READSTATE_FIRST
	READSTATE_NORMAL
)

type AVRingReader struct {
	ctx   context.Context
	Track *Media
	*util.Ring[common.AVFrame]
	Poll       time.Duration
	State      byte
	FirstSeq   uint32
	FirstTs    uint32
	SkipTs     uint32
	beforeJump uint32
	ConfSeq    int
	startTime  time.Time
	Frame      *common.AVFrame
	AbsTime    uint32
}

func (r *AVRingReader) DecConfChanged() bool {
	return r.ConfSeq != r.Track.SequenceHeadSeq
}

func (r *AVRingReader) wait() {
	if r.Poll == 0 {
		runtime.Gosched()
	} else {
		time.Sleep(r.Poll)
	}
}

func (r *AVRingReader) ReadFrame() *common.AVFrame {
	for r.Frame = &r.Value; r.ctx.Err() == nil && !r.Frame.CanRead; r.wait() {
	}
	return r.Frame
}

func (r *AVRingReader) TryRead() (item *common.AVFrame) {
	if item = &r.Value; item.CanRead {
		return
	}
	return nil
}

func (r *AVRingReader) MoveNext() {
	r.Ring = r.Next()
}

func (r *AVRingReader) Read(ctx context.Context, mode int) (err error) {
	r.ctx = ctx
	switch r.State {
	case READSTATE_INIT:
		startRing := r.Track.Ring
		if r.Track.IDRing != nil {
			startRing = r.Track.IDRing
		}
		switch mode {
		case 0:
			if r.Track.IDRing != nil {
				r.State = READSTATE_FIRST
			} else {
				r.State = READSTATE_NORMAL
			}
		case 1:
			r.State = READSTATE_NORMAL
		case 2:
			if r.Track.HistoryRing != nil {
				startRing = r.Track.HistoryRing
			}
			r.State = READSTATE_NORMAL
		}
		r.Ring = startRing
		r.ReadFrame()
		if err = r.ctx.Err(); err != nil {
			return
		}
		r.startTime = time.Now()
		if r.FirstTs == 0 {
			r.FirstTs = r.Frame.AbsTime
		}
		r.SkipTs = r.FirstTs
		r.FirstSeq = r.Frame.Sequence
	case READSTATE_FIRST:
		if r.Track.IDRing.Value.Sequence != r.FirstSeq {
			r.Ring = r.Track.IDRing
			frame := r.ReadFrame() // 直接跳到最近的关键帧
			if err = r.ctx.Err(); err != nil {
				return
			}
			r.SkipTs = frame.AbsTime - r.beforeJump
			r.State = READSTATE_NORMAL
		} else {
			r.MoveNext()
			frame := r.ReadFrame()
			r.beforeJump = frame.AbsTime - r.FirstTs
			// 防止过快消费
			if fast := time.Duration(r.beforeJump)*time.Millisecond - time.Since(r.startTime); fast > 0 && fast < time.Second {
				time.Sleep(fast)
			}
		}
	case READSTATE_NORMAL:
		r.MoveNext()
		r.ReadFrame()
	}
	r.AbsTime = r.Frame.AbsTime - r.SkipTs
	return
}
