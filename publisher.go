package engine

import (
	"context"
	"time"
)

// Publisher 发布者实体定义
type Publisher struct {
	context.Context
	cancel context.CancelFunc
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
		if p.AVRing.Timeout() {
			p.cancel() //单独关闭Publisher而复用Stream
		} else {
			return false
		}
	}
	p.Publisher = p
	p.Context, p.cancel = context.WithCancel(p.Stream)
	p.StartTime = time.Now()
	//触发钩子
	OnPublishHooks.Trigger(p.Stream)
	return true
}
