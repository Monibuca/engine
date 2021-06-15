package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
)

const (
	stapaNALULengthSize = 2

	naluTypeBitmask      = 0b0001_1111
	naluTypeBitmask_hevc = 0x7E
	naluRefIdcBitmask    = 0x60
	fuaStartBitmask      = 0b1000_0000
	fuaEndBitmask        = 0b0100_0000
)

type TSSlice []uint32

func (s TSSlice) Len() int { return len(s) }

func (s TSSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s TSSlice) Less(i, j int) bool { return s[i] < s[j] }

type VideoPack struct {
	Timestamp       uint32
	CompositionTime uint32
	Payload         []byte
	NALUs           [][]byte
	IDR             bool // 是否关键帧
	Sequence        int
}

func (vp VideoPack) Clone() VideoPack {
	return vp
}

func (vp VideoPack) Copy(ts uint32) VideoPack {
	vp.Timestamp = vp.Timestamp - ts
	return vp
}

type VideoTrack struct {
	IDRIndex byte //最近的关键帧位置，首屏渲染
	Track_Video
	SPSInfo         codec.SPSInfo
	GOP             byte            //关键帧间隔
	ExtraData       *VideoPack      `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS)
	WaitIDR         context.Context `json:"-"`
	revIDR          func()
	PushByteStream  func(pack VideoPack)
	PushNalu        func(pack VideoPack)
	WriteByteStream func(writer io.Writer, pack VideoPack) //使用函数写入，避免申请内存

}

func (s *Stream) NewVideoTrack(codec byte) (vt *VideoTrack) {
	var cancel context.CancelFunc
	vt = &VideoTrack{
		revIDR: func() {
			vt.IDRIndex = vt.Buffer.Index
			cancel()
			vt.revIDR = func() {
				vt.GOP = vt.Buffer.Index - vt.IDRIndex
				vt.IDRIndex = vt.Buffer.Index
			}
		},
	}
	vt.PushByteStream = vt.pushByteStream
	vt.PushNalu = vt.pushNalu
	vt.Stream = s
	vt.CodecID = codec
	vt.Buffer = NewRing_Video()
	vt.WaitIDR, cancel = context.WithCancel(context.Background())
	switch codec {
	case 7:
		s.VideoTracks.AddTrack("h264", vt)
	case 12:
		s.VideoTracks.AddTrack("h265", vt)
	}
	return
}

func (vt *VideoTrack) PushAnnexB(pack VideoPack) {
	pack.NALUs = codec.SplitH264(pack.Payload)
	pack.Payload = nil
	vt.PushNalu(pack)
}

func (vt *VideoTrack) pushNalu(pack VideoPack) {
	// 缓冲中只包含Nalu数据所以写入rtmp格式时需要按照ByteStream格式写入
	vt.WriteByteStream = func(writer io.Writer, pack VideoPack) {
		tmp := utils.GetSlice(4)
		defer utils.RecycleSlice(tmp)
		if pack.IDR {
			tmp[0] = 0x10 | vt.CodecID
		} else {
			tmp[0] = 0x20 | vt.CodecID
		}
		tmp[1] = 1
		writer.Write(tmp[:2])
		cts := pack.CompositionTime
		utils.BigEndian.PutUint24(tmp, cts)
		writer.Write(tmp[:3])
		for _, nalu := range pack.NALUs {
			utils.BigEndian.PutUint32(tmp, uint32(len(nalu)))
			writer.Write(tmp)
			writer.Write(nalu)
		}
	}
	switch vt.CodecID {
	case 7:
		{
			var info codec.AVCDecoderConfigurationRecord
			vt.PushNalu = func(pack VideoPack) {
				// 等待接收SPS和PPS数据
				for _, nalu := range pack.NALUs {
					if len(nalu) == 0 {
						continue
					}
					switch nalu[0] & naluTypeBitmask {
					case codec.NALU_SPS:
						info.SequenceParameterSetNALUnit = nalu
						info.SequenceParameterSetLength = uint16(len(nalu))
						vt.SPSInfo, _ = codec.ParseSPS(nalu)
					case codec.NALU_PPS:
						info.PictureParameterSetNALUnit = nalu
						info.PictureParameterSetLength = uint16(len(nalu))
					}
				}
				if info.SequenceParameterSetNALUnit != nil && info.PictureParameterSetNALUnit != nil {
					vt.ExtraData = &VideoPack{
						Payload: codec.BuildH264SeqHeaderFromSpsPps(info.SequenceParameterSetNALUnit, info.PictureParameterSetNALUnit),
						NALUs:   [][]byte{info.SequenceParameterSetNALUnit, info.PictureParameterSetNALUnit},
					}
				}
				if vt.ExtraData == nil {
					return
				}
				var fuaBuffer *bytes.Buffer
				var mSync = false
				//已完成SPS和PPS 组装，重置push函数，接收视频数据
				vt.PushNalu = func(pack VideoPack) {
					var nonIDRs [][]byte
					fuaHeaderSize := 2
					stapaHeaderSize := 1
					mTAP16LengthSize := 4
					for _, nalu := range pack.NALUs {
						if len(nalu) == 0 {
							continue
						}
						naluType := nalu[0] & naluTypeBitmask
						switch naluType {
						case codec.NALU_SPS:
						case codec.NALU_PPS:
						case codec.NALU_STAPB:
							stapaHeaderSize = 3
							fallthrough
						case codec.NALU_STAPA:
							var nalus [][]byte
							for currOffset, naluSize := stapaHeaderSize, 0; currOffset < len(nalu); currOffset += naluSize {
								naluSize = int(binary.BigEndian.Uint16(nalu[currOffset:]))
								currOffset += stapaNALULengthSize
								if currOffset+len(nalu) < currOffset+naluSize {
									utils.Printf("STAP-A declared size(%d) is larger then buffer(%d)", naluSize, len(nalu)-currOffset)
									return
								}
								nalus = append(nalus, nalu[currOffset:currOffset+naluSize])
							}
							p := pack.Clone()
							p.NALUs = nalus
							vt.PushNalu(p)
						case codec.MTAP24:
							mTAP16LengthSize = 5
							fallthrough
						case codec.MTAP16:
							for currOffset, naluSize := 3, 0; currOffset < len(nalu); currOffset += naluSize {
								naluSize = int(binary.BigEndian.Uint16(nalu[currOffset:]))
								currOffset += mTAP16LengthSize
								if currOffset+len(nalu) < currOffset+naluSize {
									utils.Printf("MTAP16 declared size(%d) is larger then buffer(%d)", naluSize, len(nalu)-currOffset)
									return
								}
								ts := binary.BigEndian.Uint16(nalu[currOffset+3:])
								if mTAP16LengthSize == 5 {
									ts = (ts << 8) | uint16(nalu[currOffset+5])
								}
								p := pack.Clone()
								p.Timestamp = uint32(ts)
								p.NALUs = [][]byte{nalu[currOffset : currOffset+naluSize]}
								vt.PushNalu(p)
							}
						case codec.NALU_FUB:
							fuaHeaderSize = 4
							fallthrough
						case codec.NALU_FUA:
							if len(nalu) < fuaHeaderSize {
								utils.Printf("Payload is not large enough to be FU-A")
								return
							}
							S := nalu[1]&fuaStartBitmask != 0
							E := nalu[1]&fuaEndBitmask != 0
							if S {
								fuaBuffer = bytes.NewBuffer([]byte{})
								naluRefIdc := nalu[0] & naluRefIdcBitmask
								fragmentedNaluType := nalu[1] & naluTypeBitmask
								nalu[fuaHeaderSize-1] = naluRefIdc | fragmentedNaluType
								fuaBuffer.Write(nalu)
								mSync = true
							}
							fuaBuffer.Write(nalu[fuaHeaderSize:])
							if E && mSync {
								p := pack.Clone()
								p.NALUs = [][]byte{fuaBuffer.Bytes()[fuaHeaderSize-1:]}
								vt.PushNalu(p)
							}
						case codec.NALU_Access_Unit_Delimiter:
						case codec.NALU_IDR_Picture:
							p := pack.Clone()
							p.IDR = true
							p.NALUs = [][]byte{nalu}
							vt.push(p)
						case codec.NALU_Non_IDR_Picture:
							nonIDRs = append(nonIDRs, nalu)
						case codec.NALU_SEI:
						case codec.NALU_Filler_Data:
						default:
							utils.Printf("nalType not support yet:%d", naluType)
						}
						if len(nonIDRs) > 0 {
							pack.NALUs = nonIDRs
							vt.push(pack)
						}
					}
				}
			}
		}
	case 12:
		var vps, sps, pps []byte
		vt.PushNalu = func(pack VideoPack) {
			// 等待接收SPS和PPS数据
			for _, nalu := range pack.NALUs {
				if len(nalu) == 0 {
					continue
				}
				switch nalu[0] & naluTypeBitmask_hevc >> 1 {
				case codec.NAL_UNIT_VPS:
					vps = nalu
				case codec.NAL_UNIT_SPS:
					sps = nalu
					vt.SPSInfo, _ = codec.ParseSPS(nalu)
				case codec.NAL_UNIT_PPS:
					pps = nalu
				}
			}
			if vps != nil && sps != nil && pps != nil {
				extraData, err := codec.BuildH265SeqHeaderFromVpsSpsPps(vps, sps, pps)
				if err != nil {
					return
				}
				vt.ExtraData = &VideoPack{
					Payload: extraData,
					NALUs:   [][]byte{vps, sps, pps},
				}
				var fuaBuffer *bytes.Buffer
				vt.PushNalu = func(pack VideoPack) {
					var nonIDRs [][]byte
					for _, nalu := range pack.NALUs {
						naluType := nalu[0] & naluTypeBitmask_hevc >> 1
						if len(nalu) == 0 {
							continue
						}
						switch naluType {
						case codec.NAL_UNIT_UNSPECIFIED_49:
							if len(nalu) < 3 {
								continue
							}
							S := nalu[3]&fuaStartBitmask != 0
							E := nalu[3]&fuaEndBitmask != 0
							naluType = nalu[3] & 0b00111111
							if S {
								fuaBuffer = bytes.NewBuffer([]byte{})
								nalu[0] = nalu[0]&0b10000001 | (naluType << 1)
								fuaBuffer.Write(nalu[:2])
								fuaBuffer.Write(nalu[3:])
							} else if E {
								fuaBuffer.Write(nalu[3:])
								pack.NALUs = [][]byte{fuaBuffer.Bytes()}
								vt.PushNalu(pack)
							} else {
								fuaBuffer.Write(nalu[3:])
							}
						case codec.NAL_UNIT_CODED_SLICE_BLA,
							codec.NAL_UNIT_CODED_SLICE_BLANT,
							codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
							codec.NAL_UNIT_CODED_SLICE_IDR,
							codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
							codec.NAL_UNIT_CODED_SLICE_CRA:
							p := pack.Clone()
							p.IDR = true
							p.NALUs = [][]byte{nalu}
							vt.push(p)
						case 0, 1, 2, 3, 4, 5, 6, 7, 9:
							nonIDRs = append(nonIDRs, nalu)
						}
					}
					if len(nonIDRs) > 0 {
						pack.NALUs = nonIDRs
						vt.push(pack)
					}
				}
			}
		}
	}
	vt.PushNalu(pack)
}

func (vt *VideoTrack) pushByteStream(pack VideoPack) {
	if pack.Payload[1] != 0 {
		return
	} else {
		vt.CodecID = pack.Payload[0] & 0x0F
		var nalulenSize int
		switch vt.CodecID {
		case 7:
			var info codec.AVCDecoderConfigurationRecord
			if _, err := info.Unmarshal(pack.Payload[5:]); err == nil {
				vt.SPSInfo, _ = codec.ParseSPS(info.SequenceParameterSetNALUnit)
				pack.NALUs = append(pack.NALUs, info.SequenceParameterSetNALUnit, info.PictureParameterSetNALUnit)
				nalulenSize = int(info.LengthSizeMinusOne&3 + 1)
				vt.ExtraData = &pack
				vt.Stream.VideoTracks.AddTrack("h264", vt)
			}
		case 12:
			if vps, sps, pps, err := codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(pack.Payload); err == nil {
				pack.NALUs = append(pack.NALUs, vps, sps, pps)
				vt.SPSInfo, _ = codec.ParseSPS(sps)
				nalulenSize = int(pack.Payload[26]) & 0x03
				vt.ExtraData = &pack
				vt.Stream.VideoTracks.AddTrack("h265", vt)
			}
		}
		vt.WriteByteStream = func(writer io.Writer, pack VideoPack) {
			writer.Write(pack.Payload)
		}
		// 已完成序列帧组装，重置Push函数，从Payload中提取Nalu供非bytestream格式使用
		vt.PushByteStream = func(pack VideoPack) {
			if len(pack.Payload) < 4 {
				return
			}
			vt.GetBPS(len(pack.Payload))
			pack.IDR = pack.Payload[0]>>4 == 1
			pack.CompositionTime = utils.BigEndian.Uint24(pack.Payload[2:])
			nalus := pack.Payload[5:]
			for len(nalus) > nalulenSize {
				nalulen := 0
				for i := 0; i < nalulenSize; i++ {
					nalulen += int(nalus[i]) << (8 * (nalulenSize - i - 1))
				}
				pack.NALUs = append(pack.NALUs, nalus[nalulenSize:nalulen+nalulenSize])
				nalus = nalus[nalulen+nalulenSize:]
			}
			vt.push(pack)
		}
	}
}

func (vt *VideoTrack) push(pack VideoPack) {
	if vt.Stream != nil {
		vt.Stream.Update()
	}
	vbr := vt.Buffer
	video := vbr.Current
	if vt.Stream.prePayload > 0 && len(pack.Payload) == 0 {
		buffer := vbr.GetBuffer()
		vt.WriteByteStream(buffer, pack)
		video.VideoPack = pack
		video.VideoPack.Payload = buffer.Bytes()
	} else {
		video.VideoPack = pack
	}
	video.Sequence = vt.PacketCount
	if pack.IDR {
		vt.revIDR()
	}
	vbr.NextW()
}
