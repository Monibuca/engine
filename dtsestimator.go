package engine

// DTSEstimator is a DTS estimator.
type DTSEstimator struct {
	prevDTS     uint32
	prevPTS     uint32
	prevPrevPTS uint32
	dts         func(uint32) uint32
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

// NewDTSEstimator allocates a DTSEstimator.
func NewDTSEstimator() *DTSEstimator {
	result := &DTSEstimator{}
	result.dts = func(pts uint32) uint32 {
		if pts > 0 {
			result.dts = result._dts
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

	return dts
}
