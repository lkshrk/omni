package tui

import "strings"

type sectionedTab struct {
	leadingBlank bool
	top          []string
	// pinnedTop and footer render outside the scroll window, so a long row list cannot push them off screen.
	pinnedTop []string
	footer    []string
	sections  []sectionedTabSection
}

type sectionedTabSection struct {
	title            string
	danger           bool
	blankAfterHeader bool
	rows             []sectionedTabRow
	empty            []string
}

type sectionedTabRow struct {
	selected bool
	// When set, the row line is produced lazily so rows outside the scroll
	// window never pay for styling; View runs on every spinner tick. A
	// deferred line must render as exactly one line, since its height is
	// needed before it is produced.
	render func() string
	// Cursor index this row occupies in its tab's navigable set, or -1 when
	// the row is not selectable.
	index   int
	line    string
	details []string
}

func renderSectionedTab(m Model, tab sectionedTab) string {
	out, _ := renderSectionedTabIndexed(m, tab)
	return out
}

// Returns the frame alongside a map from rendered line to owning row, built by
// the same pass that writes the lines so the two cannot disagree.
func renderSectionedTabIndexed(m Model, tab sectionedTab) (string, rowLineIndex) {
	windowStart, windowEnd := sectionedTabWindow(m, tab)
	var buf scrollBuf
	var owners []int
	own := func(index int) {
		for len(owners) < buf.lineCount {
			owners = append(owners, index)
		}
	}
	// A badge arriving from an async result can overrun the last known width, and an over-wide line soft-wraps and desynchronises bubbletea's frame diff, so clip every line on the way out.
	write := func(s string) {
		buf.write(clipLines(s, m.width))
		own(-1)
	}
	pinned := len(tab.pinnedTop) > 0 || len(tab.footer) > 0
	sections := newListSectionWriter(m.palette, m.width, write)

	if tab.leadingBlank {
		write("\n")
	}
	for _, line := range tab.top {
		write(line + "\n")
	}
	for _, section := range tab.sections {
		if section.title == "" && !section.danger {
			sections.wroteSection = true
		} else if section.danger {
			if sections.wroteSection {
				write("\n")
			}
			write(renderSectionHeaderDanger(m.palette, section.title, m.width) + "\n")
			sections.wroteSection = true
		} else {
			sections.Header(section.title)
		}
		if section.blankAfterHeader {
			write("\n")
		}
		if len(section.rows) == 0 {
			for _, line := range section.empty {
				write(line + "\n")
			}
			continue
		}
		for _, row := range section.rows {
			if row.selected {
				buf.markCursor()
			}
			rowStart := buf.lineCount
			write(rowLineText(row, rowStart, windowStart, windowEnd) + "\n")
			for _, detail := range row.details {
				if strings.TrimSpace(detail) == "" {
					write("\n")
				} else {
					write(detail + "\n")
				}
			}
			for i := rowStart; i < buf.lineCount && i < len(owners); i++ {
				owners[i] = row.index
			}
			if row.selected && len(row.details) > 0 {
				buf.markCursorEnd()
			}
		}
	}
	if !pinned {
		avail := sectionedTabViewport(m, tab)
		start, end := scrollWindowBounds(buf.lineCount, buf.cursorLine, avail)
		return buf.render(avail), rowLineIndex{lines: owners, start: start, end: end, chrome: listChromeLines(m)}
	}
	var sb strings.Builder
	for _, line := range tab.pinnedTop {
		sb.WriteString(clipLines(line, m.width) + "\n")
	}
	avail := sectionedTabViewport(m, tab)
	start, end := scrollWindowBounds(buf.lineCount, buf.cursorLine, avail)
	sb.WriteString(buf.render(avail))
	if len(tab.footer) > 0 {
		sb.WriteString("\n")
		for _, line := range tab.footer {
			sb.WriteString(clipLines(line, m.width) + "\n")
		}
	}
	return sb.String(), rowLineIndex{lines: owners, start: start, end: end, chrome: listChromeLines(m) + len(tab.pinnedTop)}
}

func renderFixedGroupListRow(p palette, selected bool, first, rest []rowCell, firstGap, columnGap int) string {
	return listRowPrefix(p, selected) + renderFixedGroupRow(first, rest, firstGap, columnGap)
}

// A row costs one line plus its detail block, so the scroll window can be
// found before any row line is rendered.
func sectionedTabWindow(m Model, tab sectionedTab) (int, int) {
	lines, cursorLine := sectionedTabHeights(tab)
	return sectionedTabBounds(m, tab, lines, cursorLine)
}

// Renders every line regardless of the scroll window, so a test can compare
// the height model against what the writer actually emits.
func renderSectionedTabUnwindowed(m Model, tab sectionedTab) string {
	tall := m
	tall.height = 1 << 16
	out, _ := renderSectionedTabIndexed(tall, tab)
	return out
}

func sectionedTabModelLineCount(tab sectionedTab) int {
	lines, _ := sectionedTabHeights(tab)
	return lines
}

// A written string may itself span several lines, so heights are counted the
// way the writer counts them rather than assuming one line per element.
func writtenLineCount(text string) int {
	return strings.Count(text, "\n") + 1
}

func writtenLineCountOf(texts []string) int {
	total := 0
	for _, text := range texts {
		total += writtenLineCount(text)
	}
	return total
}

func sectionedTabHeights(tab sectionedTab) (int, int) {
	lines := 0
	cursorLine := 0
	if tab.leadingBlank {
		lines++
	}
	lines += writtenLineCountOf(tab.top)
	wroteSection := false
	for _, section := range tab.sections {
		if section.title != "" || section.danger {
			if wroteSection {
				lines++
			}
			lines++
			wroteSection = true
		} else {
			wroteSection = true
		}
		if section.blankAfterHeader {
			lines++
		}
		if len(section.rows) == 0 {
			lines += writtenLineCountOf(section.empty)
			continue
		}
		for _, row := range section.rows {
			if row.selected {
				cursorLine = lines
			}
			lines += writtenLineCount(row.line) + writtenLineCountOf(row.details)
			if row.selected && len(row.details) > 0 {
				cursorLine = lines - 1
			}
		}
	}
	return lines, cursorLine
}

func sectionedTabBounds(m Model, tab sectionedTab, lines, cursorLine int) (int, int) {
	return scrollWindowBounds(lines, cursorLine, sectionedTabViewport(m, tab))
}

// Lines a tab can actually show: the body less whatever it pins above the list
// and reserves for a footer. Page movement spends this as a row count, so a
// page overshoots by however many of those lines were headers, separators or
// the selected row's detail block. Exact paging is not reachable anyway, since
// the detail block travels with the cursor.
func sectionedTabViewport(m Model, tab sectionedTab) int {
	avail := listAvailableHeight(m) - len(tab.pinnedTop)
	if len(tab.footer) > 0 {
		avail -= len(tab.footer) + 1
	}
	return max(avail, 1)
}

func rowLineText(row sectionedTabRow, start, windowStart, windowEnd int) string {
	if row.render == nil {
		return row.line
	}
	if start < windowStart || start >= windowEnd {
		return ""
	}
	return row.render()
}
