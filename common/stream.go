package common

import (
	"m7s.live/engine/v4/log"
)

type IStream interface {
	AddTrack(Track)
	RemoveTrack(Track)
	IsClosed() bool
	SSRC() uint32
	log.Zap
	Receive(any) bool
}
