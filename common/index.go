package common

import "time"

type Track interface {
	GetName() string
}

type AVTrack interface {
	Track
	WriteAVCC(ts uint32, frame AVCCFrame) //写入AVCC格式的数据
	Flush()
}

type BPS struct {
	ts    time.Time
	bytes int
	BPS   int
}

func (bps *BPS) ComputeBPS(bytes int) {
	bps.bytes += bytes
	if elapse := time.Since(bps.ts).Seconds(); elapse > 1 {
		bps.BPS = bps.bytes / int(elapse)
		bps.ts = time.Now()
	}
}

type HZ uint32

func (hz HZ) ToMini(nts uint32) uint32 {
	return nts / (uint32(hz) / 1000)
}

func (hz HZ) ToNTS(mini uint32) uint32 {
	return mini * (uint32(hz) / 1000)
}
