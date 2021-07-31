package engine

import (
	"container/ring"
	"context"
	"time"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
)

const (
	naluTypeBitmask      = 0b0001_1111
	naluTypeBitmask_hevc = 0x7E
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
	PushNalu        func(ts uint32, cts uint32, nalus ...[]byte) `json:"-"`
	UsingDonlField  bool
	writeByteStream func(pack *VideoPack)
	idrCount        int //处于缓冲中的关键帧数量
	nalulenSize     int
}

func (s *Stream) NewVideoTrack(codec byte) (vt *VideoTrack) {
	var cancel context.CancelFunc
	vt = &VideoTrack{
		revIDR: func() {
			vt.IDRing = vt.Ring
			cancel()
			current := vt.current()
			idrSequence := current.Sequence
			vt.ts = current.Timestamp
			vt.idrCount++
			vt.revIDR = func() {
				vt.idrCount++
				current = vt.current()
				vt.GOP = current.Sequence - idrSequence
				if l := vt.Ring.Len() - vt.GOP - 5; l > 5 {
					//缩小缓冲环节省内存
					vt.Unlink(l).Do(func(v interface{}) {
						if v.(*AVItem).Value.(*VideoPack).IDR {
							vt.idrCount--
						}
					})
				}
				vt.IDRing = vt.Ring
				idrSequence = current.Sequence
				vt.ts = current.Timestamp
				vt.bytes = 0
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
								vt.push(vt.current())
								nonIDRs = 0
							}
							vt.bytes += naluLen
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
							vt.bytes += naluLen
							pack := vt.current()
							pack.IDR = false
							pack.Timestamp = ts
							pack.CompositionTime = cts
							if cap(pack.NALUs) > 0 {
								if nonIDRs == 0 {
									pack.NALUs = pack.NALUs[:1]
									pack.NALUs[0] = nalu
								} else {
									pack.NALUs = append(pack.NALUs, nalu)
								}
							} else {
								pack.NALUs = [][]byte{nalu}
							}
							nonIDRs++
						case codec.NALU_SEI:
						case codec.NALU_Filler_Data:
						default:
							utils.Printf("%s,nalType not support yet:%d,[0]=0x%X", vt.Stream.StreamPath, naluType, nalu[0])
						}
					}
					if nonIDRs > 0 {
						vt.push(vt.current())
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
							vt.bytes += naluLen
							vt.push(pack)
						case 0, 1, 2, 3, 4, 5, 6, 7, 9:
							nonIDRs = append(nonIDRs, nalu)
							vt.bytes += naluLen
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
		for nalus := payload[5:]; len(nalus) > vt.nalulenSize; {
			nalulen := 0
			for i := 0; i < vt.nalulenSize; i++ {
				nalulen += int(nalus[i]) << (8 * (vt.nalulenSize - i - 1))
			}
			pack.NALUs = append(pack.NALUs, nalus[vt.nalulenSize:nalulen+vt.nalulenSize])
			nalus = nalus[nalulen+vt.nalulenSize:]
		}
		vt.push(pack)
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
}

func (vt *VideoTrack) Play(onVideo func(VideoPack), exit1, exit2 <-chan struct{}) {
	select {
	case <-vt.WaitIDR.Done():
	case <-exit1:
		return
	case <-exit2: //可能等不到关键帧就退出了
		return
	}
	vr := vt.SubRing(vt.IDRing) //从关键帧开始读取，首屏秒开
	vp := vr.Read().(*VideoPack)
	for startTimestamp := vp.Timestamp; ; vp = vr.Read().(*VideoPack) {
		select {
		case <-exit1:
			return
		case <-exit2:
			return
		default:
			onVideo(vp.Copy(startTimestamp))
			vr.MoveNext()
		}
	}
}
