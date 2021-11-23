package engine

import (
	"time"

	"github.com/Monibuca/utils/v3"
	"github.com/pion/rtp"
)

// 对rtp包进行解封装，并修复时间戳，包括时间戳跳跃
type RTPDemuxer struct {
	rtp.Packet
	Reorder   bool           // 是否支持乱序重排
	PTS       uint32         // 修复后的时间戳（毫秒）
	lastTs    uint32         // 记录上一个收到的时间戳
	lastSeq   uint16         // 记录上一个收到的序列号
	lastSeq2  uint16         // 记录上上一个收到的序列号
	timeBase  *time.Duration // 采样率
	timestamp time.Time      // 客观时间用于计算耗时
	OnDemux   func(uint32, []byte)
}

func (r *RTPDemuxer) Push(rtpRaw []byte) {
	if err := r.Unmarshal(rtpRaw); err != nil {
		utils.Println("RTP Unmarshal error", err)
		return
	}
	// 本次序号是上上次的序号+1 说明中间隔了一个错误序号（某些rtsp流中的rtcp包写成了rtp包导致的）
	if r.SequenceNumber == r.lastSeq2+1 {
		r.lastSeq = r.SequenceNumber
	} else {
		r.lastSeq2 = r.lastSeq
		r.lastSeq = r.SequenceNumber
		if r.lastSeq != r.lastSeq2+1 { //序号不连续
			utils.Println("RTP SequenceNumber error", r.lastSeq2, r.lastSeq)
			return
		}
	}
	if r.Timestamp > r.lastTs {
		delta := uint32(uint64(r.Timestamp-r.lastTs) * 1000 / uint64(*r.timeBase))
		if delta > 1000 { // 时间戳跳跃
			r.PTS += uint32(time.Since(r.timestamp) / time.Millisecond)
		} else {
			r.PTS += delta
		}
	} else if r.lastTs > r.Timestamp {
		delta := uint32(uint64(r.lastTs-r.Timestamp) * 1000 / uint64(*r.timeBase))
		if delta > 1000 { // 时间戳跳跃
			r.PTS += uint32(time.Since(r.timestamp) / time.Millisecond)
		} else {
			r.PTS -= delta
		}
	}
	r.timestamp = time.Now()
	r.OnDemux(r.PTS, r.Payload)
	r.lastTs = r.Timestamp
}
