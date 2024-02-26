package util

import (
	"testing"
)

func TestDts(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		dtsg := NewDTSEstimator()
		var pts uint32 = 0xFFFFFFFF - 5
		for i := 0; i < 10; i++ {
			dts := dtsg.Feed(pts)
			pts++
			t.Logf("dts=%d", dts)
		}
	})
}

func Test往前跳(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		data := []uint32{64175310, 64178910, 64182510, 64186110, 64189710, 64340910, 64344510, 64348110, 64351710, 64355310, 64358910}
		dtsg := NewDTSEstimator()
		for _, pts := range data {
			dts := dtsg.Feed(pts)
			t.Logf("pts=%d,dts=%d", pts, dts)
		}
	})
}
