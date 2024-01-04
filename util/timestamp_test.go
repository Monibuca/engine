package util

import (
	"testing"
)

func TestTimestamp(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		var p TimestampProcessor
		var testData = []int{0, 10, 20, 30, 40, 50, 60, 70, 80, 10, 20, 30, 40, 50, 60, 70, 80}
		for _, v := range testData {
			t.Log(p.ProcessTimestamp(v))
		}
	})
}
