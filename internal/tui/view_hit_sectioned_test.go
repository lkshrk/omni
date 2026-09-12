package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/config"
)

func sectionedHit(m Model, y int) bool {
	probe := m
	return probe.handleSectionedRowClick(y)
}

func assertBlankFrameLine(t *testing.T, m Model, y int) {
	t.Helper()
	lines := frameLines(m)
	if y >= len(lines) || strings.TrimSpace(lines[y]) != "" {
		t.Fatalf("y=%d is not the blank line past the last row; frame:\n%s", y, strings.Join(lines, "\n"))
	}
}

func statusClickModel() Model {
	m := baseModel(manyTools(4))
	m.width, m.height = 100, 24
	m.mode = viewStatus
	return m
}

func statusRowIndexOf(t *testing.T, m Model, section, label string) int {
	t.Helper()
	for i, row := range statusRows(m) {
		if row.section == section && row.label == label {
			return i
		}
	}
	t.Fatalf("no %s row labelled %q", section, label)
	return -1
}

func TestStatusRowClick_SelectsClickedRow(t *testing.T) {
	t.Parallel()
	m := statusClickModel()
	m.statusCursor = 4

	for _, tc := range []struct{ text, section, label string }{
		{"Tool Updates", statusSectionAttention, "Tool Updates"},
		{"Agent Updates", statusSectionAttention, "Agent Updates"},
		{"Doctor", statusSectionAttention, "Doctor"},
		{"0 installed · 4 available", statusSectionOverview, "Tools"},
		{"reminder off/watch off", statusSectionOverview, "Services"},
	} {
		want := statusRowIndexOf(t, m, tc.section, tc.label)
		y := frameLineOf(t, m, tc.text)
		got := clickAt(m, y)
		if got.statusCursor != want {
			t.Errorf("click on %s/%s at y=%d: statusCursor = %d, want %d", tc.section, tc.label, y, got.statusCursor, want)
		}
		if got.mode != viewStatus {
			t.Errorf("click on %s/%s: mode = %v, want viewStatus", tc.section, tc.label, got.mode)
		}
		if got.cursorHidden {
			t.Errorf("click on %s/%s hid the cursor", tc.section, tc.label)
		}
	}
}

func TestStatusRowClick_SelectedRowDetailLineKeepsRow(t *testing.T) {
	t.Parallel()
	m := statusClickModel()
	m.statusCursor = statusRowIndexOf(t, m, statusSectionAttention, "Doctor")

	rowY := frameLineOf(t, m, "Doctor")
	detailY := frameLineOf(t, m, "Cause: Refresh dashboard to run checks.")
	if detailY != rowY+1 {
		t.Fatalf("detail line at y=%d, want %d", detailY, rowY+1)
	}
	if !sectionedHit(m, detailY) {
		t.Fatalf("detail line y=%d did not resolve to a row", detailY)
	}
	if got := clickAt(m, detailY).statusCursor; got != m.statusCursor {
		t.Errorf("click on the detail line: statusCursor = %d, want %d", got, m.statusCursor)
	}
}

func TestStatusRowClick_MissesLeaveCursorUnchanged(t *testing.T) {
	t.Parallel()
	m := statusClickModel()
	m.statusCursor = 4

	pastEndY := frameLineOf(t, m, "reminder off/watch off") + 1
	assertBlankFrameLine(t, m, pastEndY)

	for _, y := range []int{
		1,
		frameLineOf(t, m, statusSectionAttention),
		frameLineOf(t, m, statusSectionOverview+" ─"),
		pastEndY,
	} {
		if sectionedHit(m, y) {
			t.Errorf("non-row y=%d resolved to a status row", y)
		}
		got := clickAt(m, y)
		if got.statusCursor != 4 {
			t.Errorf("click on non-row y=%d: statusCursor = %d, want 4", y, got.statusCursor)
		}
		if got.mode != viewStatus {
			t.Errorf("click on non-row y=%d: mode = %v, want viewStatus", y, got.mode)
		}
	}
	if sectionedHit(m, 0) {
		t.Error("tab bar line resolved to a status row")
	}
}

