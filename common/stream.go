package common

import (
	"context"

	log "github.com/sirupsen/logrus"
)

type IStream interface {
	context.Context
	Update() uint32
	AddTrack(Track)
	IsClosed() bool
	log.Ext1FieldLogger
	SSRC() uint32
}
