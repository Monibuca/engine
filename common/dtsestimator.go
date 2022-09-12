package common

// DTSEstimator is a DTS estimator.
type DTSEstimator struct {
	initializing int
	prevDTS      uint32
	prevPTS      uint32
	prevPrevPTS  uint32
}

// NewDTSEstimator allocates a DTSEstimator.
func NewDTSEstimator() *DTSEstimator {
	return &DTSEstimator{
		initializing: 2,
	}
}

// Feed provides PTS to the estimator, and returns the estimated DTS.
func (d *DTSEstimator) Feed(pts uint32) (dts uint32) {
	dts = pts
	switch d.initializing {
	case 2:
		if d.prevPrevPTS > 0 {
			d.initializing--
		}
	case 1:
		if pts < d.prevPTS {
			dts = d.prevDTS + 1
			d.initializing--
		} 
	default:
		dts = func() uint32 {
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
			// do not increase
			return d.prevDTS + 1
		}()
	}

	d.prevPrevPTS = d.prevPTS
	d.prevPTS = pts
	d.prevDTS = dts

	return
}