func TestStatusRowClick_NeverInvokesRowAction(t *testing.T) {
	t.Parallel()
	m := statusClickModel()
	m.statusCursor = 4

	y := frameLineOf(t, m, "Doctor")
	clicked, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y})
	got := clicked.(Model)
	if cmd != nil {
		t.Error("click emitted a command")
	}
	if got.statusCursor != statusRowIndexOf(t, m, statusSectionAttention, "Doctor") {
		t.Fatalf("statusCursor = %d, want the Doctor row", got.statusCursor)
	}
	if got.statusMsg != "" || got.loading {
		t.Errorf("click started work: status=%q loading=%v", got.statusMsg, got.loading)
	}
}

func groupsClickModel() Model {
	m := baseModel(nil)
	m.width, m.height = 100, 24
	m.mode = viewGroups
	m.groupNames = []string{"web-grp", "db-grp"}
	m.hostInfo = &app.HostInfo{
		Active: "alpha-host",
		Hosts:  map[string]config.HostAssignment{"alpha-host": {}, "beta-host": {}, "gamma-host": {}},
	}
	return m
}

func groupsRowIndexOf(t *testing.T, m Model, kind groupsRowKind, name string) int {
	t.Helper()
	for i, row := range groupsRows(m) {
		if row.kind != kind {
			continue
		}
		if row.host == name || row.group == name {
			return i
		}
	}
	t.Fatalf("no groups row of kind %d named %q", kind, name)
	return -1
}

func TestGroupsRowClick_SelectsClickedRow(t *testing.T) {
	t.Parallel()
	m := groupsClickModel()
	m.groupsCursor = groupsRowIndexOf(t, m, groupsRowGroup, "db-grp")

	for _, tc := range []struct {
		name string
		kind groupsRowKind
	}{
		{"alpha-host", groupsRowHost},
		{"beta-host", groupsRowHost},
		{"gamma-host", groupsRowHost},
		{"web-grp", groupsRowGroup},
	} {
		want := groupsRowIndexOf(t, m, tc.kind, tc.name)
		y := frameLineOf(t, m, tc.name)
		got := clickAt(m, y)
		if got.groupsCursor != want {
			t.Errorf("click on %s at y=%d: groupsCursor = %d, want %d", tc.name, y, got.groupsCursor, want)
		}
		if got.mode != viewGroups {
			t.Errorf("click on %s: mode = %v, want viewGroups", tc.name, got.mode)
		}
	}
}

func TestGroupsRowClick_SelectedRowDetailLinesKeepRow(t *testing.T) {
	t.Parallel()
	m := groupsClickModel()
	m.groupsCursor = groupsRowIndexOf(t, m, groupsRowHost, "alpha-host")

	rowY := frameLineOf(t, m, "alpha-host")
	statsY := frameLineOf(t, m, "current host: 0 tools, 0 dotfiles")
	hintY := frameLineOf(t, m, "r rename • g edit groups")
	if statsY != rowY+1 || hintY != rowY+2 {
		t.Fatalf("selected block = row %d, stats %d, hint %d; want contiguous from %d", rowY, statsY, hintY, rowY)
	}

	for _, y := range []int{statsY, hintY} {
		if !sectionedHit(m, y) {
			t.Errorf("selected block line y=%d did not resolve to a row", y)
		}
		if got := clickAt(m, y).groupsCursor; got != m.groupsCursor {
			t.Errorf("click on selected block line y=%d: groupsCursor = %d, want %d", y, got, m.groupsCursor)
		}
	}
}

