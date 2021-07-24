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
		go AddHook("test", func(a, b int) {
			fmt.Printf("on test,%d,%d", a, b)
		})
		go AddHook("done", wg.Done)
		TriggerHook("test", 2, 10)
		go AddHook("test", func(a, b int) {
			fmt.Printf("on test,%d,%d", a, b)
		})
		<-time.After(time.Millisecond * 100)
		TriggerHook("test", 1, 12)
		<-time.After(time.Millisecond * 100)
		TriggerHook("done")
		wg.Wait()
	})
}
