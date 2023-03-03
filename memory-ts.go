package engine

import (
	"errors"
	"fmt"
	"io"
	"net"

	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/codec/mpegts"
	"m7s.live/engine/v4/util"
)

type MemoryTs struct {
	util.BytesPool
	PMT util.Buffer
	util.BLL
}

func (ts *MemoryTs) WritePMTPacket(audio codec.AudioCodecID, video codec.VideoCodecID) {
	ts.PMT.Reset()
	mpegts.WritePMTPacket(&ts.PMT, video, audio)
}

func (ts *MemoryTs) WriteTo(w io.Writer) (int64, error) {
	w.Write(mpegts.DefaultPATPacket)
	w.Write(ts.PMT)
	return ts.BLL.WriteTo(w)
}

func (ts *MemoryTs) WritePESPacket(frame *mpegts.MpegtsPESFrame, packet mpegts.MpegTsPESPacket) (err error) {
	if packet.Header.PacketStartCodePrefix != 0x000001 {
		err = errors.New("packetStartCodePrefix != 0x000001")
		return
	}
	pesHeadItem := ts.Get(32)
	pesHeadItem.Value.Reset()
	_, err = mpegts.WritePESHeader(&pesHeadItem.Value, packet.Header)
	if err != nil {
		return
	}
	pesBuffers := append(net.Buffers{pesHeadItem.Value}, packet.Buffers...)
	defer pesHeadItem.Recycle()
	pesPktLength := util.SizeOfBuffers(pesBuffers)
	buffer := ts.Get((pesPktLength/mpegts.TS_PACKET_SIZE+1)*6 + pesPktLength)
	bwTsHeader := &buffer.Value
	bigLen := bwTsHeader.Len()
	bwTsHeader.Reset()
	ts.BLL.Push(buffer)
	var tsHeaderLength int
	for i := 0; len(pesBuffers) > 0; i++ {
		if bigLen < mpegts.TS_PACKET_SIZE {
			headerItem := ts.Get(mpegts.TS_PACKET_SIZE)
			ts.BLL.Push(headerItem)
			bwTsHeader = &headerItem.Value
			bwTsHeader.Reset()
		}
		bigLen -= mpegts.TS_PACKET_SIZE
		pesPktLength = util.SizeOfBuffers(pesBuffers)
		tsHeader := mpegts.MpegTsHeader{
			SyncByte:                   0x47,
			TransportErrorIndicator:    0,
			PayloadUnitStartIndicator:  0,
			TransportPriority:          0,
			Pid:                        frame.Pid,
			TransportScramblingControl: 0,
			AdaptionFieldControl:       1,
			ContinuityCounter:          frame.ContinuityCounter,
		}

		frame.ContinuityCounter++
		frame.ContinuityCounter = frame.ContinuityCounter % 16

		// 每一帧的开头,当含有pcr的时候,包含调整字段
		if i == 0 {
			tsHeader.PayloadUnitStartIndicator = 1

			// 当PCRFlag为1的时候,包含调整字段
			if frame.IsKeyFrame {
				tsHeader.AdaptionFieldControl = 0x03
				tsHeader.AdaptationFieldLength = 7
				tsHeader.PCRFlag = 1
				tsHeader.RandomAccessIndicator = 1
				tsHeader.ProgramClockReferenceBase = frame.ProgramClockReferenceBase
			}
		}

		// 每一帧的结尾,当不满足188个字节的时候,包含调整字段
		if pesPktLength < mpegts.TS_PACKET_SIZE-4 {
			var tsStuffingLength uint8

			tsHeader.AdaptionFieldControl = 0x03
			tsHeader.AdaptationFieldLength = uint8(mpegts.TS_PACKET_SIZE - 4 - 1 - pesPktLength)

			// TODO:如果第一个TS包也是最后一个TS包,是不是需要考虑这个情况?
			// MpegTsHeader最少占6个字节.(前4个走字节 + AdaptationFieldLength(1 byte) + 3个指示符5个标志位(1 byte))
			if tsHeader.AdaptationFieldLength >= 1 {
				tsStuffingLength = tsHeader.AdaptationFieldLength - 1
			} else {
				tsStuffingLength = 0
			}
			// error
			tsHeaderLength, err = mpegts.WriteTsHeader(bwTsHeader, tsHeader)
			if err != nil {
				return
			}
			if tsStuffingLength > 0 {
				if _, err = bwTsHeader.Write(mpegts.Stuffing[:tsStuffingLength]); err != nil {
					return
				}
			}
			tsHeaderLength += int(tsStuffingLength)
		} else {

			tsHeaderLength, err = mpegts.WriteTsHeader(bwTsHeader, tsHeader)
			if err != nil {
				return
			}
		}

		tsPayloadLength := mpegts.TS_PACKET_SIZE - tsHeaderLength

		//fmt.Println("tsPayloadLength :", tsPayloadLength)

		// 这里不断的减少PES包
		io.CopyN(bwTsHeader, &pesBuffers, int64(tsPayloadLength))
		// tmp := tsHeaderByte[3] << 2
		// tmp = tmp >> 6
		// if tmp == 2 {
		// 	fmt.Println("fuck you mother.")
		// }
		tsPktByteLen := bwTsHeader.Len()

		if tsPktByteLen != (i+1)*mpegts.TS_PACKET_SIZE && tsPktByteLen != mpegts.TS_PACKET_SIZE {
			err = errors.New(fmt.Sprintf("%s, packet size=%d", "TS_PACKET_SIZE != 188,", tsPktByteLen))
			return
		}
	}

	return nil
}

