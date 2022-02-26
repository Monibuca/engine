package track

import (
	"context"
	"time"

	"github.com/pion/rtp"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

// Base 基础Track类
type Base struct {
	Name   string
	Stream IStream `json:"-"`
	BPS
}

func (bt *Base) GetName() string {
	return bt.Name
}

func (bt *Base) Flush(bf *BaseFrame) {
	bt.ComputeBPS(bf.BytesIn)
	bf.Timestamp = time.Now()
}

// Media 基础媒体Track类
type Media[T RawSlice] struct {
	Base
	AVRing[T]            `json:"-"`
	SampleRate           uint32
	SampleSize           byte
	DecoderConfiguration DecoderConfiguration[T] `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS) AAC(config)
	// util.BytesPool                               //无锁内存池，用于发布者（在同一个协程中）复用小块的内存，通常是解包时需要临时使用
	rtpSequence    uint16      //用于生成下一个rtp包的序号
	orderQueue     []*RTPFrame //rtp包的缓存队列，用于乱序重排
	lastSeq        uint16      //上一个收到的序号，用于乱序重排
	lastSeq2       uint16      //记录上上一个收到的序列号
	firstTimestamp time.Time   //第一次写入的时间，用于计算总时间防止过快写入
}

func (av *Media[T]) LastWriteTime() time.Time {
	return av.AVRing.PreValue().Timestamp
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
	return &av.Value
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
	ts := av.Value.RTP[0].Timestamp
	av.Value.PTS = ts
	av.Value.DTS = ts
}

func (av *Media[T]) UnmarshalRTP(raw []byte) (frame *RTPFrame) {
	if frame = new(RTPFrame); frame.Unmarshal(raw) == nil {
		return
	}
	if config.Global.RTPReorder {
		if frame.SequenceNumber < av.lastSeq && av.lastSeq-frame.SequenceNumber < 0x8000 {
			// 出现旧的包直接丢弃
			return nil
		} else if av.lastSeq == 0 {
			// 初始化
			av.lastSeq = frame.SequenceNumber
			return
		} else if av.lastSeq+1 == frame.SequenceNumber {
			// 正常顺序
			av.lastSeq = frame.SequenceNumber
			copy(av.orderQueue, av.orderQueue[1:])
			return
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
				return
			}
		}
		return
	}
}

func (av *Media[T]) WriteSlice(slice T) {
	av.Value.AppendRaw(slice)
}

func (av *Media[T]) WriteAVCC(ts uint32, frame AVCCFrame) {
	cts := frame.CTS()
	av.Value.BytesIn = len(frame)
	av.Value.AppendAVCC(frame)
	av.Value.DTS = ts * 90
	av.Value.PTS = (ts + cts) * 90
	// av.Stream.Tracef("WriteAVCC:ts %d,cts %d,len %d", ts, cts, len(frame))
}

func (av *Media[T]) Flush() {
	preValue := av.PreValue()
	if av.firstTimestamp.IsZero() {
		av.firstTimestamp = time.Now()
	} else {
		av.Value.DeltaTime = (av.Value.DTS - preValue.DTS) / 90
		av.Value.AbsTime = preValue.AbsTime + av.Value.DeltaTime
	}
	av.Base.Flush(&av.Value.BaseFrame)
	// 如果收到的帧的时间戳超过实际消耗的时间100ms就休息一下，100ms作为一个弹性区间防止频繁调用sleep
	if fast := time.Duration(av.Value.AbsTime)*time.Millisecond - time.Since(av.firstTimestamp); fast > time.Millisecond*100 {
		time.Sleep(fast)
	}
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
				Timestamp:      av.Value.PTS, // Figure out how to do timestamps
				SSRC:           av.Stream.SSRC(),
			},
			Payload: pp,
		}}
		frame.Marshal()
		av.Value.AppendRTP(frame)
	}
}
