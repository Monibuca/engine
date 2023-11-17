package track

import (
	"go.uber.org/zap"
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

type Audio struct {
	Media
	CodecID          codec.AudioCodecID
	Channels         byte
	SampleSize       byte
	SizeLength       int // 通常为13
	IndexLength      int
	IndexDeltaLength int
	AVCCHead         []byte // 音频包在AVCC格式中，AAC会有两个字节，其他的只有一个字节
	codec.AudioSpecificConfig
}

func (a *Audio) Attach() {
	if a.Attached.CompareAndSwap(false, true) {
		if err := a.Stream.AddTrack(a).Await(); err != nil {
			a.Error("attach audio track failed", zap.Error(err))
			a.Attached.Store(false)
		} else {
			a.Info("audio track attached", zap.Uint32("sample rate", a.SampleRate))
		}
	}
}

func (a *Audio) Detach() {
	if a.Attached.CompareAndSwap(true, false) {
		a.Stream.RemoveTrack(a)
	}
}

func (a *Audio) GetName() string {
	if a.Name == "" {
		return a.CodecID.String()
	}
	return a.Name
}

func (av *Audio) WriteADTS(pts uint32, adts util.IBytes) {

}

func (av *Audio) WriteSequenceHead(sh []byte) error {
	av.Media.WriteSequenceHead(sh)
	return nil
}

func (av *Audio) Flush() {
	if av.CodecID == codec.CodecID_AAC && av.Value.ADTS == nil {
		item := av.BytesPool.Get(7)
		av.ToADTS(av.Value.AUList.ByteLength, item.Value)
		av.Value.ADTS = item
	}
	av.Media.Flush()
}

func (av *Audio) WriteRawBytes(pts uint32, raw util.IBytes) {
	curValue := av.Value
	curValue.BytesIn += raw.Len()
	av.Value.AUList.Push(av.GetFromPool(raw))
	av.generateTimestamp(pts)
	av.Flush()
}

func (av *Audio) WriteAVCC(ts uint32, frame *util.BLL) {
	av.Value.WriteAVCC(ts, frame)
	av.generateTimestamp(ts * 90)
	av.Flush()
}

func (a *Audio) CompleteAVCC(value *AVFrame) {
	value.AVCC.Push(a.BytesPool.GetShell(a.AVCCHead))
	value.AUList.Range(func(v *util.BLL) bool {
		v.Range(func(v util.Buffer) bool {
			value.AVCC.Push(a.BytesPool.GetShell(v))
			return true
		})
		return true
	})
}

func (a *Audio) CompleteRTP(value *AVFrame) {
	a.PacketizeRTP(value.AUList.ToList()...)
}

func (a *Audio) Narrow() {
	// if a.HistoryRing == nil && a.IDRing != nil {
	// 	a.narrow(int(a.Value.Sequence - a.IDRing.Value.Sequence))
	// }
	a.AddIDR()
}
