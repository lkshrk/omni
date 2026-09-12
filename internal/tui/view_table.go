package tui

import (
	"charm.land/lipgloss/v2"
)

// Spacing a tab may set for itself. Tabs differ on purpose: dots packs many
// narrow count columns and reads better tight, while tool and agent rows carry
// wider labels and need the room.
type tableLayout struct {
	iconWidth int
	iconGap   int
	columnGap int
	minGap    int
}

func toolsTableLayout() tableLayout {
	return tableLayout{
		iconWidth: listIconWidth,
		iconGap:   toolIconNameGapWidth,
		columnGap: listColumnGap,
		minGap:    listColumnGap,
	}
}

// Groups, settings and the dashboard lay a fixed label column against a
// right-aligned value and space it the same way. They size that label to a
// fixed width and let the middle fill what is left, so they share the row
// composition but not the measuring.
func groupsTableLayout() tableLayout {
	return labelValueTableLayout(groupsMinGap)
}

func settingsTableLayout() tableLayout {
	return labelValueTableLayout(settingsMinGap)
}

func statusTableLayout() tableLayout {
	return labelValueTableLayout(settingsMinGap)
}

func labelValueTableLayout(minGap int) tableLayout {
	return tableLayout{
		iconWidth: listIconWidth,
		iconGap:   listColumnGap,
		columnGap: listColumnGap,
		minGap:    minGap,
	}
}

func dotsTableLayout() tableLayout {
	return tableLayout{
		iconWidth: dotsIconW,
		iconGap:   dotsIconNameGapW,
		columnGap: dotsGapW,
		minGap:    dotsGapW,
	}
}

// Agents sets the icon apart by a full column gap rather than the single space
// tool rows use.
func agentsTableLayout() tableLayout {
	return tableLayout{
		iconWidth: listIconWidth,
		iconGap:   listColumnGap,
		columnGap: listColumnGap,
		minGap:    listColumnGap,
	}
}

// seed is the column's floor before any content is measured; cap 0 leaves it
// unbounded.
type tableColumn struct {
	key   string
	seed  int
	cap   int
	align rowCellAlign
}

// One rung of a tab's shrink ladder. Tabs order their own rungs: which column
// should suffer first is a property of what that tab's rows mean.
type tableShrinkStep struct {
	key   string
	floor int
}

func columnsByKey(cols []tableColumn) map[string]tableColumn {
	out := make(map[string]tableColumn, len(cols))
	for _, col := range cols {
		out[col.key] = col
	}
	return out
}

type tableWidths map[string]int

// Widens every column to its widest cell, then applies each column's cap.
func measureTableColumns(cols []tableColumn, rows int, value func(row int, key string) string) tableWidths {
	widths := make(tableWidths, len(cols))
	for _, col := range cols {
		widths[col.key] = col.seed
	}
	for i := 0; i < rows; i++ {
		for _, col := range cols {
			if w := lipgloss.Width(value(i, col.key)); w > widths[col.key] {
				widths[col.key] = w
			}
		}
	}
	for _, col := range cols {
		if col.cap > 0 && widths[col.key] > col.cap {
			widths[col.key] = col.cap
		}
	}
	return widths
}

// Runs the shared ladder, giving each rung up to its floor until the row fits.
func (w tableWidths) fit(over int, ladder ...tableShrinkStep) {
	if over <= 0 || len(ladder) == 0 {
		return
	}
	held := make(map[string]*int, len(w))
	for key, width := range w {
		value := width
		held[key] = &value
	}
	steps := make([]colShrinkStep, 0, len(ladder))
	for _, rung := range ladder {
		if ptr, ok := held[rung.key]; ok {
			steps = append(steps, colStep(ptr, rung.floor))
		}
	}
	shrinkColumns(over, steps...)
	for key, ptr := range held {
		w[key] = *ptr
	}
}

func (w tableWidths) cell(col tableColumn, text string, style lipgloss.Style) rowCell {
	width := w[col.key]
	if width <= 0 {
		return rowCell{}
	}
	rendered := style.Render(fitCellText(text, width))
	if col.align == rowCellAlignRight {
		return rightCell(rendered, width)
	}
	return leftCell(rendered, width)
}

// Composes one table row: the icon and name as the left group joined by
// iconGap, the remaining columns right-aligned and joined by columnGap. The
// caller supplies the selection prefix, which differs by tab.
func renderTableRowBody(l tableLayout, left, right []rowCell, totalWidth int) string {
	leftText := renderCellGroup(left, l.iconGap)
	rightText := renderCellGroup(right, l.columnGap)
	switch {
	case rightText == "":
		return leftText
	case leftText == "":
		return rightText
	}
	return alignLR(leftText, rightText, totalWidth, l.minGap)
}

func shrinkStep(key string, floor int) tableShrinkStep {
	return tableShrinkStep{key: key, floor: floor}
}