func TestGroupsRowClick_MissesLeaveCursorUnchanged(t *testing.T) {
	t.Parallel()
	m := groupsClickModel()
	start := groupsRowIndexOf(t, m, groupsRowGroup, "db-grp")
	m.groupsCursor = start

	pastEndY := frameLineOf(t, m, "web-grp") + 1
	assertBlankFrameLine(t, m, pastEndY)

	for _, y := range []int{
		1,
		frameLineOf(t, m, "Group Assignments"),
		frameLineOf(t, m, "Groups ─"),
		pastEndY,
	} {
		if sectionedHit(m, y) {
			t.Errorf("non-row y=%d resolved to a groups row", y)
		}
		got := clickAt(m, y)
		if got.groupsCursor != start {
			t.Errorf("click on non-row y=%d: groupsCursor = %d, want %d", y, got.groupsCursor, start)
		}
		if got.mode != viewGroups {
			t.Errorf("click on non-row y=%d: mode = %v, want viewGroups", y, got.mode)
		}
	}
	if sectionedHit(m, 0) {
		t.Error("tab bar line resolved to a groups row")
	}
}

func TestGroupsRowClick_CrossesSectionSeam(t *testing.T) {
	t.Parallel()
	m := groupsClickModel()
	m.groupsCursor = groupsRowIndexOf(t, m, groupsRowHost, "alpha-host")
	if m.assignmentSection() != 0 {
		t.Fatalf("assignmentSection = %d on a host row, want 0", m.assignmentSection())
	}

	wantGroup := groupsRowIndexOf(t, m, groupsRowGroup, "web-grp")
	onGroup := clickAt(m, frameLineOf(t, m, "web-grp"))
	if onGroup.groupsCursor != wantGroup {
		t.Fatalf("click on web-grp: groupsCursor = %d, want %d", onGroup.groupsCursor, wantGroup)
	}
	if onGroup.assignmentSection() != 1 {
		t.Errorf("assignmentSection = %d after clicking a group row, want 1", onGroup.assignmentSection())
	}
	if onGroup.groupCursor() != groupsRows(m)[wantGroup].index {
		t.Errorf("groupCursor = %d, want %d", onGroup.groupCursor(), groupsRows(m)[wantGroup].index)
	}

	wantHost := groupsRowIndexOf(t, onGroup, groupsRowHost, "beta-host")
	onHost := clickAt(onGroup, frameLineOf(t, onGroup, "beta-host"))
	if onHost.groupsCursor != wantHost {
		t.Fatalf("click on beta-host: groupsCursor = %d, want %d", onHost.groupsCursor, wantHost)
	}
	if onHost.assignmentSection() != 0 {
		t.Errorf("assignmentSection = %d after clicking back onto a host row, want 0", onHost.assignmentSection())
	}
	if onHost.hostCursor() != groupsRows(m)[wantHost].index {
		t.Errorf("hostCursor = %d, want %d", onHost.hostCursor(), groupsRows(m)[wantHost].index)
	}
}

func TestGroupsRowClick_NeverInvokesRowAction(t *testing.T) {
	t.Parallel()
	m := groupsClickModel()
	m.groupsCursor = groupsRowIndexOf(t, m, groupsRowHost, "alpha-host")

	armed := drive(m, pressRune('d'))
	if !armed.hostDeleteConfirm {
		t.Fatal("d did not arm the host delete confirmation, so the click comparison proves nothing")
	}

	y := frameLineOf(t, m, "gamma-host")
	clicked, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y})
	got := clicked.(Model)
	if cmd != nil {
		t.Error("click emitted a command")
	}
	if got.groupsCursor != groupsRowIndexOf(t, m, groupsRowHost, "gamma-host") {
		t.Fatalf("groupsCursor = %d, want the gamma-host row", got.groupsCursor)
	}
	if got.hostDeleteConfirm || got.hostRenameMode || got.groupDeleteConfirm || got.groupRenameMode {
		t.Errorf("click armed host edit state: delete=%v rename=%v groupDelete=%v groupRename=%v",
			got.hostDeleteConfirm, got.hostRenameMode, got.groupDeleteConfirm, got.groupRenameMode)
	}
	if got.hostEditMode != 0 || got.statusMsg != "" {
		t.Errorf("click started work: hostEditMode=%d status=%q", got.hostEditMode, got.statusMsg)
	}
}

