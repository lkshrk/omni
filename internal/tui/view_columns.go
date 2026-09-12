package tui

type colShrinkStep struct {
	width *int
	floor int
}

func colStep(width *int, floor int) colShrinkStep {
	return colShrinkStep{width: width, floor: floor}
}

// Gives up width one step at a time, in the caller's priority order, until the
// row fits. A column may appear more than once to surrender its comfortable
// width early and the rest only once every other column has given what it can.
func shrinkColumns(over int, steps ...colShrinkStep) {
	if over <= 0 {
		return
	}
	for _, step := range steps {
		shrinkWidth(step.width, step.floor, &over)
		if over <= 0 {
			return
		}
	}
}

func shrinkWidth(width *int, minWidth int, over *int) {
	if width == nil || over == nil || *over <= 0 || *width <= minWidth {
		return
	}
	delta := min(*width-minWidth, *over)
	*width -= delta
	*over -= delta
}
