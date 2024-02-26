package common

import (
	"context"
	"time"

	"github.com/pion/rtp"
	"go.uber.org/zap/zapcore"
	"m7s.live/engine/v4/codec"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type TimelineData[T any] struct {
	Timestamp time.Time
	Value     T
}
type TrackState byte

const (
	TrackStateOnline  TrackState = iota // 上线
	TrackStateOffline                   // 下线
)

type IIO interface {
	IsClosed() bool
	OnEvent(any)
	Stop(reason ...zapcore.Field)
	SetIO(any)
	SetParentCtx(context.Context)
	SetLogger(*log.Logger)
	IsShutdown() bool
	GetStream() IStream
	log.Zap
}

type IPuber interface {
	IIO
	GetAudioTrack() AudioTrack
	GetVideoTrack() VideoTrack
	GetConfig() *config.Publish
	Publish(streamPath string, pub IPuber) error
}

type Track interface {
	GetPublisher() IPuber
	GetReaderCount() int32
	GetName() string
	GetBPS() int
	GetFPS() int
	GetDrops() int
	LastWriteTime() time.Time
	SnapForJson()
	SetStuff(stuff ...any)
	GetRBSize() int
	Dispose()
}

type AVTrack interface {
	Track
	PreFrame() *AVFrame
	CurrentFrame() *AVFrame
	Attach()
	Detach()
	WriteAVCC(ts uint32, frame *util.BLL) error //写入AVCC格式的数据
	WriteRTP(*util.ListItem[RTPFrame])
	WriteRTPPack(*rtp.Packet)
	WriteSequenceHead(sh []byte) error
	Flush()
	SetSpeedLimit(time.Duration)
	GetRTPFromPool() *util.ListItem[RTPFrame]
	GetFromPool(util.IBytes) *util.ListItem[util.Buffer]
}
type VideoTrack interface {
	AVTrack
	GetCodec() codec.VideoCodecID
	WriteSliceBytes(slice []byte)
	WriteNalu(uint32, uint32, []byte)
	WriteAnnexB(uint32, uint32, []byte)
	SetLostFlag()
}

type AudioTrack interface {
	AVTrack
	GetCodec() codec.AudioCodecID
	WriteADTS(uint32, util.IBytes)
	WriteRawBytes(uint32, util.IBytes)
	Narrow()
}
