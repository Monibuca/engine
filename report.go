package engine

import (
	"time"

	"gopkg.in/yaml.v3"
	"m7s.live/engine/v4/common"
)

type ReportCreateStream struct {
	StreamPath string
	Time       int64
}

type ReportCloseStream struct {
	StreamPath string
	Time       int64
}

type ReportAddTrack struct {
	Name       string
	StreamPath string
	Time       int64
}
type ReportTrackInfo struct {
	BPS    int
	FPS    int
	Drops  int
	RBSize int
}
type ReportPulse struct {
	StreamPath  string
	Tracks      map[string]ReportTrackInfo
	Subscribers map[string]struct {
		Type    string
		Readers map[string]struct {
			Delay uint32
		}
	}
	Time int64
}

type Reportor struct {
	Subscriber
	pulse ReportPulse
}

func (r *Reportor) OnEvent(event any) {
	switch v := event.(type) {
	case PulseEvent:
		r.pulse.Tracks = make(map[string]ReportTrackInfo)
		r.pulse.Subscribers = make(map[string]struct {
			Type    string
			Readers map[string]struct {
				Delay uint32
			}
		})
		r.Stream.Tracks.Range(func(k string, t common.Track) {
			track := t.GetBase()
			r.pulse.Tracks[k] = ReportTrackInfo{
				BPS:    track.BPS,
				FPS:    track.FPS,
				Drops:  track.Drops,
				RBSize: t.GetRBSize(),
			}
		})
		r.Stream.Subscribers.RangeAll(func(sub ISubscriber, wait *waitTracks) {
			suber := sub.GetSubscriber()
			r.pulse.Subscribers[suber.ID] = struct {
				Type    string
				Readers map[string]struct {
					Delay uint32
				}
			}{Type: suber.Type, Readers: map[string]struct {
				Delay uint32
			}{suber.Audio.Name: {Delay: suber.AudioReader.Delay}, suber.Video.Name: {Delay: suber.VideoReader.Delay}}}
		})
		r.pulse.Time = time.Now().Unix()
		EngineConfig.Report("pulse", r.pulse)
	case common.Track:
		EngineConfig.Report("addtrack", &ReportAddTrack{v.GetBase().Name, r.Stream.Path, time.Now().Unix()})
	}
}

func (conf *GlobalConfig) OnEvent(event any) {
	if !conf.GetEnableReport() {
		conf.Engine.OnEvent(event)
		return
	}
	switch v := event.(type) {
	case SEcreate:
		conf.Report("create", &ReportCreateStream{v.Target.Path, time.Now().Unix()})
		var reportor Reportor
		reportor.IsInternal = true
		reportor.pulse.StreamPath = v.Target.Path
		if Engine.Subscribe(v.Target.Path, &reportor) == nil {
			reportor.SubPulse()
		}
	case SEpublish:
	case SErepublish:
	case SEKick:
	case SEclose:
		conf.Report("close", &ReportCloseStream{v.Target.Path, time.Now().Unix()})
	case SEwaitClose:
	case SEwaitPublish:
	case ISubscriber:
	case UnsubscribeEvent:
	default:
		conf.Engine.OnEvent(event)
	}
}

func (conf *GlobalConfig) Report(t string, v any) {
	out, err := yaml.Marshal(v)
	if err == nil {
		conf.Engine.OnEvent(append([]byte("type: "+t+"\n"), out...))
	}
}