func agentsClickModel(t *testing.T) Model {
	t.Helper()
	m := agentsRowsModel(t)
	m.width, m.height = 100, 26
	return m
}

func TestAgentsRowClick_SelectsClickedRow(t *testing.T) {
	t.Parallel()
	m := agentsClickModel(t)
	m.agentsCursor = 1

	for _, tc := range []struct {
		name string
		want int
	}{{"1.2.3", 0}, {"✗", 1}, {"0.9.0", 2}} {
		y := frameLineOf(t, m, tc.name)
		got := clickAt(m, y)
		if got.agentsCursor != tc.want {
			t.Errorf("click on %s at y=%d: agentsCursor = %d, want %d", tc.name, y, got.agentsCursor, tc.want)
		}
		if got.mode != viewSkills {
			t.Errorf("click on %s: mode = %v, want viewSkills", tc.name, got.mode)
		}
	}
}

func TestAgentsRowClick_SelectedRowDetailLinesKeepRow(t *testing.T) {
	t.Parallel()
	m := agentsClickModel(t)
	m.agentsCursor = 1

	rowY := frameLineOf(t, m, "✗")
	sourceY := frameLineOf(t, m, "source: acme/bravo")
	hintY := frameLineOf(t, m, "run S to sync it first")
	if sourceY <= rowY || hintY <= sourceY {
		t.Fatalf("selected block = row %d, source %d, hint %d; want both below the row", rowY, sourceY, hintY)
	}

	for _, y := range []int{sourceY, hintY} {
		if !sectionedHit(m, y) {
			t.Errorf("selected block line y=%d did not resolve to a row", y)
		}
		if got := clickAt(m, y).agentsCursor; got != 1 {
			t.Errorf("click on selected block line y=%d: agentsCursor = %d, want 1", y, got)
		}
	}
}

func TestAgentsRowClick_PinnedChromeIsNotARow(t *testing.T) {
	t.Parallel()
	m := agentsClickModel(t)
	m.agentsCursor = 1

	pastEndY := frameLineOf(t, m, "0.9.0") + 1
	assertBlankFrameLine(t, m, pastEndY)

	for _, y := range []int{
		1,
		frameLineOf(t, m, "~/.apm/apm.yml"),
		frameLineOf(t, m, "Packages ─"),
		pastEndY,
		frameLineOf(t, m, "1 installed"),
	} {
		if sectionedHit(m, y) {
			t.Errorf("non-row y=%d resolved to an agents row", y)
		}
		got := clickAt(m, y)
		if got.agentsCursor != 1 {
			t.Errorf("click on non-row y=%d: agentsCursor = %d, want 1", y, got.agentsCursor)
		}
		if got.mode != viewSkills {
			t.Errorf("click on non-row y=%d: mode = %v, want viewSkills", y, got.mode)
		}
	}
	if sectionedHit(m, 0) {
		t.Error("tab bar line resolved to an agents row")
	}
}

func TestAgentsRowClick_NeverInvokesRowAction(t *testing.T) {
	t.Parallel()
	m := agentsClickModel(t)
	m.agentsCursor = 0

	armed := drive(m, pressRune('d'))
	if armed.agentsConfirmIdx < 0 {
		t.Fatal("d did not arm an agents confirmation, so the click comparison proves nothing")
	}

	y := frameLineOf(t, m, "0.9.0")
	clicked, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y})
	got := clicked.(Model)
	if cmd != nil {
		t.Error("click emitted a command")
	}
	if got.agentsCursor != 2 {
		t.Fatalf("agentsCursor = %d, want 2", got.agentsCursor)
	}
	if got.agentsConfirmIdx != -1 || got.statusMsg != "" {
		t.Errorf("click started work: confirmIdx=%d status=%q", got.agentsConfirmIdx, got.statusMsg)
	}

	cleared := clickAt(armed, frameLineOf(t, armed, "0.9.0"))
	if cleared.agentsConfirmIdx != -1 {
		t.Errorf("click left confirmation armed at row %d", cleared.agentsConfirmIdx)
	}
}

