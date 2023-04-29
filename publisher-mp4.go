package engine

import (
	"io"

	"github.com/yapingcat/gomedia/go-mp4"
	"go.uber.org/zap"
	"m7s.live/engine/v4/track"
)

type MP4Publisher struct {
	Publisher
	*mp4.MovDemuxer `json:"-" yaml:"-"`
}

// Start reading the MP4 file
func (p *MP4Publisher) ReadMP4Data(source io.ReadSeeker) error {
	defer p.Stop()
	p.MovDemuxer = mp4.CreateMp4Demuxer(source)
	if tracks, err := p.ReadHead(); err != nil {
		if err == io.EOF {
			p.Info("Reached end of MP4 file")
			return nil
		}
		p.Error("Error reading MP4 header", zap.Error(err))
		return err
	} else {
		info := p.GetMp4Info()
		p.Info("MP4 info", zap.Any("info", info))
		for _, t := range tracks {
			p.Info("MP4 track", zap.Any("track", t))
			switch t.Cid {
			case mp4.MP4_CODEC_H264:
				p.VideoTrack = track.NewH264(p.Stream)
			case mp4.MP4_CODEC_H265:
				p.VideoTrack = track.NewH265(p.Stream)
			case mp4.MP4_CODEC_AAC:
				p.AudioTrack = track.NewAAC(p.Stream)
			case mp4.MP4_CODEC_G711A:
				p.AudioTrack = track.NewG711(p.Stream, true)
			case mp4.MP4_CODEC_G711U:
				p.AudioTrack = track.NewG711(p.Stream, false)
			}
		}
		for {
			pkg, err := p.ReadPacket()
			if err != nil {
				p.Error("Error reading MP4 packet", zap.Error(err))
				return err
			}
			switch pkg.Cid {
			case mp4.MP4_CODEC_H264, mp4.MP4_CODEC_H265:
				p.VideoTrack.WriteAnnexB(uint32(pkg.Pts*90), uint32(pkg.Dts*90), pkg.Data)
			case mp4.MP4_CODEC_AAC:
				p.AudioTrack.WriteADTS(uint32(pkg.Pts*90), pkg.Data)
			case mp4.MP4_CODEC_G711A, mp4.MP4_CODEC_G711U:
				p.AudioTrack.WriteRaw(uint32(pkg.Pts*90), pkg.Data)
			}
		}
	}
}
