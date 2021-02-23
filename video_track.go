package engine

import (
	"context"
	"encoding/binary"
	"sort"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
	"github.com/pion/rtp"
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

type TSSlice []uint32

func (s TSSlice) Len() int { return len(s) }

func (s TSSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s TSSlice) Less(i, j int) bool { return s[i] < s[j] }

type VideoPack struct {
	Timestamp       uint32
	CompositionTime uint32
	Payload         []byte //NALU
	NalType         byte
	Sequence        int
}

func (vp *VideoPack) ToRTMPTag() []byte {
	nalu := vp.Payload
	cts := vp.CompositionTime
	payload := utils.GetSlice(9 + len(nalu))
	if nalu[0]&31 == codec.NALU_IDR_Picture {
		payload[0] = 0x17
	} else {
		payload[0] = 0x27
	}
	payload[1] = 0x01
	utils.BigEndian.PutUint24(payload[2:], cts)
	utils.BigEndian.PutUint32(payload[5:], uint32(len(nalu)))
	copy(payload[9:], nalu)
	return payload
}

type VideoTrack struct {
	IDRIndex byte //最近的关键帧位置，首屏渲染
	Track_Video
	SPS     []byte `json:"-"`
	PPS     []byte `json:"-"`
	SPSInfo codec.SPSInfo
	GOP     byte            //关键帧间隔
	RtmpTag []byte          `json:"-"` //rtmp需要先发送一个序列帧，包含SPS和PPS
	WaitIDR context.Context `json:"-"`
	revIDR  func()
	pushRTP func(rtp.Packet)
}

func (vt *VideoTrack) PushRTP(pack rtp.Packet) {
	vt.pushRTP(pack)
}

func NewVideoTrack() *VideoTrack {
	var result VideoTrack
	var cancel context.CancelFunc
	result.Buffer = NewRing_Video()
	result.WaitIDR, cancel = context.WithCancel(context.Background())
	result.revIDR = func() {
		result.IDRIndex = result.Buffer.Index
		cancel()
		result.revIDR = func() {
			result.GOP = result.Buffer.Index - result.IDRIndex
			result.IDRIndex = result.Buffer.Index
		}
	}
	result.pushRTP = result.pushRTP0
	return &result
}

// Push 来自发布者推送的视频
func (vt *VideoTrack) Push(pack VideoPack) {
	payload := pack.Payload
	payloadLen := len(payload)
	if payloadLen == 0 {
		return
	}
	vbr := vt.Buffer
	video := vbr.Current
	video.NalType = payload[0] & naluTypeBitmask
	video.Timestamp = pack.Timestamp
	video.CompositionTime = pack.CompositionTime
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
			pack.Payload = payload[currOffset : currOffset+naluSize]
			vt.Push(pack)
		}
	case codec.NALU_FUA:
		if len(payload) < fuaHeaderSize {
			utils.Printf("Payload is not large enough to be FU-A")
			return
		}
		if payload[1]&fuaStartBitmask != 0 {
			naluRefIdc := payload[0] & naluRefIdcBitmask
			fragmentedNaluType := payload[1] & naluTypeBitmask
			buffer := vbr.GetBuffer()
			payload[fuaHeaderSize-1] = naluRefIdc | fragmentedNaluType
			buffer.Write(payload)
		} else if payload[1]&fuaEndBitmask != 0 {
			pack.Payload = video.Bytes()[fuaHeaderSize-1:]
			vt.Push(pack)
		} else {
			video.Write(payload[fuaHeaderSize:])
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

func (vt *VideoTrack) SetRtmpTag() {
	lenSPS, lenPPS := len(vt.SPS), len(vt.PPS)
	vt.RtmpTag = append([]byte{}, codec.RTMP_AVC_HEAD...)
	copy(vt.RtmpTag[6:], vt.SPS[1:4])
	vt.RtmpTag = append(vt.RtmpTag, 0xE1, byte(lenSPS>>8), byte(lenSPS))
	vt.RtmpTag = append(vt.RtmpTag, vt.SPS...)
	vt.RtmpTag = append(append(vt.RtmpTag, 0x01, byte(lenPPS>>8), byte(lenPPS)), vt.PPS...)
}
func (vt *VideoTrack) pushRTP0(pack rtp.Packet) {
	t := pack.Timestamp / 90
	if t < vt.Buffer.GetLast().Timestamp {
		if vt.WaitIDR.Err() == nil {
			return
		}
		//有B帧
		var tmpVT VideoTrack
		tmpVT.Buffer = NewRing_Video()
		tmpVT.revIDR = func() {
			tmpVT.IDRIndex = tmpVT.Buffer.Index
		}
		// tmpVT.pushRTP = func(p rtp.Packet) {
		// 	tmpVT.Push(VideoPack{Timestamp:p.Timestamp/90,Payload:p.Payload})
		// }
		gopBuffer := tmpVT.Buffer //缓存一个GOP用来计算dts
		var gopFirst byte
		var tsSlice TSSlice
		for i := vt.IDRIndex; vt.Buffer.Index != i; i++ {
			t := vt.Buffer.GetAt(i)
			c := gopBuffer.Current
			c.Payload = append(c.Payload, t.Payload...)
			c.Timestamp = t.Timestamp
			c.NalType = t.NalType
			tsSlice = append(tsSlice, gopBuffer.Current.Timestamp)
			gopBuffer.NextW()
		}
		vt.pushRTP = func(pack rtp.Packet) {
			t := pack.Timestamp / 90
			c := gopBuffer.Current
			vp := VideoPack{Timestamp: t}
			vp.Payload = append(vp.Payload, pack.Payload...)
			tmpVT.Push(vp)
			if c != gopBuffer.Current {
				if c.NalType == codec.NALU_IDR_Picture {
					sort.Sort(tsSlice) //排序后相当于DTS列表
					var offset uint32
					for i := 0; i < len(tsSlice); i++ {
						j := gopFirst + byte(i)
						f := gopBuffer.GetAt(j)
						if f.Timestamp+offset < tsSlice[i] {
							offset = tsSlice[i] - f.Timestamp
						}
					}
					for i := 0; i < len(tsSlice); i++ {
						f := gopBuffer.GetAt(gopFirst + byte(i))
						f.CompositionTime = f.Timestamp + offset - tsSlice[i]
						f.Timestamp = tsSlice[i]
						vt.Push(f.VideoPack)
					}
					gopFirst = gopBuffer.Index - 1
					tsSlice = nil
				}
				tsSlice = append(tsSlice, t)
			}
		}
		vt.pushRTP(pack)
		return
	}
	vt.Push(VideoPack{Timestamp: t, Payload: pack.Payload})
}
