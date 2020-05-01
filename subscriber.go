package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/Monibuca/engine/avformat"
	"github.com/pkg/errors"
)

// SubscriberInfo 订阅者可序列化信息，用于控制台输出
type SubscriberInfo struct {
	ID            string
	TotalDrop     int //总丢帧
	TotalPacket   int
	Type          string
	BufferLength  byte
	Delay         uint32
	SubscribeTime time.Time
}

// Subscriber 订阅者实体定义
type Subscriber struct {
	context.Context
	*Stream
	SubscriberInfo
	OnData     func(*avformat.SendPacket) error
	Cancel     context.CancelFunc
	Sign       string
	OffsetTime uint32
}

// IsClosed 检查订阅者是否已经关闭
func (s *Subscriber) IsClosed() bool {
	return s.Context != nil && s.Err() != nil
}

// Close 关闭订阅者
func (s *Subscriber) Close() {
	if s.Cancel != nil {
		s.Cancel()
	}
}

//Subscribe 开始订阅
func (s *Subscriber) Subscribe(streamPath string) (err error) {
	if !config.EnableWaitStream {
		if _, ok := streamCollection.Load(streamPath); !ok {
			return errors.New(fmt.Sprintf("Stream not found:%s", streamPath))
		}
	}
	GetStream(streamPath).Subscribe(s)
	defer s.UnSubscribe(s)
	//加锁解锁的目的是等待发布者首屏数据，如果发布者尚为发布，则会等待，否则就会往下执行
	s.WaitingMutex.RLock()
	s.WaitingMutex.RUnlock()
	sendPacket := avformat.NewSendPacket(s.VideoTag, 0)
	defer sendPacket.Recycle()
	s.OnData(sendPacket)
	packet := s.FirstScreen.Clone()
	startTime := packet.Timestamp
	packet.RLock()
	sendPacket.AVPacket = packet.AVPacket
	s.OnData(sendPacket)
	packet.NextR()
	atsent := false
	dropping := false
	droped := 0
	for {
		select {
		case <-s.Done():
			return s.Err()
		default:
			s.TotalPacket++
			packet.RLock()
			if !dropping {
				if packet.Type == avformat.FLV_TAG_TYPE_AUDIO && !atsent {
					sendPacket.AVPacket = s.AudioTag
					sendPacket.Timestamp = 0
					s.OnData(sendPacket)
					atsent = true
				}
				sendPacket.AVPacket = packet.AVPacket
				sendPacket.Timestamp = packet.Timestamp - startTime
				s.OnData(sendPacket)
				if s.checkDrop(packet) {
					dropping = true
					droped = 0
				}
				packet.NextR()
			} else if packet.IsKeyFrame {
				//遇到关键帧则退出丢帧
				dropping = false
				//fmt.Println("drop package ", droped)
				s.TotalDrop += droped
				packet.RUnlock()
			} else {
				droped++
				packet.NextR()
			}
		}
	}
}
func (s *OutputStream) checkDrop(packet *Ring) bool {
	pIndex := s.AVRing.Index
	s.BufferLength = pIndex - packet.Index
	s.Delay = s.AVRing.Timestamp - packet.Timestamp
	return s.BufferLength > RING_SIZE/2
}
