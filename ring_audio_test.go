package engine

import (
	"testing"
	"time"
)

func TestRing_Audio_Dispose(t *testing.T) {
	tests := []struct {
		name string
		r    *Ring_Audio
	}{
		{"1", NewRing_Audio()}, {"2", NewRing_Audio()}, {"3", NewRing_Audio()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			time.AfterFunc(time.Second/2, tt.r.Dispose)
			ttt := time.Now()
			for time.Since(ttt) < time.Second {
				tt.r.NextW()
			}
		})
	}
}
