package engine

import (
	"context"
	"time"

	"github.com/Monibuca/engine/v2/avformat"
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
	MetaData   func(stream *Stream) error
	OnData     func(*avformat.SendPacket) error
	Cancel     context.CancelFunc
	Sign       string
	OffsetTime uint32
	startTime  uint32
	avformat.SendPacket
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
	return s.SubscribeWithContext(streamPath, nil)
}

// SubscribeWithContext 带额外取消功能的订阅
func (s *Subscriber) SubscribeWithContext(streamPath string, ctx context.Context) (err error) {
	if !config.EnableWaitStream && FindStream(streamPath) == nil {
		return errors.Errorf("Stream not found:%s", streamPath)
	}
	GetStream(streamPath).Subscribe(s)
	if s.Context == nil {
		return errors.Errorf("stream not exist:%s", streamPath)
	}
	defer s.UnSubscribe(s)
	var done <-chan struct{}
	if ctx != nil {
		done = ctx.Done()
	}
	select {
	//等待发布者首屏数据，如果发布者尚为发布，则会等待，否则就会往下执行
	case <-s.WaitPub:
	case <-s.Context.Done():
		return s.Err()
	case <-done:
		return ctx.Err()
	}
	if s.MetaData != nil {
		if err = s.MetaData(s.Stream); err != nil {
			return err
		}
	}
	if *s.EnableVideo {
		s.sendAv(s.VideoTag, 0)
		packet := s.FirstScreen.Clone()
		s.startTime = packet.Timestamp // 开始时间戳，第一个关键帧的
		s.Delay = s.AVRing.GetLast().Timestamp - packet.Timestamp
		packet.Wait()
		s.send(packet)
		packet.NextR()
		// targetStartTime := s.AVRing.GetLast().Timestamp //实际开始时间戳
		for atsent, dropping, droped := s.AudioTag == nil, false, 0; s.Err() == nil; packet.NextR() {
			s.TotalPacket++
			if !dropping {
				if !atsent && packet.Type == avformat.FLV_TAG_TYPE_AUDIO {
					s.sendAv(s.AudioTag, 0)
					atsent = true
				}
				s.sendAv(&packet.AVPacket, packet.Timestamp-s.startTime)
				// if targetStartTime > s.startTime {
				// 	s.startTime++ //逐步追赶，使得开始时间逼近实际开始时间戳
				// }
				if s.checkDrop(packet) {
					dropping = true
					droped = 0
				}
			} else if packet.IsKeyFrame {
				//遇到关键帧则退出丢帧
				dropping = false
				//fmt.Println("drop package ", droped)
				s.TotalDrop += droped
				s.send(packet)
			} else {
				droped++
			}
		}
	} else if *s.EnableAudio {
		if s.AudioTag != nil {
			s.sendAv(s.AudioTag, 0)
		}
		for packet := s.AVRing; s.Err() == nil; packet.NextR() {
			s.TotalPacket++
			s.send(packet)
		}
	}
	return s.Err()
}
func (s *Subscriber) sendAv(packet *avformat.AVPacket, t uint32) {
	s.AVPacket = packet
	s.Timestamp = t
	if s.OnData(&s.SendPacket) != nil {
		s.Close()
	}
}
func (s *Subscriber) send(packet *Ring) {

	s.sendAv(&packet.AVPacket, packet.Timestamp-s.startTime)
}
func (s *Subscriber) checkDrop(packet *Ring) bool {
	pIndex := s.AVRing.Index
	s.BufferLength = pIndex - packet.Index
	s.Delay = s.AVRing.Timestamp - packet.Timestamp
	return s.BufferLength > s.AVRing.Size/2
}
