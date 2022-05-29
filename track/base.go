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
}
func (p *流速控制) 时间戳差(绝对时间戳 uint32) time.Duration {
	return time.Duration(绝对时间戳-p.起始时间戳) * time.Millisecond
}
func (p *流速控制) 控制流速(绝对时间戳 uint32) {
	数据时间差, 实际时间差 := p.时间戳差(绝对时间戳), time.Since(p.起始时间)
	// if 实际时间差 > 数据时间差 {
	// 	p.重置(绝对时间戳)
	// 	return
	// }
	// 如果收到的帧的时间戳超过实际消耗的时间100ms就休息一下，100ms作为一个弹性区间防止频繁调用sleep
	if 过快毫秒 := 数据时间差 - 实际时间差; 过快毫秒 > time.Millisecond*100 {
		// println("休息", 过快毫秒/time.Millisecond, 绝对时间戳, p.起始时间戳)
		if 过快毫秒 > time.Millisecond*500 {
			time.Sleep(time.Millisecond*500)
		} else {
			time.Sleep(过快毫秒)
		}
	}
}

// Media 基础媒体Track类
type Media[T RawSlice] struct {
	Base
	AVRing[T]            `json:"-"`
	SampleRate           uint32
	SampleSize           byte
	DecoderConfiguration DecoderConfiguration[T] `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS) AAC(config)
	// util.BytesPool                               //无锁内存池，用于发布者（在同一个协程中）复用小块的内存，通常是解包时需要临时使用
	rtpSequence uint16      //用于生成下一个rtp包的序号
	orderQueue  []*RTPFrame //rtp包的缓存队列，用于乱序重排
	lastSeq     uint16      //上一个收到的序号，用于乱序重排
	lastSeq2    uint16      //记录上上一个收到的序列号
	流速控制
}

func (av *Media[T]) LastWriteTime() time.Time {
	return av.AVRing.RingBuffer.PreValue().Timestamp
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
	return av.AVRing.RingBuffer.PreValue()
}

// 获取缓存中下一个rtpFrame
func (av *Media[T]) nextRTPFrame() (frame *RTPFrame) {
	if config.Global.RTPReorder {
		frame = av.orderQueue[0]
		av.lastSeq++
		copy(av.orderQueue, av.orderQueue[1:])
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
		if frame.SequenceNumber < av.lastSeq && av.lastSeq-frame.SequenceNumber < 0x8000 {
			// 出现旧的包直接丢弃
			return nil
		} else if av.lastSeq == 0 {
			// 初始化
			av.lastSeq = frame.SequenceNumber
			return frame
		} else if av.lastSeq+1 == frame.SequenceNumber {
			// 正常顺序
			av.lastSeq = frame.SequenceNumber
			copy(av.orderQueue, av.orderQueue[1:])
			return frame
		} else if frame.SequenceNumber > av.lastSeq {
			delta := int(frame.SequenceNumber - av.lastSeq)
			queueLen := len(av.orderQueue)
			// 超过缓存队列长度,TODO: 可能会丢弃正确的包
			if queueLen < delta {
				for {
					av.lastSeq++
					delta = int(frame.SequenceNumber - av.lastSeq)
					copy(av.orderQueue, av.orderQueue[1:])
					// 可以放得进去了
					if delta == queueLen-1 {
						av.orderQueue[queueLen-1] = frame
						frame, av.orderQueue[0] = av.orderQueue[0], nil
						return frame
					}
				}
			}
			// 出现后面的包先到达，缓存起来
			av.orderQueue[delta-1] = frame
			return nil
		} else {
			return nil
		}
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
	frame.Raw, _ = p.Marshal()
	return av.recorderRTP(frame)
}
func (av *Media[T]) UnmarshalRTP(raw []byte) (frame *RTPFrame) {
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
	curValue.BytesIn = len(frame)
	curValue.AppendAVCC(frame)
	curValue.DTS = ts * 90
	curValue.PTS = (ts + cts) * 90
	// av.Stream.Tracef("WriteAVCC:ts %d,cts %d,len %d", ts, cts, len(frame))
}

func (av *Media[T]) Flush() {
	curValue := &av.AVRing.RingBuffer.Value
	preValue := av.AVRing.RingBuffer.PreValue()
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
