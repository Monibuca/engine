package track

import (
	"context"

	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/util"
)

type DataReader[T any] struct {
	Ctx context.Context
	// common.Track
	*util.Ring[common.DataFrame[T]]
	// startTime time.Time
	// Frame     *common.DataFrame[T]
	// Delay     uint32
	// *log.Logger
}

func (r *DataReader[T]) Read() (item *common.DataFrame[T]) {
	item = &r.Value
	if r.Ctx.Err() == nil && !item.CanRead {
		item.Wait()
	}
	return
}

func (r *DataReader[T]) TryRead() (item *common.DataFrame[T]) {
	if item = &r.Value; item.CanRead {
		return
	}
	return nil
}

func (r *DataReader[T]) MoveNext() {
	r.Ring = r.Next()
}
