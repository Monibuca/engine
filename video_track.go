package engine

import (
	"bytes"
	"container/ring"
	"context"
	"encoding/binary"

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

type VideoPack struct {
	BasePack
	CompositionTime uint32
	NALUs           [][]byte
	IDR             bool // 是否关键帧
}

func (vp VideoPack) Copy(ts uint32) VideoPack {
	vp.Timestamp = vp.Since(ts)
	return vp
}

type VideoTrack struct {
	IDRing *ring.Ring //最近的关键帧位置，首屏渲染
	Track_Base
	SPSInfo         codec.SPSInfo
	GOP             int             //关键帧间隔
	ExtraData       *VideoPack      `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS)
	WaitIDR         context.Context `json:"-"`
	revIDR          func()
	PushByteStream  func(ts uint32, payload []byte)              `json:"-"`
	PushNalu        func(ts uint32, cts uint32, nalus ...[]byte) `json:"-"`
	UsingDonlField  bool
	writeByteStream func(pack *VideoPack)
}

func (vt *VideoTrack) initVideoRing(v interface{}) {
	pack := new(VideoPack)
	if vt.writeByteStream != nil {
		pack.Buffer = bytes.NewBuffer([]byte{})
	}
	v.(*RingItem).Value = pack
}
func (s *Stream) NewVideoTrack(codec byte) (vt *VideoTrack) {
	var cancel context.CancelFunc
	vt = &VideoTrack{
		revIDR: func() {
			vt.IDRing = vt.Ring
			cancel()
			idrSequence := vt.current().Sequence
			l := vt.Ring.Len()
			vt.revIDR = func() {
				current := vt.current()
				if vt.GOP = current.Sequence - idrSequence; vt.GOP > l-1 {
					//缓冲环不够大，导致IDR被覆盖
					exRing := NewRingBuffer(vt.GOP - l + 5).Ring
					exRing.Do(vt.initVideoRing)
					vt.Link(exRing) // 扩大缓冲环
					l = vt.Ring.Len()
					utils.Printf("%s ring grow to %d", s.StreamPath, l)
				} else if vt.GOP < l-5 {
					vt.Unlink(l - vt.GOP - 5) //缩小缓冲环节省内存
					l = vt.Ring.Len()
					utils.Printf("%s ring atrophy to %d", s.StreamPath, l)
				}
				vt.IDRing = vt.Ring
				idrSequence = current.Sequence
				vt.ts = current.Timestamp
				vt.bytes = 0
			}
		},
	}
	vt.PushByteStream = vt.pushByteStream
	vt.PushNalu = vt.pushNalu
	vt.Stream = s
	vt.CodecID = codec
	vt.Init(256)
	vt.Do(vt.initVideoRing)
	vt.WaitIDR, cancel = context.WithCancel(context.Background())
	switch codec {
	case 7:
		s.VideoTracks.AddTrack("h264", vt)
	case 12:
		s.VideoTracks.AddTrack("h265", vt)
	}
	return
}

func (vt *VideoTrack) PushAnnexB(ts uint32, cts uint32, payload []byte) {
	vt.PushNalu(ts, cts, codec.SplitH264(payload)...)
}

func (vt *VideoTrack) pushNalu(ts uint32, cts uint32, nalus ...[]byte) {
	idrBit := 0x10 | vt.CodecID
	nIdrBit := 0x20 | vt.CodecID
	tmp := make([]byte, 4)
	// 缓冲中只包含Nalu数据所以写入rtmp格式时需要按照ByteStream格式写入
	vt.writeByteStream = func(pack *VideoPack) {
		pack.Reset()
		if pack.IDR {
			tmp[0] = idrBit
		} else {
			tmp[0] = nIdrBit
		}
		tmp[1] = 1
		pack.Write(tmp[:2])
		utils.BigEndian.PutUint24(tmp, pack.CompositionTime)
		pack.Write(tmp[:3])
		for _, nalu := range pack.NALUs {
			utils.BigEndian.PutUint32(tmp, uint32(len(nalu)))
			pack.Write(tmp)
			pack.Write(nalu)
		}
		pack.Payload = pack.Bytes()
	}
	vt.Do(func(v interface{}) {
		v.(*RingItem).Value.(*VideoPack).Buffer = bytes.NewBuffer([]byte{})
	})
	switch vt.CodecID {
	case 7:
		{
			var info codec.AVCDecoderConfigurationRecord
			vt.PushNalu = func(ts uint32, cts uint32, nalus ...[]byte) {
				// 等待接收SPS和PPS数据
				for _, nalu := range nalus {
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
						NALUs: [][]byte{info.SequenceParameterSetNALUnit, info.PictureParameterSetNALUnit},
					}
					vt.ExtraData.Payload = codec.BuildH264SeqHeaderFromSpsPps(info.SequenceParameterSetNALUnit, info.PictureParameterSetNALUnit)
				}
				if vt.ExtraData == nil {
					return
				}
				var fuaBuffer *bytes.Buffer
				var mSync = false
				//已完成SPS和PPS 组装，重置push函数，接收视频数据
				vt.PushNalu = func(ts uint32, cts uint32, nalus ...[]byte) {
					var nonIDRs [][]byte
					fuaHeaderSize := 2
					stapaHeaderSize := 1
					mTAP16LengthSize := 4
					for _, nalu := range nalus {
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
							vt.PushNalu(ts, cts, nalus...)
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
								vt.PushNalu(uint32(ts), 0, nalu[currOffset:currOffset+naluSize])
							}
							/*
								0                   1                   2                   3
								0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
								+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
								|   PayloadHdr (Type=29)        |   FU header   | DONL (cond)   |
								+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-|
								|   DONL (cond) |                                               |
								|-+-+-+-+-+-+-+-+                                               |
								|                         FU payload                            |
								|                                                               |
								|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
								|                               :...OPTIONAL RTP padding        |
								+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
							*/
						case codec.NALU_FUB:
							fuaHeaderSize = 4
							fallthrough
							/*
								0                   1                   2                   3
								0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
								+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
								|   PayloadHdr (Type=28)        |         NALU 1 Size           |
								+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
								|          NALU 1 HDR           |                               |
								+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+         NALU 1 Data           |
								|                   . . .                                       |
								|                                                               |
								+               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
								|  . . .        | NALU 2 Size                   | NALU 2 HDR    |
								+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
								| NALU 2 HDR    |                                               |
								+-+-+-+-+-+-+-+-+              NALU 2 Data                      |
								|                   . . .                                       |
								|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
								|                               :...OPTIONAL RTP padding        |
								+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
							*/
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
								vt.PushNalu(ts, cts, fuaBuffer.Bytes()[fuaHeaderSize-1:])
							}
						case codec.NALU_Access_Unit_Delimiter:
						case codec.NALU_IDR_Picture:
							vt.bytes += len(nalu)
							pack := vt.current()
							pack.IDR = true
							pack.Timestamp = ts
							pack.CompositionTime = cts
							if cap(pack.NALUs) > 0 {
								pack.NALUs = pack.NALUs[:1]
								pack.NALUs[0] = nalu
							} else {
								pack.NALUs = [][]byte{nalu}
							}
							vt.push(pack)
						case codec.NALU_Non_IDR_Picture:
							nonIDRs = append(nonIDRs, nalu)
							vt.bytes += len(nalu)
						case codec.NALU_SEI:
						case codec.NALU_Filler_Data:
						default:
							utils.Printf("nalType not support yet:%d", naluType)
						}
						if len(nonIDRs) > 0 {
							pack := vt.current()
							pack.IDR = false
							pack.Timestamp = ts
							pack.CompositionTime = cts
							pack.NALUs = nonIDRs
							vt.push(pack)
						}
					}
				}
			}
		}
	case 12:
		var vps, sps, pps []byte
		vt.PushNalu = func(ts uint32, cts uint32, nalus ...[]byte) {
			// 等待接收SPS和PPS数据
			for _, nalu := range nalus {
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
					NALUs: [][]byte{vps, sps, pps},
				}
				vt.ExtraData.Payload = extraData
			}
			if vt.ExtraData != nil {
				var fuaBuffer *bytes.Buffer
				vt.PushNalu = func(ts uint32, cts uint32, nalus ...[]byte) {
					var nonIDRs [][]byte
					for _, nalu := range nalus {
						/*
						   0               1
						   0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5
						   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
						   |F|    Type   |  LayerId  | TID |
						   +-------------+-----------------+
						   Forbidden zero(F) : 1 bit
						   NAL unit type(Type) : 6 bits
						   NUH layer ID(LayerId) : 6 bits
						   NUH temporal ID plus 1 (TID) : 3 bits
						*/
						naluType := nalu[0] & naluTypeBitmask_hevc >> 1
						if len(nalu) == 0 {
							continue
						}
						switch naluType {
						// 4.4.2. Aggregation Packets (APs) (p25)
						/*
						    0               1               2               3
						    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
						   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
						   |      PayloadHdr (Type=48)     |           NALU 1 DONL         |
						   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
						   |           NALU 1 Size         |            NALU 1 HDR         |
						   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
						   |                                                               |
						   |                         NALU 1 Data . . .                     |
						   |                                                               |
						   +     . . .     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
						   |               |  NALU 2 DOND  |            NALU 2 Size        |
						   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
						   |          NALU 2 HDR           |                               |
						   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+            NALU 2 Data        |
						   |                                                               |
						   |         . . .                 +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
						   |                               :    ...OPTIONAL RTP padding    |
						   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
						*/
						case codec.NAL_UNIT_UNSPECIFIED_48:
							currOffset := 2
							if vt.UsingDonlField {
								currOffset = 4
							}
							var nalus [][]byte
							for naluSize := 0; currOffset < len(nalu); currOffset += naluSize {
								naluSize = int(binary.BigEndian.Uint16(nalu[currOffset:]))
								currOffset += 2
								if currOffset+len(nalu) < currOffset+naluSize {
									utils.Printf("STAP-A declared size(%d) is larger then buffer(%d)", naluSize, len(nalu)-currOffset)
									return
								}
								nalus = append(nalus, nalu[currOffset:currOffset+naluSize])
								if vt.UsingDonlField {
									currOffset += 1
								}
							}
							vt.PushNalu(ts, cts, nalus...)

							// 4.4.3. Fragmentation Units (p29)
							/*
							    0               1               2               3
							    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
							   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
							   |     PayloadHdr (Type=49)      |    FU header  |  DONL (cond)  |
							   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-|
							   |  DONL (cond)  |                                               |
							   |-+-+-+-+-+-+-+-+                                               |
							   |                           FU payload                          |
							   |                                                               |
							   |                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
							   |                               :    ...OPTIONAL RTP padding    |
							   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
							   +---------------+
							   |0|1|2|3|4|5|6|7|
							   +-+-+-+-+-+-+-+-+
							   |S|E|   FuType  |
							   +---------------+
							*/
						case codec.NAL_UNIT_UNSPECIFIED_49:
							offset := 3
							if vt.UsingDonlField {
								offset = 5
							}
							if len(nalu) < offset {
								continue
							}
							S := nalu[offset]&fuaStartBitmask != 0
							E := nalu[offset]&fuaEndBitmask != 0
							naluType = nalu[offset] & 0b00111111
							if S {
								fuaBuffer = bytes.NewBuffer([]byte{})
								nalu[0] = nalu[0]&0b10000001 | (naluType << 1)
								fuaBuffer.Write(nalu[:2])
							}
							fuaBuffer.Write(nalu[offset:])
							if E {
								vt.PushNalu(ts, cts, fuaBuffer.Bytes())
							}
						case codec.NAL_UNIT_CODED_SLICE_BLA,
							codec.NAL_UNIT_CODED_SLICE_BLANT,
							codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
							codec.NAL_UNIT_CODED_SLICE_IDR,
							codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
							codec.NAL_UNIT_CODED_SLICE_CRA:
							pack := vt.current()
							pack.IDR = true
							pack.Timestamp = ts
							pack.CompositionTime = cts
							if cap(pack.NALUs) > 0 {
								pack.NALUs = pack.NALUs[:1]
								pack.NALUs[0] = nalu
							} else {
								pack.NALUs = [][]byte{nalu}
							}
							vt.push(pack)
						case 0, 1, 2, 3, 4, 5, 6, 7, 9:
							nonIDRs = append(nonIDRs, nalu)
						}
					}
					if len(nonIDRs) > 0 {
						pack := vt.current()
						pack.IDR = false
						pack.Timestamp = ts
						pack.CompositionTime = cts
						pack.NALUs = nonIDRs
						vt.push(pack)
					}
				}
			}
		}
	}
	vt.PushNalu(ts, cts, nalus...)
}
func (vt *VideoTrack) current() *VideoPack {
	return vt.CurrentValue().(*VideoPack)
}
func (vt *VideoTrack) pushByteStream(ts uint32, payload []byte) {
	if payload[1] != 0 {
		return
	} else {
		vt.CodecID = payload[0] & 0x0F
		var nalulenSize int
		switch vt.CodecID {
		case 7:
			var info codec.AVCDecoderConfigurationRecord
			if _, err := info.Unmarshal(payload[5:]); err == nil {
				vt.SPSInfo, _ = codec.ParseSPS(info.SequenceParameterSetNALUnit)
				nalulenSize = int(info.LengthSizeMinusOne&3 + 1)
				vt.ExtraData = &VideoPack{
					NALUs: [][]byte{info.SequenceParameterSetNALUnit, info.PictureParameterSetNALUnit},
				}
				vt.ExtraData.Payload = payload
				vt.Stream.VideoTracks.AddTrack("h264", vt)
			}
		case 12:
			if vps, sps, pps, err := codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(payload); err == nil {
				vt.SPSInfo, _ = codec.ParseSPS(sps)
				nalulenSize = int(payload[26]) & 0x03
				vt.ExtraData = &VideoPack{
					NALUs: [][]byte{vps, sps, pps},
				}
				vt.ExtraData.Payload = payload
				vt.Stream.VideoTracks.AddTrack("h265", vt)
			}
		}
		// 已完成序列帧组装，重置Push函数，从Payload中提取Nalu供非bytestream格式使用
		vt.PushByteStream = func(ts uint32, payload []byte) {
			pack := vt.current()
			if len(payload) < 4 {
				return
			}
			vt.bytes += len(payload)
			pack.IDR = payload[0]>>4 == 1
			pack.Timestamp = ts
			pack.Sequence = vt.PacketCount
			pack.Payload = payload
			pack.CompositionTime = utils.BigEndian.Uint24(payload[2:])
			pack.NALUs = nil
			for nalus := payload[5:]; len(nalus) > nalulenSize; {
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

func (vt *VideoTrack) push(pack *VideoPack) {
	if vt.Stream != nil {
		vt.Stream.Update()
	}
	if vt.writeByteStream != nil {
		vt.writeByteStream(pack)
	}
	vt.GetBPS()
	if pack.Sequence = vt.PacketCount; pack.IDR {
		vt.revIDR()
	}
	vt.lastTs = pack.Timestamp
	vt.Step()
}
