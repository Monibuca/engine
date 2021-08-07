package engine

import (
	"container/ring"
	"time"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
)

const (
	naluTypeBitmask      = 0b0001_1111
	naluTypeBitmask_hevc = 0x7E
)

type VideoPack struct {
	AVPack
	CompositionTime uint32
	NALUs           [][]byte
	IDR             bool // 是否关键帧
}

func (v *VideoPack) ResetNALUs() {
	if cap(v.NALUs) > 0 {
		v.NALUs = v.NALUs[:0]
	}
}

func (v *VideoPack) SetNalu0(nalu []byte) {
	if cap(v.NALUs) > 0 {
		v.NALUs = v.NALUs[:1]
		v.NALUs[0] = nalu
	} else {
		v.NALUs = [][]byte{nalu}
	}
}

type VideoTrack struct {
	IDRing *ring.Ring `json:"-"` //最近的关键帧位置，首屏渲染
	AVTrack
	SPSInfo         codec.SPSInfo
	GOP             int           //关键帧间隔
	ExtraData       *VideoPack    `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS)
	WaitIDR         chan struct{} `json:"-"`
	revIDR          func()
	PushNalu        func(ts uint32, cts uint32, nalus ...[]byte) `json:"-"`
	UsingDonlField  bool
	writeByteStream func()
	idrCount        int //处于缓冲中的关键帧数量
	nalulenSize     int
	*VideoPack      `json:"-"` //当前写入的视频数据
}

func (s *Stream) NewVideoTrack(codec byte) (vt *VideoTrack) {
	vt = &VideoTrack{
		WaitIDR: make(chan struct{}),
		revIDR: func() {
			vt.IDRing = vt.Ring
			close(vt.WaitIDR)
			idrSequence := vt.Sequence
			vt.ts = vt.Timestamp
			vt.idrCount++
			vt.revIDR = func() {
				vt.idrCount++
				vt.GOP = vt.Sequence - idrSequence
				if l := vt.Ring.Len() - vt.GOP - 5; l > 5 {
					//缩小缓冲环节省内存
					vt.Unlink(l).Do(func(v interface{}) {
						if v.(*AVItem).Value.(*VideoPack).IDR {
							vt.idrCount--
						}
					})
				}
				vt.IDRing = vt.Ring
				idrSequence = vt.Sequence
				vt.resetBPS()
			}
		},
	}
	vt.PushNalu = vt.pushNalu
	vt.Stream = s
	vt.CodecID = codec
	vt.Init(s.Context, 256)
	vt.poll = time.Millisecond * 20
	vt.Do(func(v interface{}) {
		v.(*AVItem).Value = new(VideoPack)
	})
	vt.setCurrent()
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
	vt.writeByteStream = func() {
		vt.Reset()
		if vt.IDR {
			tmp[0] = idrBit
		} else {
			tmp[0] = nIdrBit
		}
		tmp[1] = 1
		vt.Write(tmp[:2])
		utils.BigEndian.PutUint24(tmp, vt.CompositionTime)
		vt.Write(tmp[:3])
		for _, nalu := range vt.NALUs {
			utils.BigEndian.PutUint32(tmp, uint32(len(nalu)))
			vt.Write(tmp)
			vt.Write(nalu)
		}
		vt.Bytes2Payload()
	}
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
				//已完成SPS和PPS 组装，重置push函数，接收视频数据
				vt.PushNalu = func(ts uint32, cts uint32, nalus ...[]byte) {
					var nonIDRs int
					for _, nalu := range nalus {
						naluLen := len(nalu)
						if naluLen == 0 {
							continue
						}
						naluType := nalu[0] & naluTypeBitmask
						switch naluType {
						case codec.NALU_SPS:
							vt.ExtraData.NALUs[0] = nalu
							vt.SPSInfo, _ = codec.ParseSPS(nalu)
						case codec.NALU_PPS:
							vt.ExtraData.NALUs[1] = nalu
							vt.ExtraData.Payload = codec.BuildH264SeqHeaderFromSpsPps(vt.ExtraData.NALUs[0], vt.ExtraData.NALUs[1])
						case codec.NALU_Access_Unit_Delimiter:
						case codec.NALU_IDR_Picture:
							if nonIDRs > 0 {
								vt.push()
								nonIDRs = 0
							}
							vt.addBytes(naluLen)
							vt.IDR = true
							vt.SetTs(ts)
							vt.CompositionTime = cts
							vt.SetNalu0(nalu)
							vt.push()
						case codec.NALU_Non_IDR_Picture:
							vt.addBytes(naluLen)
							vt.IDR = false
							vt.SetTs(ts)
							vt.CompositionTime = cts
							if nonIDRs == 0 {
								vt.SetNalu0(nalu)
							} else {
								vt.NALUs = append(vt.NALUs, nalu)
							}
							nonIDRs++
						case codec.NALU_SEI:
						case codec.NALU_Filler_Data:
						default:
							utils.Printf("%s,nalType not support yet:%d,[0]=0x%X", vt.Stream.StreamPath, naluType, nalu[0])
						}
					}
					if nonIDRs > 0 {
						vt.push()
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
					vt.SPSInfo, _ = codec.ParseHevcSPS(nalu)
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
				vt.PushNalu = func(ts uint32, cts uint32, nalus ...[]byte) {
					var nonIDRs [][]byte
					for _, nalu := range nalus {
						naluLen := len(nalu)
						if naluLen == 0 {
							continue
						}
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
						switch naluType {
						case codec.NAL_UNIT_VPS:
							vps = nalu
							vt.ExtraData.NALUs[0] = vps
						case codec.NAL_UNIT_SPS:
							sps = nalu
							vt.ExtraData.NALUs[1] = sps
							vt.SPSInfo, _ = codec.ParseHevcSPS(nalu)
						case codec.NAL_UNIT_PPS:
							pps = nalu
							vt.ExtraData.NALUs[2] = pps
							extraData, err := codec.BuildH265SeqHeaderFromVpsSpsPps(vps, sps, pps)
							if err != nil {
								return
							}
							vt.ExtraData.Payload = extraData
						case codec.NAL_UNIT_CODED_SLICE_BLA,
							codec.NAL_UNIT_CODED_SLICE_BLANT,
							codec.NAL_UNIT_CODED_SLICE_BLA_N_LP,
							codec.NAL_UNIT_CODED_SLICE_IDR,
							codec.NAL_UNIT_CODED_SLICE_IDR_N_LP,
							codec.NAL_UNIT_CODED_SLICE_CRA:
							vt.IDR = true
							vt.SetTs(ts)
							vt.CompositionTime = cts
							vt.SetNalu0(nalu)
							vt.addBytes(naluLen)
							vt.push()
						case 0, 1, 2, 3, 4, 5, 6, 7, 9:
							nonIDRs = append(nonIDRs, nalu)
							vt.addBytes(naluLen)
						}
					}
					if len(nonIDRs) > 0 {
						vt.IDR = false
						vt.SetTs(ts)
						vt.CompositionTime = cts
						vt.NALUs = nonIDRs
						vt.push()
					}
				}
			}
		}
	}
	vt.PushNalu(ts, cts, nalus...)
}

func (vt *VideoTrack) setCurrent() {
	vt.AVTrack.setCurrent()
	vt.VideoPack = vt.Value.(*VideoPack)
}

func (vt *VideoTrack) PushByteStream(ts uint32, payload []byte) {
	if payload[1] == 0 {
		vt.CodecID = payload[0] & 0x0F
		switch vt.CodecID {
		case 7:
			var info codec.AVCDecoderConfigurationRecord
			if _, err := info.Unmarshal(payload[5:]); err == nil {
				vt.SPSInfo, _ = codec.ParseSPS(info.SequenceParameterSetNALUnit)
				vt.nalulenSize = int(info.LengthSizeMinusOne&3 + 1)
				vt.ExtraData = &VideoPack{
					NALUs: [][]byte{info.SequenceParameterSetNALUnit, info.PictureParameterSetNALUnit},
				}
				vt.ExtraData.Payload = payload
				vt.Stream.VideoTracks.AddTrack("h264", vt)
			}
		case 12:
			if vps, sps, pps, err := codec.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(payload); err == nil {
				vt.SPSInfo, _ = codec.ParseSPS(sps)
				vt.nalulenSize = int(payload[26]) & 0x03
				vt.ExtraData = &VideoPack{
					NALUs: [][]byte{vps, sps, pps},
				}
				vt.ExtraData.Payload = payload
				vt.Stream.VideoTracks.AddTrack("h265", vt)
			}
		}
	} else {
		if len(payload) < 4 {
			return
		}
		vt.addBytes(len(payload))
		vt.IDR = payload[0]>>4 == 1
		vt.SetTs(ts)
		vt.Payload = payload
		vt.CompositionTime = utils.BigEndian.Uint24(payload[2:])
		vt.ResetNALUs()
		for nalus := payload[5:]; len(nalus) > vt.nalulenSize; {
			nalulen := 0
			for i := 0; i < vt.nalulenSize; i++ {
				nalulen += int(nalus[i]) << (8 * (vt.nalulenSize - i - 1))
			}
			vt.NALUs = append(vt.NALUs, nalus[vt.nalulenSize:nalulen+vt.nalulenSize])
			nalus = nalus[nalulen+vt.nalulenSize:]
		}
		vt.push()
	}
}

func (vt *VideoTrack) push() {
	if vt.Stream != nil {
		vt.Stream.Update()
	}
	if vt.writeByteStream != nil {
		vt.writeByteStream()
	}
	if vt.GetBPS(); vt.IDR {
		vt.revIDR()
	}
	if nextPack := vt.NextValue().(*VideoPack); nextPack.IDR {
		if vt.idrCount == 1 {
			exRing := ring.New(5)
			for x := exRing; x.Value == nil; x = x.Next() {
				x.Value = &AVItem{Value: new(VideoPack)}
			}
			vt.Link(exRing) // 扩大缓冲环
		} else {
			vt.idrCount--
		}
	}
	vt.Step()
	vt.setCurrent()
}

func (vt *VideoTrack) Play(onVideo func(uint32, *VideoPack), exit1, exit2 <-chan struct{}) {
	select {
	case <-vt.WaitIDR:
	case <-exit1:
		return
	case <-exit2: //可能等不到关键帧就退出了
		return
	}
	vr := vt.SubRing(vt.IDRing) //从关键帧开始读取，首屏秒开
	item, vp := vr.Read()
	for startTimestamp := item.Timestamp; ; item, vp = vr.Read() {
		select {
		case <-exit1:
			return
		case <-exit2:
			return
		default:
			onVideo(item.Timestamp-startTimestamp, vp.(*VideoPack))
			vr.MoveNext()
		}
	}
}
