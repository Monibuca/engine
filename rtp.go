package engine

import (
	"time"

	"github.com/Monibuca/utils/v3"
	"github.com/pion/rtp"
)

// 对rtp包进行解封装，并修复时间戳，包括时间戳跳跃
type RTPDemuxer struct {
	rtp.Packet
	PTS       uint32             // 修复后的时间戳（毫秒）
	lastTs    uint32             // 记录上一个收到的时间戳
	lastSeq   uint16             // 记录上一个收到的序列号
	lastSeq2  uint16             // 记录上上一个收到的序列号
	timeBase  *time.Duration     // 采样率
	timestamp time.Time          // 客观时间用于计算耗时
	orderMap  map[uint16]RTPNalu // 缓存，用于乱序重排
	OnDemux   func(uint32, []byte)
}

func (r *RTPDemuxer) tryPop(ts uint32, payload []byte) {
	for {
		r.lastSeq++
		r.push(ts, payload)
		if next, ok := r.orderMap[r.lastSeq+1]; ok {
			delete(r.orderMap, r.lastSeq+1)
			ts = next.PTS
			payload = next.Payload
		} else {
			break
		}
	}
}
func (r *RTPDemuxer) push(ts uint32, payload []byte) {
	if ts > r.lastTs {
		delta := uint32(uint64(ts-r.lastTs) * 1000 / uint64(*r.timeBase))
		if delta > 1000 { // 时间戳跳跃
			r.PTS += uint32(time.Since(r.timestamp) / time.Millisecond)
		} else {
			r.PTS += delta
		}
		
	} else if r.lastTs > ts {
		delta := uint32(uint64(r.lastTs-ts) * 1000 / uint64(*r.timeBase))
		if delta > 1000 { // 时间戳跳跃
			r.PTS += uint32(time.Since(r.timestamp) / time.Millisecond)
		} else {
			r.PTS -= delta
		}
	}
	r.timestamp = time.Now()
	r.OnDemux(r.PTS, r.Payload)
	r.lastTs = ts
}
func (r *RTPDemuxer) Push(rtpRaw []byte) {
	if err := r.Unmarshal(rtpRaw); err != nil {
		utils.Println("RTP Unmarshal error", err)
		return
	}
	if config.RTPReorder {
		if r.SequenceNumber < r.lastSeq {
			return
		} else if r.lastSeq == 0 {
			r.timestamp = time.Now()
			r.tryPop(r.Timestamp, r.Payload)
			r.lastSeq = r.SequenceNumber
		} else if r.lastSeq+1 == r.SequenceNumber {
			r.tryPop(r.Timestamp, r.Payload)
		} else if _, ok := r.orderMap[r.SequenceNumber]; !ok {
			r.orderMap[r.SequenceNumber] = RTPNalu{
				Payload: r.Payload,
				PTS:     r.Timestamp,
			}
			// 20个包都没有出现，丢弃
			if len(r.orderMap) > 20 {
				utils.Println("RTP SequenceNumber lost", r.lastSeq+1)
				r.lastSeq++
				next, ok := r.orderMap[r.lastSeq]
				for !ok {
					r.lastSeq++
					next, ok = r.orderMap[r.lastSeq]
				}
				delete(r.orderMap, r.lastSeq)
				r.tryPop(next.PTS, next.Payload)
			}
		}
		return
	} else {
		if r.lastSeq == 0 {
			r.timestamp = time.Now()
			r.lastSeq = r.SequenceNumber
		} else if r.SequenceNumber == r.lastSeq2+1 { // 本次序号是上上次的序号+1 说明中间隔了一个错误序号（某些rtsp流中的rtcp包写成了rtp包导致的）
			r.lastSeq = r.SequenceNumber
		} else {
			r.lastSeq2 = r.lastSeq
			r.lastSeq = r.SequenceNumber
			if r.lastSeq != r.lastSeq2+1 { //序号不连续
				utils.Println("RTP SequenceNumber error", r.lastSeq2, r.lastSeq)
				return
			}
		}
	}
	r.push(r.Timestamp, r.Payload)
}