func (ts *MemoryTs) WriteAudioFrame(frame AudioFrame, pes *mpegts.MpegtsPESFrame) (err error) {
	// packetLength = 原始音频流长度 + adts(7) + MpegTsOptionalPESHeader长度(8 bytes, 因为只含有pts)
	var packet mpegts.MpegTsPESPacket
	if frame.CodecID == codec.CodecID_AAC {
		packet.Header.PesPacketLength = uint16(7 + frame.AUList.ByteLength + 8)
		packet.Buffers = frame.GetADTS()
	} else {
		packet.Header.PesPacketLength = uint16(frame.AUList.ByteLength + 8)
		packet.Buffers = frame.AUList.ToBuffers()
	}
	packet.Header.PacketStartCodePrefix = 0x000001
	packet.Header.ConstTen = 0x80
	packet.Header.StreamID = mpegts.STREAM_ID_AUDIO
	packet.Header.Pts = uint64(frame.PTS)
	pes.ProgramClockReferenceBase = packet.Header.Pts
	packet.Header.PtsDtsFlags = 0x80
	packet.Header.PesHeaderDataLength = 5
	return ts.WritePESPacket(pes, packet)
}

func (ts *MemoryTs) WriteVideoFrame(frame VideoFrame, pes *mpegts.MpegtsPESFrame) (err error) {
	var buffer net.Buffers
	//需要对原始数据(ES),进行一些预处理,视频需要分割nalu(H264编码),并且打上sps,pps,nalu_aud信息.
	if len(frame.ParamaterSets) == 2 {
		buffer = append(buffer, codec.NALU_AUD_BYTE)
	} else {
		buffer = append(buffer, codec.AudNalu)
	}
	buffer = append(buffer, frame.GetAnnexB()...)
	pktLength := util.SizeOfBuffers(buffer) + 10 + 3
	if pktLength > 0xffff {
		pktLength = 0
	}

	var packet mpegts.MpegTsPESPacket
	packet.Header.PacketStartCodePrefix = 0x000001
	packet.Header.ConstTen = 0x80
	packet.Header.StreamID = mpegts.STREAM_ID_VIDEO
	packet.Header.PesPacketLength = uint16(pktLength)
	packet.Header.Pts = uint64(frame.PTS)
	pes.ProgramClockReferenceBase = packet.Header.Pts
	packet.Header.Dts = uint64(frame.DTS)
	packet.Header.PtsDtsFlags = 0xC0
	packet.Header.PesHeaderDataLength = 10
	packet.Buffers = buffer
	return ts.WritePESPacket(pes, packet)
}
