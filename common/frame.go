package common

import (
	"io"
	"net"
	"time"

	"github.com/pion/rtp"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type AnnexBFrame []byte // 一帧AnnexB格式数据
type RTPFrame struct {
	rtp.Packet
}

func (rtp *RTPFrame) Clone() *RTPFrame {
	return &RTPFrame{*rtp.Packet.Clone()}
}

func (rtp *RTPFrame) H264Type() (naluType codec.H264NALUType) {
	return naluType.Parse(rtp.Payload[0])
}
func (rtp *RTPFrame) H265Type() (naluType codec.H265NALUType) {
	return naluType.Parse(rtp.Payload[0])
}

func (rtp *RTPFrame) Unmarshal(raw []byte) *RTPFrame {
	if err := rtp.Packet.Unmarshal(raw); err != nil {
		log.Error(err)
		return nil
	}
	return rtp
}

type BaseFrame struct {
	DeltaTime uint32    // 相对上一帧时间戳，毫秒
	AbsTime   uint32    // 绝对时间戳，毫秒
	Timestamp time.Time // 写入时间,可用于比较两个帧的先后
	Sequence  uint32    // 在一个Track中的序号
	BytesIn   int       // 输入字节数用于计算BPS
}

type DataFrame[T any] struct {
	BaseFrame
	Value T
}

type AVFrame struct {
	BaseFrame
	IFrame  bool
	PTS     uint32
	DTS     uint32
	AVCC    util.BLL    `json:"-"` // 打包好的AVCC格式(MPEG-4格式、Byte-Stream Format)
	RTP     []*RTPFrame `json:"-"`
	AUList  util.BLLs   `json:"-"` // 裸数据
	mem     util.BLL
	CanRead bool `json:"-"`
}

func (av *AVFrame) WriteAVCC(ts uint32, frame util.BLL) {
	av.AbsTime = ts
	av.BytesIn += frame.ByteLength
	frame.Transfer(&av.AVCC)
	av.DTS = ts * 90
}

func (av *AVFrame) AppendMem(item *util.ListItem[util.BLI]) {
	av.mem.Push(item)
}

func (av *AVFrame) AppendRTP(rtp *RTPFrame) {
	av.RTP = append(av.RTP, rtp)
}

// Reset 重置数据,复用内存
func (av *AVFrame) Reset() {
	if av.RTP != nil {
		av.RTP = av.RTP[:0]
	}
	av.mem.Recycle()
	av.AVCC.Recycle()
	av.AUList.Recycle()
	av.BytesIn = 0
	av.AbsTime = 0
	av.DeltaTime = 0
}

type SequenceHead struct {
	AVCC []byte
	Seq  int //收到第几个序列帧，用于变码率时让订阅者发送序列帧
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
