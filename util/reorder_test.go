package util

import (
	"testing"
)

type stuff struct {
	seq uint16
}

func (s *stuff) Clone() *stuff {
	return s
}

func TestReorder(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		var recoder RTPReorder[*stuff]
		for i := (uint16)(0); i < 25; i++ {
			recoder.Push(i*2, &stuff{seq: i * 2})
		}
		if recoder.Pop() != nil {
			t.Error("pop nil")
		}
		for i := (uint16)(0); i < 25; i++ {
			x := recoder.Push(i*2+1, &stuff{seq: i*2 + 1})
			if x != nil {
				t.Logf("%d", x.seq)
			}
			for {
				if x = recoder.Pop(); x == nil {
					break
				} else {
					t.Logf("%d", x.seq)
				}
			}
		}
	})
}
