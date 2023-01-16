package util

import (
	"testing"
)

func TestBuffer(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		var b Buffer
		t.Log(b == nil)
		b.Write([]byte{1, 2, 3})
		if b == nil {
			t.Fail()
		} else {
			t.Logf("b:% x", b)
		}
	})
}

func TestMallocSlice(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		var a [][]byte = [][]byte{}
		b := MallocSlice(&a)
		if *b != nil {
			t.Fail()
		} else if *b = []byte{1}; a[0][0] != 1 {
			t.Fail()
		}
	})
}
