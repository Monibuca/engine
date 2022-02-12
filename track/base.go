package track

import (
	. "github.com/Monibuca/engine/v4/common"
	"github.com/pion/rtp"
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
	bf.SeqInStream = bt.Stream.Update()
}

// Media 基础媒体Track类
type Media[T RawSlice] struct {
	Base
	AVRing[T]            `json:"-"`
	CodecID              byte
	SampleRate           uint32
	SampleSize           byte
	DecoderConfiguration DecoderConfiguration[T] `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS) AAC(config)
	// util.BytesPool                               //无锁内存池，用于发布者（在同一个协程中）复用小块的内存，通常是解包时需要临时使用
	lastAvccTS           uint32                  //上一个avcc帧的时间戳
	rtpSequence          uint16
}

func (av *Media[T]) WriteSlice(slice T) {
	av.Value.AppendRaw(slice)
}

func (av *Media[T]) WriteAVCC(ts uint32, frame AVCCFrame) {
	if av.lastAvccTS == 0 {
		av.lastAvccTS = ts
	} else {
		av.Value.DeltaTime = ts - av.lastAvccTS
	}
	cts := frame.CTS()
	av.Value.BytesIn = len(frame)
	av.Value.AppendAVCC(frame)
	av.Value.DTS = ts * 90
	av.Value.PTS = (ts + cts) * 90
	av.Stream.Tracef("WriteAVCC:ts %d,cts %d,len %d", ts, cts, len(frame))
}

func (av *Media[T]) Flush() {
	av.Base.Flush(&av.Value.BaseFrame)
	av.Step()
}

// Packetize packetizes the payload of an RTP packet and returns one or more RTP packets
func (av *Media[T]) PacketizeRTP(payloads ...[]byte) {
	for i, pp := range payloads {
		av.rtpSequence++
		var frame = RTPFrame{Packet: rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Padding:        false,
				Extension:      false,
				Marker:         i == len(payloads)-1,
				PayloadType:    av.DecoderConfiguration.PayloadType,
				SequenceNumber: av.rtpSequence,
				Timestamp:      av.Value.DTS, // Figure out how to do timestamps
				SSRC:           av.Stream.SSRC(),
			},
			Payload: pp,
		}}
		frame.Marshal()
		av.Value.AppendRTP(frame)
	}
}
