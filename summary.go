package engine

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"m7s.live/engine/v4/util"
)

var (
	summary SummaryUtil
	lastSummary Summary
	children util.Map[string, *Summary]
	collectLock sync.RWMutex
)
// ServerSummary 系统摘要定义
type Summary struct {
	Address string
	Memory  struct {
		Total uint64
		Free  uint64
		Used  uint64
		Usage float64
	}
	CPUUsage float64
	HardDisk struct {
		Total uint64
		Free  uint64
		Used  uint64
		Usage float64
	}
	NetWork []NetWorkInfo
	Streams []StreamSummay
	ts      time.Time //上次更新时间
}

// NetWorkInfo 网速信息
type NetWorkInfo struct {
	Name         string
	Receive      uint64
	Sent         uint64
	ReceiveSpeed uint64
	SentSpeed    uint64
}
type SummaryUtil Summary
// Report 上报数据
func (s *Summary) Report(slave *Summary) {
	children.Set(slave.Address, slave)
}

func (s *SummaryUtil) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.collect())
}

func (s *SummaryUtil) MarshalYAML() (any, error) {
	return s.collect(), nil
}

func (s *SummaryUtil) collect() *Summary {
	if collectLock.TryLock() {
		defer collectLock.Unlock()
	} else {
		collectLock.RLock()
		defer collectLock.RUnlock()
		return &lastSummary
	}
	dur := time.Since(s.ts)
	if dur < time.Second {
		return &lastSummary
	}
	s.ts = time.Now()
	v, _ := mem.VirtualMemory()
	d, _ := disk.Usage("/")
	nv, _ := net.IOCounters(true)

	s.Memory.Total = v.Total >> 20
	s.Memory.Free = v.Available >> 20
	s.Memory.Used = v.Used >> 20
	s.Memory.Usage = v.UsedPercent

	if cc, _ := cpu.Percent(time.Second, false); len(cc) > 0 {
		s.CPUUsage = cc[0]
	}
	s.HardDisk.Free = d.Free >> 30
	s.HardDisk.Total = d.Total >> 30
	s.HardDisk.Used = d.Used >> 30
	s.HardDisk.Usage = d.UsedPercent
	netWorks := []NetWorkInfo{}
	for i, n := range nv {
		info := NetWorkInfo{
			Name:    n.Name,
			Receive: n.BytesRecv,
			Sent:    n.BytesSent,
		}
		if len(lastSummary.NetWork) > i {
			info.ReceiveSpeed = (n.BytesRecv - lastSummary.NetWork[i].Receive) / uint64(dur.Seconds())
			info.SentSpeed = (n.BytesSent - lastSummary.NetWork[i].Sent) / uint64(dur.Seconds())
		}
		netWorks = append(netWorks, info)
	}
	s.NetWork = netWorks
	s.Streams = util.MapList(&Streams, func(name string, ss *Stream) StreamSummay {
		return ss.Summary()
	})
	lastSummary = Summary(*s)
	return &lastSummary
}
