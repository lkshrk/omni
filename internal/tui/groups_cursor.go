package tui

import "github.com/lkshrk/omni/internal/app"

type groupsRowKind int

const (
	groupsRowHost groupsRowKind = iota
	groupsRowGroup
)

type groupsRow struct {
	kind  groupsRowKind
	index int
	host  string
	group string
}

// Hosts then groups, in the same order renderGroups paints them, so a cursor
// index always names the row the user sees highlighted.
func groupsRows(m Model) []groupsRow {
	hosts := app.PrioritizedHostSummaries(m.hostInfo)
	names := buildAllGroupNames(m.groupNames)
	rows := make([]groupsRow, 0, len(hosts)+len(names))
	for i, host := range hosts {
		rows = append(rows, groupsRow{kind: groupsRowHost, index: i, host: host.Name})
	}
	for i, name := range names {
		rows = append(rows, groupsRow{kind: groupsRowGroup, index: i, group: name})
	}
	return rows
}

func (m Model) selectedGroupsRow() (groupsRow, bool) {
	rows := groupsRows(m)
	if len(rows) == 0 {
		return groupsRow{}, false
	}
	return rows[clampIndex(m.groupsCursor, len(rows))], true
}

func (m Model) assignmentSection() int {
	if row, ok := m.selectedGroupsRow(); ok && row.kind == groupsRowGroup {
		return 1
	}
	return 0
}

func (m Model) hostCursor() int {
	if row, ok := m.selectedGroupsRow(); ok && row.kind == groupsRowHost {
		return row.index
	}
	return 0
}

func (m Model) groupCursor() int {
	if row, ok := m.selectedGroupsRow(); ok && row.kind == groupsRowGroup {
		return row.index
	}
	return 0
}

func (m Model) groupsNav() listNav {
	return newListNav(m.groupsCursor, len(groupsRows(m)), sectionedTabViewport(m, groupsSectionedTab(m)))
}

func (m *Model) clampGroupsCursor() {
	m.groupsCursor = clampIndex(m.groupsCursor, len(groupsRows(*m)))
}

func (m *Model) placeHostCursor(host string) {
	for i, row := range groupsRows(*m) {
		if row.kind == groupsRowHost && row.host == host {
			m.groupsCursor = i
			return
		}
	}
}

func (m *Model) placeGroupCursor(group string) {
	for i, row := range groupsRows(*m) {
		if row.kind == groupsRowGroup && row.group == group {
			m.groupsCursor = i
			return
		}
	}
}

func (m *Model) focusGroupsHostSection() {
	for i, row := range groupsRows(*m) {
		if row.kind == groupsRowHost {
			m.groupsCursor = i
			return
		}
	}
}

func (m *Model) focusGroupsGroupSection() {
	for i, row := range groupsRows(*m) {
		if row.kind == groupsRowGroup {
			m.groupsCursor = i
			return
		}
	}
}

func (m *Model) selectGroupsHostRow(index int) {
	for i, row := range groupsRows(*m) {
		if row.kind == groupsRowHost && row.index == index {
			m.groupsCursor = i
			return
		}
	}
}

func (m *Model) selectGroupsGroupRow(index int) {
	for i, row := range groupsRows(*m) {
		if row.kind == groupsRowGroup && row.index == index {
			m.groupsCursor = i
			return
		}
	}
}
