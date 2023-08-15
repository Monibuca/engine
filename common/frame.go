package common

import (
	"bytes"
	"io"
	"net"
	"sync"
	"sync/atomic"
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

type IDataFrame[T any] interface {
	Init()               // 初始化
	Reset()              // 重置数据,复用内存
	Ready()              // 标记为可读取
	ReaderEnter() int32  // 读取者数量+1
	ReaderLeave() int32  // 读取者数量-1
	StartWrite() bool    // 开始写入
	SetSequence(uint32)  // 设置序号
	GetSequence() uint32 // 获取序号
	ReaderCount() int32  // 读取者数量
	Discard() int32      // 如果写入时还有读取者没有离开则废弃该帧，剥离RingBuffer，防止并发读写
	IsDiscarded() bool   // 是否已废弃
	IsWriting() bool     // 是否正在写入
	Wait()               // 阻塞等待可读取
	Broadcast()          // 广播可读取
}

type DataFrame[T any] struct {
	DeltaTime   uint32       // 相对上一帧时间戳，毫秒
	WriteTime   time.Time    // 写入时间,可用于比较两个帧的先后
	Sequence    uint32       // 在一个Track中的序号
	BytesIn     int          // 输入字节数用于计算BPS
	CanRead     bool         `json:"-" yaml:"-"` // 是否可读取
	readerCount atomic.Int32 `json:"-" yaml:"-"` // 读取者数量
	Data        T            `json:"-" yaml:"-"`
	sync.Cond   `json:"-" yaml:"-"`
}

func NewDataFrame[T any]() *DataFrame[T] {
	return &DataFrame[T]{}
}
func (df *DataFrame[T]) IsWriting() bool {
	return !df.CanRead
}

func (df *DataFrame[T]) IsDiscarded() bool {
	return df.L == nil
}

func (df *DataFrame[T]) Discard() int32 {
	df.L = nil //标记为废弃
	return df.readerCount.Load()
}

func (df *DataFrame[T]) SetSequence(sequence uint32) {
	df.Sequence = sequence
}

func (df *DataFrame[T]) GetSequence() uint32 {
	return df.Sequence
}

func (df *DataFrame[T]) ReaderEnter() int32 {
	return df.readerCount.Add(1)
}

func (df *DataFrame[T]) ReaderCount() int32 {
	return df.readerCount.Load()
}

func (df *DataFrame[T]) ReaderLeave() int32 {
	return df.readerCount.Add(-1)
}

func (df *DataFrame[T]) StartWrite() bool {
	if df.readerCount.Load() > 0 {
		df.Discard() //标记为废弃
		return false
	} else {
		df.CanRead = false //标记为正在写入
		return true
	}
}

func (df *DataFrame[T]) Ready() {
	df.WriteTime = time.Now()
	df.CanRead = true //标记为可读取
	df.Broadcast()
}

func (df *DataFrame[T]) Init() {
	df.L = EmptyLocker
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

func NewAVFrame() *AVFrame {
	return &AVFrame{}
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
	av.IFrame = false
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