func TestAgentsRowClick_EmptyTabIsNoOp(t *testing.T) {
	t.Parallel()
	m := agentsClickModel(t)
	m.agentsRows = nil
	if m.agentsRowCount() != 0 {
		t.Fatalf("agentsRowCount = %d, want 0", m.agentsRowCount())
	}

	for y := 1; y < m.height; y++ {
		if sectionedHit(m, y) {
			t.Errorf("y=%d resolved to a row on an empty agents tab", y)
		}
		if got := clickAt(m, y); got.agentsCursor != 0 || got.mode != viewSkills {
			t.Errorf("click at y=%d: agentsCursor = %d, mode = %v", y, got.agentsCursor, got.mode)
		}
	}
}

func dotsClickModel(rows int) Model {
	m := dotsNavModel(rows)
	m.width, m.height = 100, 24
	return m
}

func TestDotsRowClick_SelectsClickedRow(t *testing.T) {
	t.Parallel()
	m := dotsClickModel(6)
	m.dotsCursor = 2

	for _, tc := range []struct {
		name string
		want int
	}{{"dot00", 0}, {"dot02", 2}, {"dot04", 4}, {"dot05", 5}} {
		y := frameLineOf(t, m, tc.name)
		got := clickAt(m, y)
		if got.dotsCursor != tc.want {
			t.Errorf("click on %s at y=%d: dotsCursor = %d, want %d", tc.name, y, got.dotsCursor, tc.want)
		}
		if got.mode != viewDots {
			t.Errorf("click on %s: mode = %v, want viewDots", tc.name, got.mode)
		}
	}
}

func TestDotsRowClick_SelectedRowHintLineKeepsRow(t *testing.T) {
	t.Parallel()
	m := dotsClickModel(6)
	m.dotsCursor = 2

	rowY := frameLineOf(t, m, "dot02")
	hintY := frameLineOf(t, m, "space peek • v variant")
	if hintY != rowY+1 {
		t.Fatalf("hint line at y=%d, want %d", hintY, rowY+1)
	}
	if !sectionedHit(m, hintY) {
		t.Fatalf("hint line y=%d did not resolve to a row", hintY)
	}
	if got := clickAt(m, hintY).dotsCursor; got != 2 {
		t.Errorf("click on the hint line: dotsCursor = %d, want 2", got)
	}
}

func TestDotsRowClick_MissesLeaveCursorUnchanged(t *testing.T) {
	t.Parallel()
	m := dotsClickModel(6)
	m.dotsCursor = 2

	pastEndY := frameLineOf(t, m, "dot05") + 1
	assertBlankFrameLine(t, m, pastEndY)

	for _, y := range []int{
		1,
		frameLineOf(t, m, "/repo/dotfiles"),
		frameLineOf(t, m, "Synced ─"),
		pastEndY,
	} {
		if sectionedHit(m, y) {
			t.Errorf("non-row y=%d resolved to a dots row", y)
		}
		got := clickAt(m, y)
		if got.dotsCursor != 2 {
			t.Errorf("click on non-row y=%d: dotsCursor = %d, want 2", y, got.dotsCursor)
		}
		if got.mode != viewDots {
			t.Errorf("click on non-row y=%d: mode = %v, want viewDots", y, got.mode)
		}
	}
	if sectionedHit(m, 0) {
		t.Error("tab bar line resolved to a dots row")
	}
}

