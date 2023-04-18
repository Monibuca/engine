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

func (av *Audio) WriteADTS(pts uint32, adts []byte) {

}

func (av *Audio) Flush() {
	if av.CodecID == codec.CodecID_AAC && av.Value.ADTS == nil {
		item := util.GetBLI(7)
		av.ToADTS(av.Value.AUList.ByteLength, item.Value)
		av.Value.ADTS = item
	}
	av.Media.Flush()
}

func (av *Audio) WriteRaw(pts uint32, raw []byte) {
	curValue := &av.Value
	curValue.BytesIn += len(raw)
	curValue.AUList.PushShell(raw)
	av.generateTimestamp(pts)
	av.Flush()
}

func (av *Audio) WriteAVCC(ts uint32, frame *util.BLL) {
	av.Value.WriteAVCC(ts, frame)
	av.generateTimestamp(ts * 90)
	av.Flush()
}

func (a *Audio) CompleteAVCC(value *AVFrame) {
	value.AVCC.PushShell(a.AVCCHead)
	value.AUList.Range(func(v *util.BLL) bool {
		v.Range(func(v util.Buffer) bool {
			value.AVCC.PushShell(v)
			return true
		})
		return true
	})
}

func (a *Audio) CompleteRTP(value *AVFrame) {
	a.PacketizeRTP(value.AUList.ToList()...)
}

func (a *Audio) Narrow() {
	if a.HistoryRing == nil && a.IDRing != nil {
		a.narrow(int(a.Value.Sequence - a.IDRing.Value.Sequence))
	}
	a.AddIDR()
}
