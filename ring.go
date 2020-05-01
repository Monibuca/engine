package engine

import (
	"sync"

	"github.com/Monibuca/engine/avformat"
)

const RING_SIZE = 256

type RingItem struct {
	*avformat.AVPacket
	*sync.RWMutex
}
type Ring struct {
	*RingItem
	buffer []*RingItem
	Index  byte
}

func NewRing() (r *Ring) {
	r = new(Ring)
	r.buffer = make([]*RingItem, RING_SIZE)
	for i := 0; i < RING_SIZE; i++ {
		r.buffer[i] = &RingItem{new(avformat.AVPacket), new(sync.RWMutex)}
	}
	r.RingItem = r.buffer[0]
	r.Lock()
	return
}

func (r *Ring) GetNext() *RingItem {
	return r.buffer[r.Index+1]
}
func (r *Ring) GetLast() *RingItem {
	return r.buffer[r.Index-1]
}
func (r *Ring) GoNext() {
	r.Index++
	r.RingItem = r.buffer[r.Index]
}
func (r *Ring) GoBack() {
	r.Index--
	r.RingItem = r.buffer[r.Index]
}
func (r *Ring) NextW() {
	r.Index++
	item := r.RingItem
	r.RingItem = r.buffer[r.Index]
	r.RingItem.Lock()
	item.UnLock()
}
func (r *Ring) NextR() {
	r.RingItem.RUnlock()
	r.GoNext()
}
func (r Ring) Clone() *Ring {
	return &r
}
