package engine

import (
	"container/ring"
	"github.com/Monibuca/engine/v3/util"
	"time"

	"github.com/Monibuca/utils/v3"
	"github.com/Monibuca/utils/v3/codec"
)

const (
	naluTypeBitmask      = 0b0001_1111
	naluTypeBitmask_hevc = 0x7E
	defaultRingSize      = 75 //the maximum GOP size by default
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
	SPSInfo        codec.SPSInfo
	GOP            int           //关键帧间隔
	ExtraData      *VideoPack    `json:"-"` //H264(SPS、PPS) H265(VPS、SPS、PPS)
	WaitIDR        chan struct{} `json:"-"`
	revIDR         func()
	PushNalu       func(ts uint32, cts uint32, nalus ...[]byte) `json:"-"`
	UsingDonlField bool
	idrCount       int //处于缓冲中的关键帧数量
	nalulenSize    int
	*VideoPack     `json:"-"` //当前写入的视频数据
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
				if l := vt.Size - vt.GOP - 5; l > 5 {
					vt.Size -= l
					//缩小缓冲环节省内存
					vt.AVRing.Unlink(l).Do(func(v interface{}) {
						if v.(*AVItem).Value.(*VideoPack).IDR {
							// 将关键帧的缓存放入对象池
							vt.idrCount--
						}
					})
				}
				vt.IDRing = vt.AVRing.Ring
				idrSequence = vt.Sequence
				vt.resetBPS()
			}
		},
	}
	vt.timebase = 90000
	vt.PushNalu = vt.pushNalu
	vt.Stream = s
	vt.CodecID = codec
	vt.AVRing.Init(s.Context, defaultRingSize)
	vt.poll = time.Millisecond * 20
	vt.AVRing.Do(func(v interface{}) {
		v.(*AVItem).Value = new(VideoPack)
	})
	vt.setCurrent()
	return
}

func (vt *VideoTrack) PushAnnexB(ts uint32, cts uint32, payload []byte) {
	vt.PushNalu(ts, cts, codec.SplitH264(payload)...)
}
func (vt *VideoTrack) writeByteStream() {
	totalLen := 0
	for _, s := range vt.NALUs {
		totalLen += len(s)
	}
	tmp := make([]byte, 5+totalLen+4*len(vt.NALUs))
	if vt.IDR {
		tmp[0] = 0x10 | vt.CodecID
	} else {
		tmp[0] = 0x20 | vt.CodecID
	}
	tmp[1] = 1
	utils.BigEndian.PutUint24(tmp[2:], vt.CompositionTime)
	i := 5
	for _, nalu := range vt.NALUs {
		utils.BigEndian.PutUint32(tmp[i:], uint32(len(nalu)))
		i += 4
		i += copy(tmp[i:], nalu)
	}
	vt.VideoPack.Payload = tmp
}

func (vt *VideoTrack) pushNalu(ts uint32, cts uint32, nalus ...[]byte) {

	// 缓冲中只包含Nalu数据所以写入rtmp格式时需要按照ByteStream格式写入

	switch vt.CodecID {
	case codec.CodecID_H264:
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
				vt.Stream.VideoTracks.AddTrack("h264", vt)
				//已完成SPS和PPS 组装，重置push函数，接收视频数据
				vt.PushNalu = func(ts uint32, cts uint32, nalus ...[]byte) {
					var nonIDRs int
					var IDRs int
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
							vt.addBytes(naluLen)
							if IDRs == 0 {
								vt.setIDR(true)
								vt.SetNalu0(nalu)
							} else {
								vt.NALUs = append(vt.NALUs, nalu)
							}
							IDRs++
						case codec.NALU_Non_IDR_Picture:
							vt.addBytes(naluLen)
							if nonIDRs == 0 {
								vt.setIDR(false)
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
					if nonIDRs+IDRs > 0 {
						vt.setTS(ts)
						vt.CompositionTime = cts
						vt.push()
					}
				}
			}
		}
	case codec.CodecID_H265:
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
				vt.Stream.VideoTracks.AddTrack("h265", vt)
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
							vt.setIDR(true)
							vt.setTS(ts)
							vt.CompositionTime = cts
							vt.SetNalu0(nalu)
							vt.addBytes(naluLen)
							vt.push() //always push a new VideoPack for IDR frame
						case 0, 1, 2, 3, 4, 5, 6, 7, 9:
							nonIDRs = append(nonIDRs, nalu)
							vt.addBytes(naluLen)
						}
					}
					if len(nonIDRs) > 0 {
						vt.setIDR(false)
						vt.setTS(ts)
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
		vt.setTS(ts)
		vt.VideoPack.Payload = payload
		vt.CompositionTime = utils.BigEndian.Uint24(payload[2:])
		vt.ResetNALUs()
		for nalus := payload[5:]; len(nalus) > vt.nalulenSize; {
			nalulen := 0
			for i := 0; i < vt.nalulenSize; i++ {
				nalulen += int(nalus[i]) << ((vt.nalulenSize - i - 1) << 3)
			}
			if end := nalulen + vt.nalulenSize; len(nalus) >= end {
				vt.NALUs = append(vt.NALUs, nalus[vt.nalulenSize:end])
				nalus = nalus[end:]
			} else {
				utils.Printf("PushByteStream error,len %d,nalulenSize:%d,end:%d", len(nalus), vt.nalulenSize, end)
				break
			}
		}
		if len(vt.NALUs) > 0 {
			vt.push()
		}
	}
}

// 设置关键帧信息，主要是为了判断缓存之前是否是关键帧，用来调度缓存
func (vt *VideoTrack) setIDR(idr bool) {
	// 如果当前帧的类型和需要设置的类型相同，则不需要操作
	if idr == vt.VideoPack.IDR {
		return
	}
	vt.VideoPack.IDR = idr
}

func (vt *VideoTrack) push() {
	if len(vt.NALUs) == 0 {
		panic("push error,nalus is empty")
	}
	if vt.Stream != nil {
		vt.Stream.Update()
	}
	vt.writeByteStream()
	vt.AVTrack.GetBPS()
	if vt.IDR {
		vt.revIDR()
	}
	if nextPack := vt.AVRing.NextValue().(*VideoPack); nextPack.IDR {
		if vt.idrCount == 1 {
			if min := util.Min(config.MaxRingSize, vt.GOP+5); vt.AVRing.Size < min {
				exRing := ring.New(min - vt.AVRing.Size)
				for x := exRing; x.Value == nil; x = x.Next() {
					x.Value = &AVItem{DataItem: DataItem{Value: new(VideoPack)}}
				}
				vt.AVRing.Link(exRing) // 扩大缓冲环
			}
		} else {
			vt.idrCount--
		}
	}
	vt.AVRing.Step()
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
	vr := vt.AVRing.SubRing(vt.IDRing)      //从关键帧开始读取，首屏秒开
	realSt := vt.AVRing.PreItem().Timestamp // 当前时间戳
	item, vp := vr.Read()
	startTimestamp := item.Timestamp
	for chase := true; ; item, vp = vr.Read() {
		select {
		case <-exit1:
			return
		case <-exit2:
			return
		default:
			onVideo(uint32(item.Timestamp.Sub(startTimestamp).Milliseconds()), vp.(*VideoPack))
			if chase {
				add10 := startTimestamp.Add(time.Millisecond * 10)
				if realSt.After(add10) {
					startTimestamp = add10
				} else {
					startTimestamp = realSt
					chase = false
				}
			}
			vr.MoveNext()
		}
	}
}
