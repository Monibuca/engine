package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestAddHook(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)
		AddHook("test", func(a, b int) {
			fmt.Printf("on test1,%d,%d\n", a, b)
		})
		AddHook("done", wg.Done)
		<-time.After(time.Millisecond * 100)
		TriggerHook("test", 2, 10)
		AddHook("test", func(a, b int) {
			fmt.Printf("on test2,%d,%d\n", a, b)
		})
		<-time.After(time.Millisecond * 100)
		TriggerHook("test", 1, 12)
		<-time.After(time.Millisecond * 100)
		TriggerHook("done")
		wg.Wait()
	})
}
