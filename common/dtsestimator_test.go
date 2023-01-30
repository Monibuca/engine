package common

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
