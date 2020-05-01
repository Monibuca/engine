package engine

import (
	"time"
)

// Publisher 发布者实体定义
type Publisher struct {
	*Stream
}

// Close 关闭发布者
func (p *Publisher) Close() {
	if p.Running() {
		p.Cancel()
	}
}

// Running 发布者是否正在发布
func (p *Publisher) Running() bool {
	return p.Stream != nil && p.Err() == nil
}

// Publish 发布者进行发布操作
func (p *Publisher) Publish(streamPath string) bool {
	p.Stream = GetStream(streamPath)
	//检查是否已存在发布者
	if p.Publisher != nil {
		return false
	}
	p.Publisher = p
	p.StartTime = time.Now()
	//触发钩子
	OnPublishHooks.Trigger(p.Stream)
	return true
}