func TestDotsRowClick_NeverInvokesRowActionAndClearsConfirm(t *testing.T) {
	t.Parallel()
	m := dotsClickModel(6)
	m.dotsCursor = 2

	armed := drive(m, pressRune('d'))
	if armed.dotsConfirmIdx != 2 {
		t.Fatalf("d armed dotsConfirmIdx = %d, want 2; the click comparison proves nothing", armed.dotsConfirmIdx)
	}

	y := frameLineOf(t, m, "dot04")
	clicked, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y})
	got := clicked.(Model)
	if cmd != nil {
		t.Error("click emitted a command")
	}
	if got.dotsCursor != 4 {
		t.Fatalf("dotsCursor = %d, want 4", got.dotsCursor)
	}
	if got.dotsConfirmIdx != -1 || got.dotsOverwriteIdx != -1 || got.dotsLocalIdx != -1 || got.dotsIgnoreIdx != -1 || got.dotsVariantIdx != -1 {
		t.Errorf("click armed confirm state: confirm=%d overwrite=%d local=%d ignore=%d variant=%d",
			got.dotsConfirmIdx, got.dotsOverwriteIdx, got.dotsLocalIdx, got.dotsIgnoreIdx, got.dotsVariantIdx)
	}

	cleared := clickAt(armed, frameLineOf(t, armed, "dot00"))
	if cleared.dotsConfirmIdx != -1 || cleared.dotsVariantMode != dotsVariantNone {
		t.Errorf("click left confirm state armed: idx=%d mode=%v", cleared.dotsConfirmIdx, cleared.dotsVariantMode)
	}
	if cleared.dotsCursor != 0 {
		t.Errorf("dotsCursor = %d, want 0", cleared.dotsCursor)
	}
}

func TestDotsRowClick_SelectsExpandedChildRow(t *testing.T) {
	t.Parallel()
	m := dotsNavExpandedModel()
	m.width, m.height = 100, 24
	if got := len(dotsVisibleRows(m)); got != 5 {
		t.Fatalf("expanded visible rows = %d, want 5", got)
	}

	got := clickAt(m, frameLineOf(t, m, "two"))
	if got.dotsCursor != 3 {
		t.Errorf("click on the second child: dotsCursor = %d, want 3", got.dotsCursor)
	}
	if got.dotsExpandedName != "beta" {
		t.Errorf("click on a sibling child collapsed beta: dotsExpandedName = %q", got.dotsExpandedName)
	}
	rows := dotsVisibleRows(got)
	if row := rows[got.dotsCursor]; !row.isChild || row.child.Name != "two" {
		t.Errorf("cursor row = %+v, want the child named two", row)
	}
}

func TestDotsRowClick_ParentRowCollapsesExpandedEntry(t *testing.T) {
	t.Parallel()
	m := dotsNavExpandedModel()
	m.width, m.height = 100, 24

	got := clickAt(m, frameLineOf(t, m, "gamma"))
	if got.dotsExpandedName != "" {
		t.Errorf("dotsExpandedName = %q, want the expanded entry collapsed", got.dotsExpandedName)
	}
	rows := dotsVisibleRows(got)
	if len(rows) != 3 {
		t.Fatalf("collapsed visible rows = %d, want 3", len(rows))
	}
	if got.dotsCursor != 2 {
		t.Fatalf("dotsCursor = %d, want 2", got.dotsCursor)
	}
	if name := rows[got.dotsCursor].entry.Name; name != "gamma" {
		t.Errorf("cursor row = %q, want gamma", name)
	}
}

func TestDotsRowClick_EmptyTabIsNoOp(t *testing.T) {
	t.Parallel()
	m := dotsClickModel(0)

	for y := 1; y < m.height; y++ {
		if sectionedHit(m, y) {
			t.Errorf("y=%d resolved to a row on an empty dots tab", y)
		}
		if got := clickAt(m, y); got.dotsCursor != 0 || got.mode != viewDots {
			t.Errorf("click at y=%d: dotsCursor = %d, mode = %v", y, got.dotsCursor, got.mode)
		}
	}
}

func TestSectionedRowClick_IgnoresUnhandledModes(t *testing.T) {
	t.Parallel()
	m := statusClickModel()
	for _, mode := range []viewMode{viewList, viewSettings, viewGroupPicker, viewCommand} {
		probe := m
		probe.mode = mode
		for y := 0; y < probe.height; y++ {
			if probe.handleSectionedRowClick(y) {
				t.Fatalf("mode %v: y=%d resolved to a sectioned row", mode, y)
			}
		}
	}
}
