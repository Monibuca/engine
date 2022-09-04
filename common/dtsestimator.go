package common

// DTSEstimator is a DTS estimator.
type DTSEstimator struct {
	prevDTS     uint32
	prevPTS     uint32
	prevPrevPTS uint32
	dts         func(uint32) uint32
	delta       uint32
}

func (d *DTSEstimator) _dts(pts uint32) uint32 {
	// P or I frame
	if pts > d.prevPTS {
		// previous frame was B
		// use the DTS of the previous frame
		if d.prevPTS < d.prevPrevPTS {
			return d.prevPTS
		}

		// previous frame was P or I
		// use two frames ago plus a small quantity
		// to avoid non-monotonous DTS with B-frames
		return d.prevPrevPTS + 1
	}

	// B Frame
	// increase by a small quantity
	return d.prevDTS + 1
}
func (d *DTSEstimator) _dts2(pts uint32) uint32 {
	return d.prevDTS + d.delta
}

// NewDTSEstimator allocates a DTSEstimator.
func NewDTSEstimator() *DTSEstimator {
	result := &DTSEstimator{}
	result.dts = func(pts uint32) uint32 {
		if result.prevPTS > 0 {
			result.delta = pts - result.prevPTS
			result.dts = result._dts2
		}
		return pts
	}
	return result
}

// Feed provides PTS to the estimator, and returns the estimated DTS.
func (d *DTSEstimator) Feed(pts uint32) uint32 {
	dts := d.dts(pts)

	d.prevPrevPTS = d.prevPTS
	d.prevPTS = pts
	d.prevDTS = dts
	if d.prevPTS > d.prevPrevPTS {
		delta := d.prevPTS - d.prevPrevPTS
		// if delta < d.delta {
			d.delta = delta
		// }
	} else {
		delta := d.prevPrevPTS - d.prevPTS
		// if delta < d.delta {
			d.delta = delta
		// }
	}
	return dts
}
