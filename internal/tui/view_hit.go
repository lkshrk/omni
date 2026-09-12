package tui

// rowLineIndex maps a screen row back to the list row rendered there. lines is
// indexed by buffer line and holds the row's cursor index, or -1 for a line no
// row owns; start is the first buffer line the scroll window shows; chrome is
// the number of screen rows drawn above that window.
type rowLineIndex struct {
	lines  []int
	start  int
	end    int
	chrome int
}

func (idx rowLineIndex) rowAt(y int) (int, bool) {
	offset := y - idx.chrome
	if offset < 0 {
		return 0, false
	}
	line := idx.start + offset
	// end bounds the click to what the window actually drew; lines holds every
	// row, including those scrolled past the bottom of the viewport.
	if line < 0 || line >= idx.end || line >= len(idx.lines) || idx.lines[line] < 0 {
		return 0, false
	}
	return idx.lines[line], true
}

func listChromeLines(m Model) int {
	return frameTopLines(m)
}

func toolRowLineIndex(m Model) rowLineIndex {
	_, idx := renderSectionedTabIndexed(m, toolsSectionedTab(m))
	return idx
}

// An overlay owns the screen while it is open, so a click underneath it must
// reach neither a row nor the tab bar. The loading twins count: they draw the
// same popup frame before their content arrives.
func (m Model) overlayOpen() bool {
	switch {
	case m.hostRequired, m.showFilePicker, m.stowInstallPrompt, m.help.ShowAll, m.setupReloading:
		return true
	case m.traceLog != nil, m.traceLogLoading, m.dotsPeek != nil, m.dotsPeekLoading:
		return true
	case m.dashboardReconcilePlanOpen, m.hostRenameMode, m.groupCreating, m.groupRenameMode, m.groupDeleteConfirm:
		return true
	case m.hostCreateStep != 0, m.hostEditMode != 0, m.editingPriority, m.editingServiceDuration, m.dangerConfirmRow >= 0:
		return true
	}
	return false
}

func (m Model) rowClicksEnabled() bool {
	return !m.overlayOpen()
}

func (m *Model) handleListRowClick(x, y int) bool {
	if m.mode != viewList && m.mode != viewSearch {
		return false
	}
	if !m.rowClicksEnabled() {
		return false
	}
	if x < 0 || len(m.visibleTools) == 0 {
		return false
	}
	idx, ok := toolRowLineIndex(*m).rowAt(y)
	if !ok || idx >= len(m.visibleTools) {
		return false
	}
	m.setToolsCursor(idx)
	m.cursorHidden = false
	m.clearListConfirmation()
	return true
}

func (m *Model) handleSectionedRowClick(y int) bool {
	if !m.rowClicksEnabled() {
		return false
	}
	var idx rowLineIndex
	switch m.mode {
	case viewStatus:
		_, idx = renderSectionedTabIndexed(*m, statusSectionedTab(*m))
	case viewGroups:
		_, idx = renderSectionedTabIndexed(*m, groupsSectionedTab(*m))
	case viewSkills:
		_, idx = renderSectionedTabIndexed(*m, m.agentsSectionedTab())
	case viewDots:
		_, idx = renderDotsIndexed(*m)
	default:
		return false
	}
	row, ok := idx.rowAt(y)
	if !ok {
		return false
	}
	switch m.mode {
	case viewStatus:
		m.statusCursor = clampIndex(row, len(statusRows(*m)))
	case viewGroups:
		m.groupsCursor = clampIndex(row, len(groupsRows(*m)))
	case viewSkills:
		m.agentsCursor = clampIndex(row, m.agentsRowCount())
		m.agentsConfirmIdx = -1
	case viewDots:
		visible := dotsVisibleRows(*m)
		m.clearDotsConfirmState()
		m.setDotsCursor(row, visible)
	}
	m.cursorHidden = false
	return true
}
