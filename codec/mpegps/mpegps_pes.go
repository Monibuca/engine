package mpegps

import "io"

func (es *MpegPsEsStream) parsePESPacket(payload []byte) (err error) {
	if len(payload) < 4 {
		return io.ErrShortBuffer
	}
	//data_alignment_indicator := (payload[0]&0b0001_0000)>>4 == 1
	flag := payload[1]
	ptsFlag := flag>>7 == 1
	dtsFlag := (flag&0b0100_0000)>>6 == 1
	pesHeaderDataLen := payload[2]
	if len(payload) < int(pesHeaderDataLen) {
		return io.ErrShortBuffer
	}
	payload = payload[3:]
	extraData := payload[:pesHeaderDataLen]
	if ptsFlag && len(extraData) > 4 {
		es.PTS = uint32(extraData[0]&0b0000_1110) << 29
		es.PTS += uint32(extraData[1]) << 22
		es.PTS += uint32(extraData[2]&0b1111_1110) << 14
		es.PTS += uint32(extraData[3]) << 7
		es.PTS += uint32(extraData[4]) >> 1
		if dtsFlag && len(extraData) > 9 {
			es.DTS = uint32(extraData[5]&0b0000_1110) << 29
			es.DTS += uint32(extraData[6]) << 22
			es.DTS += uint32(extraData[7]&0b1111_1110) << 14
			es.DTS += uint32(extraData[8]) << 7
			es.DTS += uint32(extraData[9]) >> 1
		}
	}
	es.Write(payload[pesHeaderDataLen:])
	return
}
