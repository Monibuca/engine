package engine

import (
	"log"
	"reflect"
	"time"
)

// Publisher 发布者接口
type Publisher interface {
	OnClosed()
}

// InputStream 发布者实体定义
type InputStream struct {
	*Room
}

// Close 关闭发布者
func (p *InputStream) Close() {
	if p.Running() {
		p.Cancel()
	}
}

// Running 发布者是否正在发布
func (p *InputStream) Running() bool {
	return p.Room != nil && p.Err() == nil
}

// OnClosed 发布者关闭事件，用于回收资源
func (p *InputStream) OnClosed() {
}

// Publish 发布者进行发布操作
func (p *InputStream) Publish(streamPath string, publisher Publisher) bool {
	p.Room = AllRoom.Get(streamPath)
	//检查是否已存在发布者
	if p.Publisher != nil {
		return false
	}
	p.Publisher = publisher
	//反射获取发布者类型信息
	p.Type = reflect.ValueOf(publisher).Elem().Type().Name()
	log.Printf("publish set :%s", p.Type)
	p.StartTime = time.Now()
	//触发钩子
	OnPublishHooks.Trigger(p.Room)

	return true
}
