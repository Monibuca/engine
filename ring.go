package engine

import (
	"sync"

	"github.com/Monibuca/engine/v2/avformat"
)

const RING_SIZE_EXP = 10
const RING_SIZE = 1 << RING_SIZE_EXP
const RING_SIZE_MASK = RING_SIZE - 1

type RingItem struct {
	*avformat.AVPacket
	*sync.RWMutex
}
type Ring struct {
	*RingItem
	buffer []*RingItem
	Index  int
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
func (r *Ring) offset(v int) int {
	return (r.Index + v) & RING_SIZE_MASK
}
func (r *Ring) GoTo(index int) {
	r.Index = index
	r.RingItem = r.buffer[index]
}
func (r *Ring) GetAt(index int) *RingItem {
	return r.buffer[index]
}
func (r *Ring) GetNext() *RingItem {
	return r.buffer[r.offset(1)]
}
func (r *Ring) GetLast() *RingItem {
	return r.buffer[r.offset(-1)]
}
func (r *Ring) GoNext() {
	r.Index = r.offset(1)
	r.RingItem = r.buffer[r.Index]
}
func (r *Ring) GoBack() {
	r.Index = r.offset(-1)
	r.RingItem = r.buffer[r.Index]
}
func (r *Ring) NextW() {
	item := r.RingItem
	r.GoNext()
	r.RingItem.Lock()
	item.Unlock()
}
func (r *Ring) NextR() {
	r.RingItem.RUnlock()
	r.GoNext()
}
func (r Ring) Clone() *Ring {
	return &r
}
