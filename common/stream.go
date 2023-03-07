package common

import (
	"time"

	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type IStream interface {
	AddTrack(*util.Promise[Track])
	RemoveTrack(Track)
	Close()
	IsClosed() bool
	SSRC() uint32
	log.Zap
	Receive(any) bool
	SetIDR(Track)
	GetPublisherConfig() *config.Publish
	GetStartTime() time.Time
}
