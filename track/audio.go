package track

import (
	"strings"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/util"
)

type Audio interface {
	AVTrack
	ReadRing() *AVRing[AudioSlice]
	Play(onAudio func(*AVFrame[AudioSlice]) bool)
}

type BaseAudio struct {
	Media[AudioSlice]
	Channels byte
	avccHead []byte
}

func (at *BaseAudio) ReadRing() *AVRing[AudioSlice] {
	return util.Clone(at.AVRing)
}
func (at *BaseAudio) Play(onAudio func(*AVFrame[AudioSlice]) bool) {
	ar := at.ReadRing()
	for ap := ar.Read(); at.Stream.Err() == nil; ap = ar.Read() {
		if !onAudio(ap) {
			break
		}
		ar.MoveNext()
	}
}

func (at *BaseAudio) WriteAVCC(ts uint32, frame AVCCFrame) {
	at.Media.WriteAVCC(ts, frame)
	at.Flush()
}

func (at *BaseAudio) Flush() {
	if at.Value.AVCC == nil {
		at.Value.AppendAVCC(at.avccHead)
		for _, raw := range at.Value.Raw {
			at.Value.AppendAVCC(raw)
		}
	}
	at.Media.Flush()
}

type UnknowAudio struct {
	Name   string
	Stream IStream
	Know   Audio
}

func (at *UnknowAudio) WriteAVCC(ts uint32, frame AVCCFrame) {
	if at.Know == nil {
		codecID := frame.AudioCodecID()
		if at.Name == "" {
			at.Name = strings.ToLower(codec.SoundFormat[codecID])
		}
		switch codecID {
		case codec.CodecID_AAC:
			if !frame.IsSequence() {
				return
			}
			a := NewAAC(at.Stream)
			at.Know = a
			a.avccHead = []byte{frame[0], 1}
			a.WriteAVCC(0, frame)
			a.Stream.AddTrack(a.Name, a)
		case codec.CodecID_PCMA,
			codec.CodecID_PCMU:
			alaw := true
			if codecID == codec.CodecID_PCMU {
				alaw = false
			}
			a := NewG711(at.Stream, alaw)
			at.Know = a
			a.SampleRate = HZ(codec.SoundRate[(frame[0]&0x0c)>>2])
			a.Channels = frame[0]&0x01 + 1
			a.avccHead = frame[:1]
			a.Stream.AddTrack(a.Name, a)
		}
	} else {
		at.Know.WriteAVCC(ts, frame)
	}
}
