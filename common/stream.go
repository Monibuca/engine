package common

import (
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
)

type IStream interface {
	AddTrack(Track)
	RemoveTrack(Track)
	Close()
	IsClosed() bool
	SSRC() uint32
	log.Zap
	Receive(any) bool
	SetIDR(Track)
	GetPublisherConfig() *config.Publish
}
