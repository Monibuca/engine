package mpegps

import (
	"errors"

	"m7s.live/engine/v4/util"
)

var (
	ErrNotFoundStartCode = errors.New("not found the need start code flag")
	ErrMarkerBit         = errors.New("marker bit value error")
	ErrFormatPack        = errors.New("not package standard")
	ErrParsePakcet       = errors.New("parse ps packet error")
)

/*
 This implement from VLC source code
 notes: https://github.com/videolan/vlc/blob/master/modules/mux/mpeg/bits.h
*/

/*
https://github.com/videolan/vlc/blob/master/modules/demux/mpeg
*/
type DecPSPackage struct {
	systemClockReferenceBase      uint64
	systemClockReferenceExtension uint64
	programMuxRate                uint32
	IOBuffer
	Payload []byte
	PTS     uint32
	DTS     uint32
	EsHandler
	audio MpegPsEsStream
	video MpegPsEsStream
}

func (dec *DecPSPackage) clean() {
	dec.systemClockReferenceBase = 0
	dec.systemClockReferenceExtension = 0
	dec.programMuxRate = 0
	dec.Payload = nil
	dec.PTS = 0
	dec.DTS = 0
}

func (dec *DecPSPackage) ReadPayload() (payload []byte, err error) {
	payloadlen, err := dec.Uint16()
	if err != nil {
		return
	}
	return dec.ReadN(int(payloadlen))
}
func (dec *DecPSPackage) Feed(ps []byte) {
	if len(ps) >= 4 && util.BigEndian.Uint32(ps) == StartCodePS {
		if dec.Len() > 0 {
			dec.Skip(4)
			dec.Read(0)
			dec.Reset()
		}
		dec.Write(ps)
	} else if dec.Len() > 0 {
		dec.Write(ps)
	}
}

// read the buffer and push video or audio
func (dec *DecPSPackage) Read(ts uint32) error {
again:
	dec.clean()
	if err := dec.Skip(9); err != nil {
		return err
	}

	psl, err := dec.ReadByte()
	if err != nil {
		return err
	}
	psl &= 0x07
	if err = dec.Skip(int(psl)); err != nil {
		return err
	}
	var video []byte
	var nextStartCode, videoTs, videoCts uint32
loop:
	for err == nil {
		if nextStartCode, err = dec.Uint32(); err != nil {
			break
		}
		switch nextStartCode {
		case StartCodeSYS:
			dec.ReadPayload()
			//err = dec.decSystemHeader()
		case StartCodeMAP:
			err = dec.decProgramStreamMap()
		case StartCodeVideo:
			// var cts uint32
			if err = dec.decPESPacket(); err == nil {
				if len(video) == 0 {
					dec.video.PTS = dec.PTS
					dec.video.DTS = dec.DTS
					// if dec.PTS == 0 {
					// 	dec.PTS = ts
					// }
					// if dec.DTS != 0 {
					// 	cts = dec.PTS - dec.DTS
					// } else {
					// 	dec.DTS = dec.PTS
					// }
					// videoTs = dec.DTS / 90
					// videoCts = cts / 90
				}
				video = append(video, dec.Payload...)
			} else {
				// utils.Println("video", err)
			}
		case StartCodeAudio:
			if err = dec.decPESPacket(); err == nil {
				// ts := ts / 90
				// if dec.PTS != 0 {
				// 	ts = dec.PTS / 90
				// }
				dec.audio.PTS = dec.PTS
				dec.audio.Buffer = dec.Payload
				dec.ReceiveAudio(dec.audio)
				// pusher.PushAudio(ts, dec.Payload)
			} else {
				// utils.Println("audio", err)
			}
		case StartCodePS:
			break loop
		default:
			dec.ReadPayload()
		}
	}
	if len(video) > 0 {
		dec.video.Buffer = video
		dec.ReceiveVideo(dec.video)
		if false {
			println("video", videoTs, videoCts, len(video))
		}
		// pusher.PushVideo(videoTs, videoCts, video)
	}
	if nextStartCode == StartCodePS {
		// utils.Println(aurora.Red("StartCodePS recursion..."), err)
		goto again
	}
	return err
}

/*
	func (dec *DecPSPackage) decSystemHeader() error {
		syslens, err := dec.Uint16()
		if err != nil {
			return err
		}
		// drop rate video audio bound and lock flag
		syslens -= 6
		if err = dec.Skip(6); err != nil {
			return err
		}

		// ONE WAY: do not to parse the stream  and skip the buffer
		//br.Skip(syslen * 8)

		// TWO WAY: parse every stream info
		for syslens > 0 {
			if nextbits, err := dec.Uint8(); err != nil {
				return err
			} else if (nextbits&0x80)>>7 != 1 {
				break
			}
			if err = dec.Skip(2); err != nil {
				return err
			}
			syslens -= 3
		}
		return nil
	}
*/
func (dec *DecPSPackage) decProgramStreamMap() error {
	psm, err := dec.ReadPayload()
	if err != nil {
		return err
	}
	l := len(psm)
	index := 2
	programStreamInfoLen := util.BigEndian.Uint16(psm[index:])
	index += 2
	index += int(programStreamInfoLen)
	programStreamMapLen := util.BigEndian.Uint16(psm[index:])
	index += 2
	for programStreamMapLen > 0 {
		if l <= index+1 {
			break
		}
		streamType := psm[index]
		index++
		elementaryStreamID := psm[index]
		index++
		if elementaryStreamID >= 0xe0 && elementaryStreamID <= 0xef {
			dec.video.Type = streamType
		} else if elementaryStreamID >= 0xc0 && elementaryStreamID <= 0xdf {
			dec.audio.Type = streamType
		}
		if l <= index+1 {
			break
		}
		elementaryStreamInfoLength := util.BigEndian.Uint16(psm[index:])
		index += 2
		index += int(elementaryStreamInfoLength)
		programStreamMapLen -= 4 + elementaryStreamInfoLength
	}
	return nil
}

func (dec *DecPSPackage) decPESPacket() error {
	payload, err := dec.ReadPayload()
	if err != nil {
		return err
	}
	if len(payload) < 4 {
		return errors.New("not enough data")
	}
	//data_alignment_indicator := (payload[0]&0b0001_0000)>>4 == 1
	flag := payload[1]
	ptsFlag := flag>>7 == 1
	dtsFlag := (flag&0b0100_0000)>>6 == 1
	var pts, dts uint32
	pesHeaderDataLen := payload[2]
	payload = payload[3:]
	extraData := payload[:pesHeaderDataLen]
	if ptsFlag && len(extraData) > 4 {
		pts = uint32(extraData[0]&0b0000_1110) << 29
		pts += uint32(extraData[1]) << 22
		pts += uint32(extraData[2]&0b1111_1110) << 14
		pts += uint32(extraData[3]) << 7
		pts += uint32(extraData[4]) >> 1
		if dtsFlag && len(extraData) > 9 {
			dts = uint32(extraData[5]&0b0000_1110) << 29
			dts += uint32(extraData[6]) << 22
			dts += uint32(extraData[7]&0b1111_1110) << 14
			dts += uint32(extraData[8]) << 7
			dts += uint32(extraData[9]) >> 1
		}
	}
	dec.PTS = pts
	dec.DTS = dts
	dec.Payload = payload[pesHeaderDataLen:]
	return err
}
