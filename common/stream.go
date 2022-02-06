package common

import "context"

type IStream interface {
	context.Context
	Update() uint32
	AddTrack(Track)
}
