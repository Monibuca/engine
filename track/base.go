package track

import (
	"time"
	"unsafe"

	"github.com/pion/rtp"
	"go.uber.org/zap"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type 流速控制 struct {
	起始时间戳 time.Duration
	起始dts time.Duration
	等待上限  time.Duration
	起始时间  time.Time
}

func (p *流速控制) 重置(绝对时间戳 time.Duration, dts time.Duration) {
	p.起始时间 = time.Now()
	p.起始时间戳 = 绝对时间戳
	p.起始dts = dts
	// println("重置", p.起始时间.Format("2006-01-02 15:04:05"), p.起始时间戳)
}
func (p *流速控制) 根据起始DTS计算绝对时间戳(dts time.Duration) time.Duration {
	if dts < p.起始dts {
		dts += (1 << 32)
	}
	return ((dts-p.起始dts)*time.Millisecond + p.起始时间戳*90) / 90
}

func (p *流速控制) 控制流速(绝对时间戳 time.Duration, dts time.Duration) (等待了 time.Duration) {
	数据时间差, 实际时间差 := 绝对时间戳-p.起始时间戳, time.Since(p.起始时间)
	// println("数据时间差", 数据时间差, "实际时间差", 实际时间差, "绝对时间戳", 绝对时间戳, "起始时间戳", p.起始时间戳, "起始时间", p.起始时间.Format("2006-01-02 15:04:05"))
	// if 实际时间差 > 数据时间差 {
	// 	p.重置(绝对时间戳)
	// 	return
	// }
	// 如果收到的帧的时间戳超过实际消耗的时间100ms就休息一下，100ms作为一个弹性区间防止频繁调用sleep
	if 过快 := (数据时间差 - 实际时间差); 过快 > 100*time.Millisecond {
		// fmt.Println("过快毫秒", 过快.Milliseconds())
		// println("过快毫秒", p.name, 过快.Milliseconds())
		if 过快 > p.等待上限 {
			等待了 = p.等待上限
		} else {
			等待了 = 过快
		}
		time.Sleep(等待了)
	} else if 过快 < -100*time.Millisecond {
		// fmt.Println("过慢毫秒", 过快.Milliseconds())
		// p.重置(绝对时间戳, dts)
		// println("过慢毫秒", p.name, 过快.Milliseconds())
	}
	return
}

type SpesificTrack interface {
	CompleteRTP(*AVFrame)
	CompleteAVCC(*AVFrame)
	WriteSliceBytes([]byte)
	WriteRTPFrame(*util.ListItem[RTPFrame])
	generateTimestamp(uint32)
	WriteSequenceHead([]byte) error
	writeAVCCFrame(uint32, *util.BLLReader, *util.BLL) error
	GetNALU_SEI() *util.ListItem[util.Buffer]
	Flush()
}

type IDRingList struct {
	IDRList     util.List[*util.Ring[*AVFrame]]
	IDRing      *util.Ring[*AVFrame]
	HistoryRing *util.Ring[*AVFrame]
}

func (p *IDRingList) AddIDR(IDRing *util.Ring[*AVFrame]) {
	p.IDRList.PushValue(IDRing)
	p.IDRing = IDRing
}

func (p *IDRingList) ShiftIDR() {
	p.IDRList.Shift()
	p.HistoryRing = p.IDRList.Next.Value
}

// Media 基础媒体Track类
type Media struct {
	Base[any, *AVFrame]
	PayloadType     byte
	IDRingList      `json:"-" yaml:"-"` //最近的关键帧位置，首屏渲染
	SSRC            uint32
	SampleRate      uint32
	BytesPool       util.BytesPool      `json:"-" yaml:"-"`
	RtpPool         util.Pool[RTPFrame] `json:"-" yaml:"-"`
	SequenceHead    []byte              `json:"-" yaml:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS) AAC(config)
	SequenceHeadSeq int
	RTPDemuxer
	SpesificTrack `json:"-" yaml:"-"`
	deltaTs       time.Duration //用于接续发布后时间戳连续
	deltaDTSRange time.Duration //DTS差的范围
	流速控制
}

func (av *Media) GetFromPool(b util.IBytes) (item *util.ListItem[util.Buffer]) {
	if b.Reuse() {
		item = av.BytesPool.Get(b.Len())
		copy(item.Value, b.Bytes())
	} else {
		return av.BytesPool.GetShell(b.Bytes())
	}
	return
}

func (av *Media) GetRTPFromPool() (result *util.ListItem[RTPFrame]) {
	result = av.RtpPool.Get()
	if result.Value.Packet == nil {
		result.Value.Packet = &rtp.Packet{}
		result.Value.PayloadType = av.PayloadType
		result.Value.SSRC = av.SSRC
		result.Value.Version = 2
		result.Value.Raw = make([]byte, 1460)
	}
	result.Value.Raw = result.Value.Raw[:1460]
	result.Value.Payload = result.Value.Raw[:0]
	return
}

// 为json序列化而计算的数据
func (av *Media) SnapForJson() {
	v := av.LastValue
	if av.RawPart != nil {
		av.RawPart = av.RawPart[:0]
	} else {
		av.RawPart = make([]int, 0, 10)
	}
	if av.RawSize = v.AUList.ByteLength; av.RawSize > 0 {
		r := v.AUList.NewReader()
		for b, err := r.ReadByte(); err == nil && len(av.RawPart) < 10; b, err = r.ReadByte() {
			av.RawPart = append(av.RawPart, int(b))
		}
	}
}

func (av *Media) SetSpeedLimit(value time.Duration) {
	av.等待上限 = value
}

func (av *Media) SetStuff(stuff ...any) {
	// 代表发布者已经离线，该Track成为遗留Track，等待下一任发布者接续发布
	for _, s := range stuff {
		switch v := s.(type) {
		case IStream:
			pubConf := v.GetPublisherConfig()
			av.Base.SetStuff(v)
			av.Init(256, NewAVFrame)
			av.SSRC = uint32(uintptr(unsafe.Pointer(av)))
			av.等待上限 = pubConf.SpeedLimit
		case uint32:
			av.SampleRate = v
		case byte:
			av.PayloadType = v
		case util.BytesPool:
			av.BytesPool = v
		case SpesificTrack:
			av.SpesificTrack = v
		case []any:
			av.SetStuff(v...)
		default:
			av.Base.SetStuff(v)
		}
	}
}

func (av *Media) LastWriteTime() time.Time {
	return av.LastValue.WriteTime
}

func (av *Media) CurrentFrame() *AVFrame {
	return av.Value
}
func (av *Media) PreFrame() *AVFrame {
	return av.LastValue
}

func (av *Media) generateTimestamp(ts uint32) {
	av.Value.PTS = time.Duration(ts)
	av.Value.DTS = time.Duration(ts)
}

func (av *Media) WriteSequenceHead(sh []byte) {
	av.SequenceHead = sh
	av.SequenceHeadSeq++
}
func (av *Media) AppendAuBytes(b ...[]byte) {
	var au util.BLL
	for _, bb := range b {
		au.Push(av.BytesPool.GetShell(bb))
	}
	av.Value.AUList.PushValue(&au)
}

func (av *Media) narrow(gop int) {
	if l := av.Size - gop; l > 12 {
		if log.Trace {
			av.Trace("resize", zap.Int("before", av.Size), zap.Int("after", av.Size-5))
		}
		//缩小缓冲环节省内存
		av.Reduce(5)
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
	curValue, preValue, nextValue := av.Value, av.LastValue, av.Next()
	useDts := curValue.Timestamp == 0
	if av.State == TrackStateOffline {
		av.State = TrackStateOnline
		if useDts {
			av.deltaTs = curValue.DTS - preValue.DTS
		} else {
			av.deltaTs = curValue.Timestamp - preValue.Timestamp
		}
		curValue.DTS = preValue.DTS + 90
		curValue.PTS = preValue.PTS + 90
		curValue.Timestamp = preValue.Timestamp + time.Millisecond
		av.Info("track back online", zap.Duration("delta", av.deltaTs))
	} else if av.deltaTs != 0 {
		if useDts {
			curValue.DTS -= av.deltaTs
			curValue.PTS -= av.deltaTs
		} else {
			rtpts := av.deltaTs * 90 / time.Millisecond
			curValue.DTS -= rtpts
			curValue.PTS -= rtpts
			curValue.Timestamp -= av.deltaTs
		}
	}
	if av.起始时间.IsZero() {
		curValue.DeltaTime = 0
		if useDts {
			curValue.Timestamp = time.Since(av.Stream.GetStartTime())
		}
		av.重置(curValue.Timestamp, curValue.DTS)
	} else {
		if useDts {
			deltaDts := curValue.DTS - preValue.DTS
			if deltaDts <= 0 && deltaDts > -(1<<15) {
				// 生成一个无奈的deltaDts
				deltaDts = 90
				// 必须保证DTS递增
				curValue.DTS = preValue.DTS + deltaDts
			} else if deltaDts != 90 {
				// 正常情况下生成容错范围
				av.deltaDTSRange = deltaDts * 2
			}
			curValue.Timestamp = av.根据起始DTS计算绝对时间戳(curValue.DTS)
		}

		curValue.DeltaTime = uint32(deltaTS(curValue.Timestamp, preValue.Timestamp) / time.Millisecond)
	}
	if log.Trace {
		av.Trace("write", zap.Uint32("seq", curValue.Sequence), zap.Duration("dts", curValue.DTS), zap.Duration("dts delta", curValue.DTS-preValue.DTS), zap.Uint32("delta", curValue.DeltaTime), zap.Duration("timestamp", curValue.Timestamp), zap.Int("au", curValue.AUList.Length), zap.Int("rtp", curValue.RTP.Length), zap.Int("avcc", curValue.AVCC.ByteLength), zap.Int("raw", curValue.AUList.ByteLength), zap.Int("bps", av.BPS))
	}
	bufferTime := av.Stream.GetPublisherConfig().BufferTime
	if bufferTime > 0 && av.IDRingList.IDRList.Length > 1 && deltaTS(curValue.Timestamp, av.IDRingList.IDRList.Next.Next.Value.Value.Timestamp) > bufferTime {
		av.ShiftIDR()
		av.narrow(int(curValue.Sequence - av.HistoryRing.Value.Sequence))
	}
	// 下一帧为订阅起始帧，即将覆盖，需要扩环
	if nextValue == av.IDRing || nextValue == av.HistoryRing {
		// if av.AVRing.Size < 512 {
		if log.Trace {
			av.Stream.Trace("resize", zap.Int("before", av.Size), zap.Int("after", av.Size+5), zap.String("name", av.Name))
		}
		av.Glow(5)
		// } else {
		// 	av.Stream.Error("sub ring overflow", zap.Int("size", av.AVRing.Size), zap.String("name", av.Name))
		// }
	}

	if curValue.AUList.Length > 0 {
		// 补完RTP
		if config.Global.EnableRTP && curValue.RTP.Length == 0 {
			av.CompleteRTP(curValue)
		}
		// 补完AVCC
		if config.Global.EnableAVCC && curValue.AVCC.ByteLength == 0 {
			av.CompleteAVCC(curValue)
		}
	}
	av.ComputeBPS(curValue.BytesIn)
	av.Step()
	if av.等待上限 > 0 {
		等待了 := av.控制流速(curValue.Timestamp, curValue.DTS)
		if log.Trace && 等待了 > 0 {
			av.Trace("speed control", zap.Duration("sleep", 等待了))
		}
	}
}

func deltaTS(curTs time.Duration, preTs time.Duration) time.Duration {
	if curTs < preTs {
		return curTs + (1<<32)*time.Millisecond - preTs
	}
	return curTs - preTs
}
