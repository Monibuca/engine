package common

// DTSEstimator is a DTS estimator.
type DTSEstimator struct {
	hasB    bool
	prevPTS uint32
	prevDTS uint32
	cache   []uint32
}

// NewDTSEstimator allocates a DTSEstimator.
func NewDTSEstimator() *DTSEstimator {
	result := &DTSEstimator{}
	return result
}

func (d *DTSEstimator) add(pts uint32) {
	i := 0
	if len(d.cache) >= 4 {
		i = len(d.cache) - 3
	}

	var new_cache []uint32
	for ; i < len(d.cache); i = i + 1 {
		if d.cache[i] > pts {
			break
		}
		new_cache = append(new_cache, d.cache[i])
	}
	new_cache = append(new_cache, pts)
	new_cache = append(new_cache, d.cache[i:]...)
	d.cache = new_cache
}

// Feed provides PTS to the estimator, and returns the estimated DTS.
func (d *DTSEstimator) Feed(pts uint32) uint32 {
	d.add(pts)
	dts := pts
	if !d.hasB {
		if pts < d.prevPTS {
			d.hasB = true
			dts = d.cache[0]
		}
	} else {
		dts = d.cache[0]
	}

	if d.prevDTS > dts {
		dts = d.prevDTS
	}

	d.prevPTS = pts
	d.prevDTS = dts
	return dts
}
