package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pkg/errors"
)

// SubscriberInfo 订阅者可序列化信息，用于控制台输出
type SubscriberInfo struct {
	ID            string
	TotalDrop     int //总丢帧
	TotalPacket   int
	Type          string
	BufferLength  int
	Delay         uint32
	SubscribeTime time.Time
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	context.Context
	*Stream
	SubscriberInfo
	cancel     context.CancelFunc
	Sign       string
	OffsetTime uint32
	startTime  uint32
	vtIndex int //第几个视频轨
	atIndex int //第几个音频轨
}

// IsClosed 检查订阅者是否已经关闭
func (s *Subscriber) IsClosed() bool {
	return s.Context != nil && s.Err() != nil
}

// Close 关闭订阅者
func (s *Subscriber) Close() {
	if s.cancel != nil {
		s.UnSubscribe(s)
		s.cancel()
	}
}

func (s *Subscriber) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.SubscriberInfo)
}
//Subscribe 开始订阅
func (s *Subscriber) Subscribe(streamPath string) error {
	if !config.EnableWaitStream && FindStream(streamPath) == nil {
		return errors.Errorf("Stream not found:%s", streamPath)
	}
	GetStream(streamPath).Subscribe(s)
	if s.Context == nil {
		return errors.Errorf("stream not exist:%s", streamPath)
	}
	return nil
}