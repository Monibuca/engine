package engine

type TSSlice []uint32

func (s TSSlice) Len() int           { return len(s) }
func (s TSSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s TSSlice) Less(i, j int) bool { return s[i] < s[j] }

type B struct {
	TSSlice
	data  []*RTPNalu
	MaxTS uint32
}

func (b *B) Push(x interface{}) {
	p := x.(*RTPNalu)
	if p.PTS > b.MaxTS {
		b.MaxTS = p.PTS
	}
	b.TSSlice = append(b.TSSlice, p.PTS)
	b.data = append(b.data, p)
}

func (b *B) Pop() interface{} {
	l := b.Len()-1
	defer func() {
		b.TSSlice = b.TSSlice[:l]
		b.data = b.data[:l]
	}()
	return struct {
		DTS uint32
		*RTPNalu
	}{DTS: b.TSSlice[l], RTPNalu: b.data[l]}
}
