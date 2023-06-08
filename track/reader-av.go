package track

import (
	"context"
	"runtime"
	"time"

	"go.uber.org/zap"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

const (
	READSTATE_INIT = iota
	READSTATE_FIRST
	READSTATE_NORMAL
)
const (
	SUBMODE_REAL = iota
	SUBMODE_NOJUMP
	SUBMODE_BUFFER
)
type AVRingReader struct {
	ctx   context.Context
	Track *Media
	*util.Ring[common.AVFrame]
	wait       func()
	State      byte
	FirstSeq   uint32
	FirstTs    time.Duration
	SkipTs     time.Duration //ms
	beforeJump time.Duration
	ConfSeq    int
	startTime  time.Time
	Frame      *common.AVFrame
	AbsTime    uint32
	Delay      uint32
	*log.Logger
}

func (r *AVRingReader) DecConfChanged() bool {
	return r.ConfSeq != r.Track.SequenceHeadSeq
}

func NewAVRingReader(t *Media, poll time.Duration) *AVRingReader {
	r := &AVRingReader{
		Track: t,
	}
	if poll == 0 {
		r.wait = runtime.Gosched
	} else {
		r.wait = func() {
			time.Sleep(poll)
		}
	}
	return r
}

func (r *AVRingReader) ReadFrame() *common.AVFrame {
	for r.Frame = &r.Value; r.ctx.Err() == nil && !r.Frame.CanRead; r.Frame.WG.Wait() {
	}
	// 超过一半的缓冲区大小，说明Reader太慢，需要丢帧
	if r.State == READSTATE_NORMAL && r.Track.LastValue.Sequence-r.Frame.Sequence > uint32(r.Track.Size/2) && r.Track.IDRing != nil && r.Track.IDRing.Value.Sequence > r.Frame.Sequence {
		r.Warn("reader too slow", zap.Uint32("lastSeq", r.Track.LastValue.Sequence), zap.Uint32("seq", r.Frame.Sequence))
		r.Ring = r.Track.IDRing
		return r.ReadFrame()
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
		r.Info("start read", zap.Int("mode", mode))
		startRing := r.Track.Ring
		if r.Track.IDRing != nil {
			startRing = r.Track.IDRing
		} else {
			r.Warn("no IDRring")
		}
		switch mode {
		case SUBMODE_REAL:
			if r.Track.IDRing != nil {
				r.State = READSTATE_FIRST
			} else {
				r.State = READSTATE_NORMAL
			}
		case SUBMODE_NOJUMP:
			r.State = READSTATE_NORMAL
		case SUBMODE_BUFFER:
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
			r.FirstTs = r.Frame.Timestamp
		}
		r.SkipTs = r.FirstTs
		r.FirstSeq = r.Frame.Sequence
		r.Info("first frame read", zap.Duration("firstTs", r.FirstTs), zap.Uint32("firstSeq", r.FirstSeq))
	case READSTATE_FIRST:
		if r.Track.IDRing.Value.Sequence != r.FirstSeq {
			r.Ring = r.Track.IDRing
			frame := r.ReadFrame() // 直接跳到最近的关键帧
			if err = r.ctx.Err(); err != nil {
				return
			}
			r.SkipTs = frame.Timestamp - r.beforeJump
			r.Info("jump", zap.Uint32("skipSeq", r.Track.IDRing.Value.Sequence-r.FirstSeq), zap.Duration("skipTs", r.SkipTs))
			r.State = READSTATE_NORMAL
		} else {
			r.MoveNext()
			frame := r.ReadFrame()
			r.beforeJump = frame.Timestamp - r.FirstTs
			// 防止过快消费
			if fast := r.beforeJump - time.Since(r.startTime); fast > 0 && fast < time.Second {
				time.Sleep(fast)
			}
		}
	case READSTATE_NORMAL:
		r.MoveNext()
		r.ReadFrame()
	}
	r.AbsTime = uint32((r.Frame.Timestamp - r.SkipTs).Milliseconds())
	if r.AbsTime == 0 {
		r.AbsTime = 1
	}
	r.Delay = uint32((r.Track.LastValue.Timestamp - r.Frame.Timestamp).Milliseconds())
	// fmt.Println(r.Track.Name, r.Delay)
	// println(r.Track.Name, r.State, r.Frame.AbsTime, r.SkipTs, r.AbsTime)
	return
}
func (r *AVRingReader) GetPTS32() uint32 {
	return uint32((r.Frame.PTS - r.SkipTs*90/time.Millisecond))
}
func (r *AVRingReader) GetDTS32() uint32 {
	return uint32((r.Frame.DTS - r.SkipTs*90/time.Millisecond))
}
func (r *AVRingReader) ResetAbsTime() {
	r.SkipTs = r.Frame.Timestamp
	r.AbsTime = 1
}
