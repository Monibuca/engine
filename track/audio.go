package track

import (
	"m7s.live/engine/v4/codec"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

type Audio struct {
	Media
	CodecID    codec.AudioCodecID
	Channels   byte
	SampleSize byte
	AVCCHead   []byte // 音频包在AVCC格式中，AAC会有两个字节，其他的只有一个字节
	codec.AudioSpecificConfig
}

func (a *Audio) Attach() {
	if a.Attached.CompareAndSwap(false, true) {
		a.Stream.AddTrack(a)
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

func (av *Audio) WriteADTS(adts []byte) {

}
func (av *Audio) WriteRaw(pts uint32, raw []byte) {
	curValue := &av.Value
	curValue.BytesIn += len(raw)
	if len(av.AVCCHead) == 2 {
		raw = raw[7:] //AAC 去掉7个字节的ADTS头
	}
	curValue.AUList.Push(av.BytesPool.GetShell(raw))
	av.generateTimestamp(pts)
	av.Flush()
}

func (av *Audio) WriteAVCC(ts uint32, frame util.BLL) {
	av.Value.WriteAVCC(ts, frame)
	av.generateTimestamp(ts * 90)
	av.Flush()
}

func (a *Audio) CompleteAVCC(value *AVFrame) {
	value.AVCC.Push(a.BytesPool.GetShell(a.AVCCHead))
	value.AUList.Range(func(v *util.BLL) bool {
		v.Range(func(v util.BLI) bool {
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
	if a.HistoryRing == nil && a.IDRing != nil {
		a.narrow(int(a.Value.Sequence - a.IDRing.Value.Sequence))
	}
	a.AddIDR()
}
