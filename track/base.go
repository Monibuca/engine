package track

import (
	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/util"
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
	SampleRate           HZ
	SampleSize           byte
	DecoderConfiguration AVFrame[T] `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS) AAC(config)
	util.BytesPool                  //无锁内存池，用于发布者（在同一个协程中）复用小块的内存，通常是解包时需要临时使用
	lastAvccTS           uint32     //上一个avcc帧的时间戳
}

func (av *Media[T]) WriteRTP(raw []byte) {
	av.Value.AppendRTP(raw)
	var packet rtp.Packet
	if err := packet.Unmarshal(raw); err != nil {
		return
	}
	av.Value.AppendRTPPackets(packet)
	if packet.Marker {
		av.Flush()
	}
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
	av.Value.BytesIn = len(frame)
	av.Value.AppendAVCC(frame)
	av.Value.DTS = av.SampleRate.ToNTS(ts)
	av.Value.PTS = av.SampleRate.ToNTS(ts + frame.CTS())
}

func (av *Media[T]) Flush() {
	av.Base.Flush(&av.Value.BaseFrame)
	av.Step()
}
