package engine

import (
	"encoding/binary"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
)

const (
	fuaHeaderSize       = 2
	stapaHeaderSize     = 1
	stapaNALULengthSize = 2

	naluTypeBitmask   = 0x1F
	naluRefIdcBitmask = 0x60
	fuaStartBitmask   = 0x80 //1000 0000
	fuaEndBitmask     = 0x40 //0100 0000
)

type VideoPack struct {
	Timestamp uint32
	Payload   []byte //NALU
	NalType   byte
	Sequence  int
}
type VideoTrack struct {
	FirstScreen byte //最近的关键帧位置，首屏渲染
	Track_Video
	SPS       []byte `json:"-"`
	PPS       []byte `json:"-"`
	SPSInfo   codec.SPSInfo
	GOP       byte          //关键帧间隔
	RtmpTag   []byte        `json:"-"` //rtmp需要先发送一个序列帧，包含SPS和PPS
	WaitFirst chan struct{} `json:"-"`
	revIDR    func()
}

func NewVideoTrack() *VideoTrack {
	result := &VideoTrack{
		WaitFirst: make(chan struct{}),
	}
	result.Buffer = NewRing_Video()
	result.revIDR = result.firstRevIDR
	return result
}

// Push 来自发布者推送的视频
func (vt *VideoTrack) Push(timestamp uint32, payload []byte) {
	payloadLen := len(payload)
	if payloadLen == 0 {
		return
	}
	vbr := vt.Buffer
	video := vbr.Current
	video.NalType = payload[0] & naluTypeBitmask
	video.Timestamp = timestamp
	video.Sequence = vt.PacketCount
	switch video.NalType {
	case codec.NALU_STAPA:
		for currOffset, naluSize := stapaHeaderSize, 0; currOffset < len(payload); currOffset += naluSize {
			naluSize = int(binary.BigEndian.Uint16(payload[currOffset:]))
			currOffset += stapaNALULengthSize
			if currOffset+len(payload) < currOffset+naluSize {
				utils.Printf("STAP-A declared size(%d) is larger then buffer(%d)", naluSize, len(payload)-currOffset)
				return
			}
			vt.Push(timestamp, payload[currOffset:currOffset+naluSize])
		}
	case codec.NALU_FUA:
		if len(payload) < fuaHeaderSize {
			utils.Printf("Payload is not large enough to be FU-A")
			return
		}
		if payload[1]&fuaStartBitmask != 0 {
			naluRefIdc := payload[0] & naluRefIdcBitmask
			fragmentedNaluType := payload[1] & naluTypeBitmask
			video.Payload = append([]byte{}, payload...)
			video.Payload[fuaHeaderSize-1] = naluRefIdc | fragmentedNaluType
		} else {
			video.Payload = append(video.Payload, payload[fuaHeaderSize:]...)
		}
		if payload[1]&fuaEndBitmask != 0 {
			vt.Push(timestamp, video.Payload[fuaHeaderSize-1:])
		}
	case codec.NALU_SPS:
		vt.SPS = payload
		vt.SPSInfo, _ = codec.ParseSPS(payload)
	case codec.NALU_PPS:
		vt.PPS = payload
		if vt.RtmpTag == nil {
			vt.SetRtmpTag()
		}
	case codec.NALU_Access_Unit_Delimiter:

	case codec.NALU_IDR_Picture:
		vt.revIDR()
		fallthrough
	case codec.NALU_Non_IDR_Picture:
		video.Payload = payload
		vt.Track_Video.GetBPS(payloadLen)
		vbr.NextW()
	case codec.NALU_SEI:
	case codec.NALU_Filler_Data:
	default:
		utils.Printf("nalType not support yet:%d", video.NalType)
	}
}
func (vt *VideoTrack) firstRevIDR() {
	vt.FirstScreen = vt.Buffer.Index
	close(vt.WaitFirst)
	vt.revIDR = vt.afterRevIDR
}
func (vt *VideoTrack) afterRevIDR() {
	vt.GOP = vt.Buffer.Index - vt.FirstScreen
	vt.FirstScreen = vt.Buffer.Index
}
func (vt *VideoTrack) SetRtmpTag() {
	lenSPS, lenPPS := len(vt.SPS), len(vt.PPS)
	vt.RtmpTag = append([]byte{}, codec.RTMP_AVC_HEAD...)
	copy(vt.RtmpTag[6:], vt.SPS[1:4])
	vt.RtmpTag = append(vt.RtmpTag, 0xE1, byte(lenSPS>>8), byte(lenSPS))
	vt.RtmpTag = append(vt.RtmpTag, vt.SPS...)
	vt.RtmpTag = append(append(vt.RtmpTag, 0x01, byte(lenPPS>>8), byte(lenPPS)), vt.PPS...)
}
