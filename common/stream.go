package common

import (
	"v4.m7s.live/engine/log"
)

type IStream interface {
	AddTrack(Track)
	RemoveTrack(Track)
	IsClosed() bool
	SSRC() uint32
	log.Zap
	Receive(any) bool
}
