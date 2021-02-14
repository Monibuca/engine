package engine

type AudioPack struct {
	Timestamp      uint32
	Payload        []byte
	SequenceNumber uint16
}
type AudioTrack struct {
	Track_Audio
	SoundFormat byte   //4bit
	SoundRate   int    //2bit
	SoundSize   byte   //1bit
	SoundType   byte   //1bit
	RtmpTag     []byte //rtmp协议需要先发这个帧
}

// Push 来自发布者推送的音频
func (at *AudioTrack) Push(timestamp uint32, payload []byte) {
	payloadLen := len(payload)
	if payloadLen < 4 {
		return
	}
	audio := at.Buffer
	audio.Current.Timestamp = timestamp
	audio.Current.Payload = payload
	at.Track_Audio.GetBPS(payloadLen)
	audio.NextW()
}

func NewAudioTrack() *AudioTrack {
	var result AudioTrack
	result.Buffer = NewRing_Audio()
	return &result
}