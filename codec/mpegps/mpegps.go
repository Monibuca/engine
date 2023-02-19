package mpegps

import (
	"encoding/binary"
	"errors"
	"io"

	"m7s.live/engine/v4/util"
)

const (
	StartCodePS        = 0x000001ba
	StartCodeSYS       = 0x000001bb
	StartCodeMAP       = 0x000001bc
	StartCodeVideo     = 0x000001e0
	StartCodeAudio     = 0x000001c0
	PrivateStreamCode  = 0x000001bd
	MEPGProgramEndCode = 0x000001b9
)

type EsHandler interface {
	ReceiveAudio(MpegPsEsStream)
	ReceiveVideo(MpegPsEsStream)
}

type MpegPsEsStream struct {
	Type byte
	util.Buffer
	PTS uint32
	DTS uint32
}

type MpegPsStream struct {
	buffer util.Buffer
	EsHandler
	audio MpegPsEsStream
	video MpegPsEsStream
}

func (ps *MpegPsStream) Drop() {
	ps.buffer.Reset()
	ps.audio.Reset()
	ps.video.Reset()
}

func (ps *MpegPsStream) Feed(data util.Buffer) (err error) {
	reader := &data
	if ps.buffer.CanRead() {
		ps.buffer.Write(data)
		reader = &ps.buffer
	}
	var begin util.Buffer
	var payload []byte
	defer func() {
		if err != nil && begin.CanRead() {
			ps.buffer.Reset()
			ps.buffer.Write(begin)
		}
	}()
	for err == nil && reader.CanReadN(4) {
		begin = *reader
		code := reader.ReadUint32()
		switch code {
		case StartCodePS:
			if ps.audio.Buffer.CanRead() {
				ps.ReceiveAudio(ps.audio)
				ps.audio.Buffer = make(util.Buffer, 0)
			}
			if ps.video.Buffer.CanRead() {
				ps.ReceiveVideo(ps.video)
				ps.video.Buffer = make(util.Buffer, 0)
			}
			if reader.CanReadN(9) {
				reader.ReadN(9)
				if reader.CanRead() {
					psl := reader.ReadByte() & 0x07
					if reader.CanReadN(int(psl)) {
						reader.ReadN(int(psl))
						continue
					}
				}
			}
			err = io.ErrShortBuffer
		case StartCodeSYS, PrivateStreamCode:
			_, err = ps.ReadPayload(reader)
		case StartCodeMAP:
			err = ps.decProgramStreamMap(reader)
		case StartCodeVideo:
			payload, err = ps.ReadPayload(reader)
			if err == nil {
				err = ps.video.parsePESPacket(payload)
			}
		case StartCodeAudio:
			payload, err = ps.ReadPayload(reader)
			if err == nil {
				err = ps.audio.parsePESPacket(payload)
			}
		case MEPGProgramEndCode:
			return
		default:
			err = errors.New("start code error")
		}
	}
	return
}

func (ps *MpegPsStream) ReadPayload(data *util.Buffer) (payload []byte, err error) {
	if !data.CanReadN(2) {
		return nil, io.ErrShortBuffer
	}
	payloadlen := data.ReadUint16()
	if data.CanReadN(int(payloadlen)) {
		payload = data.ReadN(int(payloadlen))
	} else {
		err = io.ErrShortBuffer
	}
	return
}

func (ps *MpegPsStream) decProgramStreamMap(data *util.Buffer) error {
	psm, err := ps.ReadPayload(data)
	if err != nil {
		return err
	}
	l := len(psm)
	index := 2
	programStreamInfoLen := binary.BigEndian.Uint16(psm[index:])
	index += 2
	index += int(programStreamInfoLen)
	programStreamMapLen := binary.BigEndian.Uint16(psm[index:])
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
			ps.video.Type = streamType
		} else if elementaryStreamID >= 0xc0 && elementaryStreamID <= 0xdf {
			ps.audio.Type = streamType
		}
		if l <= index+1 {
			break
		}
		elementaryStreamInfoLength := binary.BigEndian.Uint16(psm[index:])
		index += 2
		index += int(elementaryStreamInfoLength)
		programStreamMapLen -= 4 + elementaryStreamInfoLength
	}
	return nil
}
