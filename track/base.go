package track

import (
	"context"
	"time"

	"github.com/pion/rtp"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

type 流速控制 struct {
	起始时间戳 uint32
	起始时间  time.Time
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
	if config.Global.SpeedLimit == 0 {
		return
	}
	数据时间差, 实际时间差 := p.时间戳差(绝对时间戳), time.Since(p.起始时间)
	// println(绝对时间戳, 实际时间差)
	// if 实际时间差 > 数据时间差 {
	// 	p.重置(绝对时间戳)
	// 	return
	// }
	// 如果收到的帧的时间戳超过实际消耗的时间100ms就休息一下，100ms作为一个弹性区间防止频繁调用sleep
	if 过快毫秒 := (数据时间差 - 实际时间差) / time.Millisecond; 过快毫秒 > 300 {
		if 过快毫秒 > time.Duration(config.Global.SpeedLimit) {
			time.Sleep(time.Millisecond * time.Duration(config.Global.SpeedLimit))
		} else {
			time.Sleep(过快毫秒 * time.Millisecond)
		}
	}
}

// Media 基础媒体Track类
type Media[T RawSlice] struct {
	Base
	AVRing[T]
	SampleRate           uint32
	DecoderConfiguration DecoderConfiguration[T] `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS) AAC(config)
	// util.BytesPool                               //无锁内存池，用于发布者（在同一个协程中）复用小块的内存，通常是解包时需要临时使用
	rtpSequence uint16 //用于生成下一个rtp包的序号
	lastSeq     uint16 //上一个收到的序号，用于乱序重排
	lastSeq2    uint16 //记录上上一个收到的序列号
	乱序重排        util.RTPReorder[*RTPFrame]
	流速控制
}

func (av *Media[T]) LastWriteTime() time.Time {
	return av.AVRing.RingBuffer.LastValue.Timestamp
}

func (av *Media[T]) Play(ctx context.Context, onMedia func(*AVFrame[T]) error) error {
	for ar := av.ReadRing(); ctx.Err() == nil; ar.MoveNext() {
		ap := ar.Read(ctx)
		if err := onMedia(ap); err != nil {
			// TODO: log err
			return err
		}
	}
	return ctx.Err()
}

func (av *Media[T]) ReadRing() *AVRing[T] {
	return util.Clone(av.AVRing)
}

func (av *Media[T]) GetDecoderConfiguration() DecoderConfiguration[T] {
	return av.DecoderConfiguration
}

func (av *Media[T]) CurrentFrame() *AVFrame[T] {
	return &av.AVRing.RingBuffer.Value
}
func (av *Media[T]) PreFrame() *AVFrame[T] {
	return av.AVRing.RingBuffer.LastValue
}

// 获取缓存中下一个rtpFrame
func (av *Media[T]) nextRTPFrame() (frame *RTPFrame) {
	if config.Global.RTPReorder {
		return av.乱序重排.Pop()
	}
	return
}

func (av *Media[T]) generateTimestamp() {
	ts := av.AVRing.RingBuffer.Value.RTP[0].Timestamp
	av.AVRing.RingBuffer.Value.PTS = ts
	av.AVRing.RingBuffer.Value.DTS = ts
}

// 对RTP包乱序重排
func (av *Media[T]) recorderRTP(frame *RTPFrame) *RTPFrame {
	if config.Global.RTPReorder {
		return av.乱序重排.Push(frame.SequenceNumber, frame)
	} else {
		if av.lastSeq == 0 {
			av.lastSeq = frame.SequenceNumber
		} else if frame.SequenceNumber == av.lastSeq2+1 { // 本次序号是上上次的序号+1 说明中间隔了一个错误序号（某些rtsp流中的rtcp包写成了rtp包导致的）
			av.lastSeq = frame.SequenceNumber
		} else {
			av.lastSeq2 = av.lastSeq
			av.lastSeq = frame.SequenceNumber
			if av.lastSeq != av.lastSeq2+1 { //序号不连续
				// av.Stream.Warn("RTP SequenceNumber error", av.lastSeq2, av.lastSeq)
				return frame
			}
		}
		return frame
	}
}
func (av *Media[T]) UnmarshalRTPPacket(p *rtp.Packet) (frame *RTPFrame) {
	frame = &RTPFrame{
		Packet: *p,
	}
	av.Value.BytesIn += len(p.Payload) + 12
	return av.recorderRTP(frame)
}
func (av *Media[T]) UnmarshalRTP(raw []byte) (frame *RTPFrame) {
	av.Value.BytesIn += len(raw)
	if frame = new(RTPFrame); frame.Unmarshal(raw) == nil {
		return
	}
	return av.recorderRTP(frame)
}

func (av *Media[T]) WriteSlice(slice T) {
	av.AVRing.RingBuffer.Value.AppendRaw(slice)
}

func (av *Media[T]) WriteAVCC(ts uint32, frame AVCCFrame) {
	curValue := &av.AVRing.RingBuffer.Value
	cts := frame.CTS()
	curValue.BytesIn += len(frame)
	curValue.AppendAVCC(frame)
	curValue.DTS = ts * 90
	curValue.PTS = (ts + cts) * 90
	// av.Stream.Tracef("WriteAVCC:ts %d,cts %d,len %d", ts, cts, len(frame))
}

func (av *Media[T]) Flush() {
	curValue := &av.AVRing.RingBuffer.Value
	preValue := av.AVRing.RingBuffer.LastValue
	if av.起始时间.IsZero() {
		av.重置(curValue.AbsTime)
	} else {
		curValue.DeltaTime = (curValue.DTS - preValue.DTS) / 90
		curValue.AbsTime = preValue.AbsTime + curValue.DeltaTime
	}
	av.Base.Flush(&curValue.BaseFrame)
	av.控制流速(curValue.AbsTime)
	av.Step()
}

// Packetize packetizes the payload of an RTP packet and returns one or more RTP packets
func (av *Media[T]) PacketizeRTP(payloads ...[]byte) {
	for i, pp := range payloads {
		av.rtpSequence++
		var frame = &RTPFrame{Packet: rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Padding:        false,
				Extension:      false,
				Marker:         i == len(payloads)-1,
				PayloadType:    av.DecoderConfiguration.PayloadType,
				SequenceNumber: av.rtpSequence,
				Timestamp:      av.AVRing.RingBuffer.Value.PTS, // Figure out how to do timestamps
				SSRC:           av.Stream.SSRC(),
			},
			Payload: pp,
		}}
		frame.Marshal()
		av.AVRing.RingBuffer.Value.AppendRTP(frame)
	}
}
