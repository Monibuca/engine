package track

import (
	"strings"

	"github.com/Monibuca/engine/v4/codec"
	. "github.com/Monibuca/engine/v4/common"
	"github.com/Monibuca/engine/v4/util"
)

type Audio struct {
	Media[AudioSlice]
	Channels byte
	avccHead []byte
}

func (av *Audio) GetName() string {
	if av.Name == "" {
		return strings.ToLower(codec.SoundFormat[av.CodecID])
	}
	return av.Name
}
func (at *Audio) GetInfo() *Audio {
	return at
}
func (at *Audio) ReadRing() *AVRing[AudioSlice] {
	return util.Clone(at.AVRing)
}
func (at *Audio) Play(onAudio func(*AVFrame[AudioSlice]) error) {
	ar := at.ReadRing()
	for ap := ar.Read(); at.Stream.Err() == nil; ap = ar.Read() {
		if onAudio(ap) != nil {
			break
		}
		ar.MoveNext()
	}
}
func (at *Audio) WriteADTS(adts []byte) {
	at.SampleRate = uint32(codec.SamplingFrequencies[(adts[2]&0x3c)>>2])
	at.Channels = ((adts[2] & 0x1) << 2) | ((adts[3] & 0xc0) >> 6)
	at.DecoderConfiguration.AppendAVCC(codec.ADTSToAudioSpecificConfig(adts))
	at.DecoderConfiguration.AppendRaw(at.DecoderConfiguration.AVCC[0][2:])
	at.DecoderConfiguration.FillFLV(codec.FLV_TAG_TYPE_AUDIO, 0)
}
func (at *Audio) WriteAVCC(ts uint32, frame AVCCFrame) {
	at.Media.WriteAVCC(ts, frame)
	at.Flush()
}

func (at *Audio) Flush() {
	if at.Value.AVCC == nil {
		at.Value.AppendAVCC(at.avccHead)
		for _, raw := range at.Value.Raw {
			at.Value.AppendAVCC(raw)
		}
	}
	// FLV tag 补完
	if at.Value.FLV == nil {
		at.Value.FillFLV(codec.FLV_TAG_TYPE_AUDIO, at.Value.DTS/90)
	}
	at.Media.Flush()
}

type UnknowAudio struct {
	Name   string
	Stream IStream
	Know   AVTrack
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
			a.SampleSize = 16
			a.avccHead = []byte{frame[0], 1}
			a.WriteAVCC(0, frame)
			a.Stream.AddTrack(a)
		case codec.CodecID_PCMA,
			codec.CodecID_PCMU:
			alaw := true
			if codecID == codec.CodecID_PCMU {
				alaw = false
			}
			a := NewG711(at.Stream, alaw)
			at.Know = a
			a.SampleRate = uint32(codec.SoundRate[(frame[0]&0x0c)>>2])
			a.SampleSize = 16
			if frame[0]&0x02 == 0 {
				a.SampleSize = 8
			}
			a.Channels = frame[0]&0x01 + 1
			a.avccHead = frame[:1]
			a.Stream.AddTrack(a)
		}
	} else {
		at.Know.WriteAVCC(ts, frame)
	}
}
