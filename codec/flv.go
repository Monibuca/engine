package codec

import (
	"io"
	"net"

	"m7s.live/engine/v4/util"
)

const (
	// FLV Tag Type
	FLV_TAG_TYPE_AUDIO  = 0x08
	FLV_TAG_TYPE_VIDEO  = 0x09
	FLV_TAG_TYPE_SCRIPT = 0x12
)

var (
	Codec2SoundFormat = map[string]byte{
		"aac":  10,
		"pcma": 7,
		"pcmu": 8,
	}
	// 音频格式. 4 bit
	SoundFormat = map[byte]string{
		0:  "Linear PCM, platform endian",
		1:  "ADPCM",
		2:  "MP3",
		3:  "Linear PCM, little endian",
		4:  "Nellymoser 16kHz mono",
		5:  "Nellymoser 8kHz mono",
		6:  "Nellymoser",
		7:  "PCMA",
		8:  "PCMU",
		9:  "reserved",
		10: "AAC",
		11: "Speex",
		14: "MP3 8Khz",
		15: "Device-specific sound"}

	// 采样频率. 2 bit
	SoundRate = map[byte]int{
		0: 5500,
		1: 11000,
		2: 22000,
		3: 44000}

	// 量化精度. 1 bit
	SoundSize = map[byte]string{
		0: "8Bit",
		1: "16Bit"}

	// 音频类型. 1bit
	SoundType = map[byte]string{
		0: "Mono",
		1: "Stereo"}

	// 视频帧类型. 4bit
	FrameType = map[byte]string{
		1: "keyframe (for AVC, a seekable frame)",
		2: "inter frame (for AVC, a non-seekable frame)",
		3: "disposable inter frame (H.263 only)",
		4: "generated keyframe (reserved for server use only)",
		5: "video info/command frame"}

	// 视频编码类型. 4bit
	CodecID = map[byte]string{
		1:  "JPEG (currently unused)",
		2:  "Sorenson H.263",
		3:  "Screen video",
		4:  "On2 VP6",
		5:  "On2 VP6 with alpha channel",
		6:  "Screen video version 2",
		7:  "H264",
		12: "H265"}
)

var FLVHeader = []byte{'F', 'L', 'V', 0x01, 0x05, 0, 0, 0, 9, 0, 0, 0, 0}

func WriteFLVTag(w io.Writer, t byte, timestamp uint32, payload net.Buffers) (err error) {
	head := make([]byte, 11)
	tail := make([]byte, 4)
	head[0] = t
	dataSize := uint32(util.SizeOfBuffers(payload))
	util.PutBE(tail, dataSize+11)
	util.PutBE(head[1:4], dataSize)
	head[4] = byte(timestamp >> 16)
	head[5] = byte(timestamp >> 8)
	head[6] = byte(timestamp)
	head[7] = byte(timestamp >> 24)
	var tag = net.Buffers{head}
	tag = append(tag, payload...)
	tag = append(tag, tail)
	// Tag Data
	_, err = tag.WriteTo(w)
	return
}
func ReadFLVTag(r io.Reader) (t byte, timestamp uint32, payload []byte, err error) {
	head := make([]byte, 11)
	if _, err = io.ReadFull(r, head); err != nil {
		return
	}
	t = head[0]
	dataSize := util.ReadBE[int](head[1:4])
	timestamp = (uint32(head[7]) << 24) | (uint32(head[4]) << 16) | (uint32(head[5]) << 8) | uint32(head[6])
	payload = make([]byte, dataSize)
	if _, err = io.ReadFull(r, payload); err == nil {
		_, err = io.ReadFull(r, head[:4])
	}
	return
}

func VideoAVCC2FLV(avcc net.Buffers, ts uint32) (flv net.Buffers) {
	b := util.Buffer(make([]byte, 0, 15))
	b.WriteByte(FLV_TAG_TYPE_VIDEO)
	dataSize := util.SizeOfBuffers(avcc)
	b.WriteUint24(uint32(dataSize))
	b.WriteUint24(ts)
	b.WriteByte(byte(ts >> 24))
	b.WriteUint24(0)
	return append(append(append(flv, b), avcc...), util.PutBE(b.Malloc(4), dataSize+11))
}
