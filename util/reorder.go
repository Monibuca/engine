package util

// RTPReorder RTP包乱序重排
type RTPReorder[T any] struct {
	lastSeq uint16 //最新收到的rtp包序号
	queue   []*T   // 缓存队列,0号元素位置代表lastReq+1，永远保持为空
}

func (p *RTPReorder[T]) Push(seq uint16, v *T) *T {
	// 初始化
	if len(p.queue) == 0 {
		p.lastSeq = seq
		p.queue = make([]*T, 20)
		return v
	}
	if seq < p.lastSeq && p.lastSeq-seq < 0x8000 {
		// 旧的包直接丢弃
		return nil
	}
	delta := seq - p.lastSeq
	if delta == 1 {
		// 正常顺序,无需缓存
		p.lastSeq = seq
		p.pop()
		return v
	}
	if seq > p.lastSeq {
		//delta必然大于1
		queueLen := uint16(len(p.queue))
		if queueLen < delta {
			//超过缓存最大范围,无法挽回,只能造成丢包（序号断裂）
			for {
				p.lastSeq++
				delta--
				p.pop()
				// 可以放得进去了
				if delta == queueLen-1 {
					p.queue[queueLen-1] = v
					v = p.queue[0]
					p.queue[0] = nil
					return v
				}
			}
		} else {
			// 出现后面的包先到达，缓存起来
			p.queue[delta-1] = v
			return nil
		}
	}
	return nil
}

func (p *RTPReorder[T]) pop() {
	copy(p.queue, p.queue[1:]) //整体数据向前移动一位，保持0号元素代表lastSeq+1
}

// Pop 从缓存中取出一个包，需要连续调用直到返回nil
func (p *RTPReorder[T]) Pop() (next *T) {
	if len(p.queue) == 0 {
		return
	}
	if next = p.queue[0]; next != nil {
		p.lastSeq++
		p.pop()
	}
	return
}
