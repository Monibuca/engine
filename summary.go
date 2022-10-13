package engine

import (
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var summary Summary
var children util.Map[string, *Summary]

func init() {
	children.Init()
	go summary.Start()
}

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
	NetWork     []NetWorkInfo
	Streams     []StreamSummay
	lastNetWork []net.IOCountersStat
	ref         int32
}

// NetWorkInfo 网速信息
type NetWorkInfo struct {
	Name         string
	Receive      uint64
	Sent         uint64
	ReceiveSpeed uint64
	SentSpeed    uint64
}

// StartSummary 开始定时采集数据，每秒一次
func (s *Summary) Start() {
	for range time.Tick(time.Second) {
		if s.ref > 0 {
			summary.collect()
		}
	}
}
func (s *Summary) Point() *Summary {
	return s
}

// Running 是否正在采集数据
func (s *Summary) Running() bool {
	return s.ref > 0
}

// Add 增加订阅者
func (s *Summary) Add() {
	if count := atomic.AddInt32(&s.ref, 1); count == 1 {
		log.Info("start report summary")
	} else {
		log.Info("summary count", count)
	}
}

// Done 删除订阅者
func (s *Summary) Done() {
	if count := atomic.AddInt32(&s.ref, -1); count == 0 {
		log.Info("stop report summary")
		s.lastNetWork = nil
	} else {
		log.Info("summary count", count)
	}
}

// Report 上报数据
func (s *Summary) Report(slave *Summary) {
	children.Set(slave.Address, slave)
}

func (s *Summary) collect() *Summary {
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
		if s.lastNetWork != nil && len(s.lastNetWork) > i {
			info.ReceiveSpeed = n.BytesRecv - s.lastNetWork[i].BytesRecv
			info.SentSpeed = n.BytesSent - s.lastNetWork[i].BytesSent
		}
		netWorks = append(netWorks, info)
	}
	s.NetWork = netWorks
	s.lastNetWork = nv
	s.Streams = util.MapList(&Streams, func(name string, ss *Stream) StreamSummay {
		return ss.Summary()
	})
	return s
}
