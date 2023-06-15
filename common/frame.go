package common

import (
	"bytes"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pion/rtp"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

func SplitAnnexB[T ~[]byte](frame T, process func(T), delimiter []byte) {
	for after := frame; len(frame) > 0; frame = after {
		if frame, after, _ = bytes.Cut(frame, delimiter); len(frame) > 0 {
			process(frame)
		}
	}
}

type RTPFrame struct {
	*rtp.Packet
	Raw []byte
}

func (r *RTPFrame) H264Type() (naluType codec.H264NALUType) {
	return naluType.Parse(r.Payload[0])
}
func (r *RTPFrame) H265Type() (naluType codec.H265NALUType) {
	return naluType.Parse(r.Payload[0])
}

func (r *RTPFrame) Unmarshal(raw []byte) *RTPFrame {
	if r.Packet == nil {
		r.Packet = &rtp.Packet{}
	}
	if err := r.Packet.Unmarshal(raw); err != nil {
		log.Error(err)
		return nil
	}
	return r
}

type DataFrame[T any] struct {
	DeltaTime uint32    // 相对上一帧时间戳，毫秒
	WriteTime time.Time // 写入时间,可用于比较两个帧的先后
	Sequence  uint32    // 在一个Track中的序号
	BytesIn   int       // 输入字节数用于计算BPS
	CanRead   bool      `json:"-" yaml:"-"`
	Data      T         `json:"-" yaml:"-"`
	sync.Cond `json:"-" yaml:"-"`
}

func (df *DataFrame[T]) Reset() {
	df.BytesIn = 0
	df.DeltaTime = 0
}

type AVFrame struct {
	DataFrame[any]
	IFrame    bool
	PTS       time.Duration
	DTS       time.Duration
	Timestamp time.Duration               // 绝对时间戳
	ADTS      *util.ListItem[util.Buffer] `json:"-" yaml:"-"` // ADTS头
	AVCC      util.BLL                    `json:"-" yaml:"-"` // 打包好的AVCC格式(MPEG-4格式、Byte-Stream Format)
	RTP       util.List[RTPFrame]         `json:"-" yaml:"-"`
	AUList    util.BLLs                   `json:"-" yaml:"-"` // 裸数据
}

func (av *AVFrame) WriteAVCC(ts uint32, frame *util.BLL) {
	if ts == 0 {
		ts = 1
	}
	av.Timestamp = time.Duration(ts) * time.Millisecond
	av.BytesIn += frame.ByteLength
	for {
		item := frame.Shift()
		if item == nil {
			break
		}
		av.AVCC.Push(item)
	}
	// frame.Transfer(&av.AVCC)
	// frame.ByteLength = 0
}

// Reset 重置数据,复用内存
func (av *AVFrame) Reset() {
	av.RTP.Recycle()
	av.AVCC.Recycle()
	av.AUList.Recycle()
	if av.ADTS != nil {
		av.ADTS.Recycle()
		av.ADTS = nil
	}
	av.Timestamp = 0
	av.DataFrame.Reset()
}

type ParamaterSets [][]byte

func (v ParamaterSets) GetAnnexB() (r net.Buffers) {
	for _, v := range v {
		r = append(r, codec.NALU_Delimiter2, v)
	}
	return
}

func (v ParamaterSets) WriteAnnexBTo(w io.Writer) (n int, err error) {
	var n1, n2 int
	for _, v := range v {
		if n1, err = w.Write(codec.NALU_Delimiter2); err != nil {
			return
		}
		n += n1
		if n2, err = w.Write(v); err != nil {
			return
		}
		n += n2
	}
	return
}
