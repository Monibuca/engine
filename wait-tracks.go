package engine

import (
	"m7s.live/engine/v4/common"
	. "m7s.live/engine/v4/common"
	"m7s.live/engine/v4/track"
	"m7s.live/engine/v4/util"
)

type waitTrackNames []string

// Waiting是否正在等待
func (w waitTrackNames) Waiting() bool {
	return w != nil
}

// Waitany 是否等待任意的
func (w waitTrackNames) Waitany() bool {
	return len(w) == 0
}

// Wait 设置需要等待的名称，空数组为等待任意的
func (w *waitTrackNames) Wait(names ...string) {
	if names == nil {
		*w = make([]string, 0)
	} else {
		*w = names
	}
}

// StopWait 不再需要等待了
func (w *waitTrackNames) StopWait() {
	*w = nil
}
func (w waitTrackNames) InviteTrack(suber ISubscriber) {
	if len(w) > 0 {
		InviteTrack(w[0], suber)
	}
}
// Accept 检查名称是否在等待候选项中
func (w *waitTrackNames) Accept(name string) bool {
	if !w.Waiting() {
		return false
	}
	if w.Waitany() {
		w.StopWait()
		return true
	} else {
		for _, n := range *w {
			if n == name {
				w.StopWait()
				return true
			}
		}
	}
	return false
}

type waitTracks struct {
	*util.Promise[ISubscriber] // 等待中的Promise
	audio                      waitTrackNames
	video                      waitTrackNames
	data                       waitTrackNames
}

// NeedWait 是否需要等待Track
func (w *waitTracks) NeedWait() bool {
	return w.audio.Waiting() || w.video.Waiting() || w.data.Waiting()
}

// Accept 有新的Track来到，检查是否可以不再需要等待了
func (w *waitTracks) Accept(t Track) bool {
	suber := w.Promise.Value
	switch t.(type) {
	case *track.Audio:
		if w.audio.Accept(t.GetName()) {
			suber.OnEvent(t)
		}
	case *track.Video:
		if w.video.Accept(t.GetName()) {
			suber.OnEvent(t)
		}
	case common.Track:
		w.data.Accept(t.GetName())
		suber.OnEvent(t)
	}
	if w.NeedWait() {
		return false
	} else {
		w.Resolve()
		return true
	}
}
