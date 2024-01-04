package util

// 时间戳评估，对于时间戳不规范的情况，需要纠正，使得时间戳自增，相对合理

// 定义一个全局结构来存储状态
type TimestampProcessor struct {
	baseTimestamp   int
	lastTimestamp   int
	normalTotalTime int
	averageInterval int
	normalCount     int
}

// 处理单个时间戳的函数
func (p *TimestampProcessor) ProcessTimestamp(timestamp int) int {
	if p.normalCount == 0 {
		// 处理第一个时间戳
		p.baseTimestamp = timestamp
		p.lastTimestamp = timestamp
		p.normalCount = 1
		return timestamp
	}

	delta := timestamp - p.lastTimestamp
	// 计算当前间隔
	currentInterval := Conditoinal(delta > 0, delta, -delta)
	// 判断是否为突变
	if p.averageInterval > 0 && currentInterval > 10*p.averageInterval {
		// 突变，调整起始时间戳和相关累计信息
		p.baseTimestamp = p.lastTimestamp
		p.normalTotalTime = p.averageInterval
		p.normalCount = 1
	} else {
		// 非突变，累加时间和更新计数
		p.normalTotalTime += currentInterval
		p.normalCount++
		p.averageInterval = p.normalTotalTime / (p.normalCount - 1)
	}

	// 更新最后一个时间戳
	p.lastTimestamp = timestamp

	// 计算输出时间戳
	outputTimestamp := p.baseTimestamp + p.normalTotalTime

	return outputTimestamp
}
