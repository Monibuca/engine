package track

import (
	"time"
	"unsafe"

	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

type 流速控制 struct {
	起始时间戳 uint32
	起始时间  time.Time
	等待上限  time.Duration
}

func (p *流速控制) 重置(绝对时间戳 uint32) {
	p.起始时间 = time.Now()
	p.起始时间戳 = 绝对时间戳
	// println("重置", p.起始时间.Format("2006-01-02 15:04:05"), p.起始时间戳)
}
func (p *流速控制) 时间戳差(绝对时间戳 uint32) time.Duration {
	return time.Duration(绝对时间戳-p.起始时间戳) * time.Millisecond
}
func (p *流速控制) 控制流速(绝对时间戳 uint32) {
	数据时间差, 实际时间差 := p.时间戳差(绝对时间戳), time.Since(p.起始时间)
	// println("数据时间差", 数据时间差, "实际时间差", 实际时间差, "绝对时间戳", 绝对时间戳, "起始时间戳", p.起始时间戳, "起始时间", p.起始时间.Format("2006-01-02 15:04:05"))
	// if 实际时间差 > 数据时间差 {
	// 	p.重置(绝对时间戳)
	// 	return
	// }
	// 如果收到的帧的时间戳超过实际消耗的时间100ms就休息一下，100ms作为一个弹性区间防止频繁调用sleep
	if 过快毫秒 := (数据时间差 - 实际时间差) / time.Millisecond; 过快毫秒 > 100 {
		// println("过快毫秒", 过快毫秒)
		if 过快毫秒 > p.等待上限 {
			time.Sleep(time.Millisecond * p.等待上限)
		} else {
			time.Sleep(过快毫秒 * time.Millisecond)
		}
	} else if 过快毫秒 < -100 {
		// println("过慢毫秒", 过快毫秒)
	}
}

type SpesificTrack interface {
	CompleteRTP(*AVFrame)
	CompleteAVCC(*AVFrame)
	WriteSliceBytes([]byte)
	WriteRTPFrame(*RTPFrame)
	generateTimestamp(uint32)
	Flush()
}

type IDRingList struct {
	util.List[*util.Ring[AVFrame]]
	IDRing      *util.Ring[AVFrame]
	HistoryRing *util.Ring[AVFrame]
}

func (p *IDRingList) AddIDR(IDRing *util.Ring[AVFrame]) {
	p.PushValue(IDRing)
	p.IDRing = IDRing
}

func (p *IDRingList) ShiftIDR() {
	p.Shift()
	p.HistoryRing = p.Next.Value
}

// Media 基础媒体Track类
type Media struct {
	Base
	RingBuffer[AVFrame]
	IDRingList      `json:"-"` //最近的关键帧位置，首屏渲染
	SampleRate      uint32
	SSRC            uint32
	PayloadType     byte
	BytesPool       util.BytesPool `json:"-"`
	rtpPool         util.Pool[RTPFrame]
	SequenceHead    []byte `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS) AAC(config)
	SequenceHeadSeq int
	RTPMuxer
	RTPDemuxer
	SpesificTrack `json:"-"`
	流速控制
}

// 为json序列化而计算的数据
func (av *Media) SnapForJson() {
	v := av.LastValue
	if av.RawPart != nil {
		av.RawPart = av.RawPart[:0]
	}
	av.RawSize = v.AUList.ByteLength
	r := v.AUList.NewReader()
	for b, err := r.ReadByte(); err == nil && len(av.RawPart) < 10; b, err = r.ReadByte() {
		av.RawPart = append(av.RawPart, int(b))
	}
}

func (av *Media) SetSpeedLimit(value time.Duration) {
	av.等待上限 = value * time.Millisecond
}

func (av *Media) SetStuff(stuff ...any) {
	for _, s := range stuff {
		switch v := s.(type) {
		case string:
			av.Name = v
		case int:
			av.Init(v)
			av.SSRC = uint32(uintptr(unsafe.Pointer(av)))
			av.等待上限 = config.Global.SpeedLimit
		case uint32:
			av.SampleRate = v
		case byte:
			av.PayloadType = v
		case IStream:
			av.Stream = v
		case util.BytesPool:
			av.BytesPool = v
		case SpesificTrack:
			av.SpesificTrack = v
		}
	}
}

func (av *Media) LastWriteTime() time.Time {
	return av.LastValue.Timestamp
}

// func (av *Media) Play(ctx context.Context, onMedia func(*AVFrame) error) error {
// 	for ar := av.ReadRing(); ctx.Err() == nil; ar.MoveNext() {
// 		ap := ar.Read(ctx)
// 		if err := onMedia(ap); err != nil {
// 			// TODO: log err
// 			return err
// 		}
// 	}
// 	return ctx.Err()
// }

func (av *Media) CurrentFrame() *AVFrame {
	return &av.Value
}
func (av *Media) PreFrame() *AVFrame {
	return av.LastValue
}

func (av *Media) generateTimestamp(ts uint32) {
	av.Value.PTS = ts
	av.Value.DTS = ts
}

func (av *Media) AppendAuBytes(b ...[]byte) {
	var au util.BLL
	for _, bb := range b {
		au.Push(av.BytesPool.GetShell(bb))
	}
	av.Value.AUList.PushValue(&au)
}

func (av *Media) narrow(gop int) {
	if l := av.Size - gop - 5; l > 5 {
		// av.Stream.Debug("resize", zap.Int("before", av.Size), zap.Int("after", av.Size-l), zap.String("name", av.Name))
		//缩小缓冲环节省内存
		av.Reduce(l).Do(func(v AVFrame) {
			v.Reset()
		})
	}
}

func (av *Media) AddIDR() {
	if av.Stream.GetPublisherConfig().BufferTime > 0 {
		av.IDRingList.AddIDR(av.Ring)
		if av.HistoryRing == nil {
			av.HistoryRing = av.IDRing
		}
	} else {
		av.IDRing = av.Ring
	}
}

func (av *Media) Flush() {
	curValue, preValue, nextValue := &av.Value, av.LastValue, av.Next()
	bufferTime := av.Stream.GetPublisherConfig().BufferTime
	if bufferTime > 0 && av.IDRingList.Length > 1 && time.Duration(curValue.AbsTime-av.IDRingList.Next.Next.Value.Value.AbsTime)*time.Millisecond > bufferTime {
		av.ShiftIDR()
		av.narrow(int(curValue.Sequence - av.HistoryRing.Value.Sequence))
	}
	// 下一帧为订阅起始帧，即将覆盖，需要扩环
	if nextValue == av.IDRing || nextValue == av.HistoryRing {
		// if av.AVRing.Size < 512 {
		// av.Stream.Debug("resize", zap.Int("before", av.Size), zap.Int("after", av.Size+5), zap.String("name", av.Name))
		av.Glow(5)
		// } else {
		// 	av.Stream.Error("sub ring overflow", zap.Int("size", av.AVRing.Size), zap.String("name", av.Name))
		// }
	}
	// 补完RTP
	if config.Global.EnableRTP && curValue.RTP.Length == 0 {
		av.CompleteRTP(curValue)
	}
	// 补完AVCC
	if config.Global.EnableAVCC && curValue.AVCC.ByteLength == 0 {
		av.CompleteAVCC(curValue)
	}
	if av.起始时间.IsZero() {
		curValue.DeltaTime = 0
		av.重置(curValue.AbsTime)
	} else if curValue.AbsTime == 0 {
		curValue.DeltaTime = (curValue.DTS - preValue.DTS) / 90
		curValue.AbsTime = preValue.AbsTime + curValue.DeltaTime
	} else {
		curValue.DeltaTime = curValue.AbsTime - preValue.AbsTime
	}
	av.Base.Flush(&curValue.BaseFrame)
	if av.等待上限 > 0 {
		av.控制流速(curValue.AbsTime)
	}
	preValue = curValue
	curValue = av.MoveNext()
	curValue.CanRead = false
	curValue.Reset()
	curValue.Sequence = av.MoveCount
	preValue.CanRead = true
}
