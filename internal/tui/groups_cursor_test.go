package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/config"
)

func groupsCursorModel(hosts map[string]config.HostAssignment, active string, groups []string) Model {
	m := baseModel(nil)
	m.mode = viewGroups
	m.groupNames = groups
	if hosts != nil {
		m.hostInfo = &app.HostInfo{Active: active, Hosts: hosts}
	}
	return m
}

func groupsRowIndices(m Model, kind groupsRowKind) []int {
	var out []int
	for i, row := range groupsRows(m) {
		if row.kind == kind {
			out = append(out, i)
		}
	}
	return out
}

func TestGroupsCursor_WrapsAcrossSectionSeam(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}, "beta": {}}, "", []string{"work", "personal"})
	hostRows := groupsRowIndices(m, groupsRowHost)
	groupRows := groupsRowIndices(m, groupsRowGroup)
	if len(hostRows) != 2 || len(groupRows) < 2 {
		t.Fatalf("row layout = %d hosts / %d groups, want 2 hosts and at least 2 groups", len(hostRows), len(groupRows))
	}
	lastHost := hostRows[len(hostRows)-1]
	firstGroup := groupRows[0]

	m.selectGroupsHostRow(len(hostRows) - 1)
	if got := drive(m, pressRune('j')).groupsCursor; got != firstGroup {
		t.Errorf("j at last host: groupsCursor = %d, want %d (first group row)", got, firstGroup)
	}

	m.selectGroupsGroupRow(0)
	if got := drive(m, pressRune('k')).groupsCursor; got != lastHost {
		t.Errorf("k at first group: groupsCursor = %d, want %d (last host row)", got, lastHost)
	}
}

func TestGroupsCursor_WrapsAroundListEnds(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}, "beta": {}}, "", []string{"work", "personal"})
	last := len(groupsRows(m)) - 1

	m.groupsCursor = last
	down := drive(m, pressRune('j'))
	if down.groupsCursor != 0 {
		t.Errorf("j at last group: groupsCursor = %d, want 0", down.groupsCursor)
	}
	if down.assignmentSection() != 0 || down.hostCursor() != 0 {
		t.Errorf("after wrap: section = %d hostCursor = %d, want 0/0", down.assignmentSection(), down.hostCursor())
	}

	m.groupsCursor = 0
	up := drive(m, pressRune('k'))
	if up.groupsCursor != last {
		t.Errorf("k at first host: groupsCursor = %d, want %d", up.groupsCursor, last)
	}
	if up.assignmentSection() != 1 {
		t.Errorf("after wrap: section = %d, want 1", up.assignmentSection())
	}
}

// buildAllGroupNames always synthesizes this machine's group, so the group section is never empty.
func TestGroupsCursor_NoConfiguredGroupsLeavesMachineGroupRow(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}, "beta": {}}, "", nil)
	hostRows := groupsRowIndices(m, groupsRowHost)
	groupRows := groupsRowIndices(m, groupsRowGroup)
	if len(hostRows) != 2 || len(groupRows) != 1 {
		t.Fatalf("row layout = %d hosts / %d groups, want 2 hosts and 1 synthesized group", len(hostRows), len(groupRows))
	}

	m.selectGroupsHostRow(0)
	up := drive(m, pressRune('k'))
	if up.groupsCursor != groupRows[0] {
		t.Errorf("k at first host: groupsCursor = %d, want %d (machine group row)", up.groupsCursor, groupRows[0])
	}
	if up.assignmentSection() != 1 {
		t.Errorf("k at first host: section = %d, want 1", up.assignmentSection())
	}

	m.selectGroupsHostRow(len(hostRows) - 1)
	down := drive(m, pressRune('j'))
	if down.groupsCursor != groupRows[0] {
		t.Errorf("j at last host: groupsCursor = %d, want %d (machine group row)", down.groupsCursor, groupRows[0])
	}
}

func TestGroupsCursor_NoHostsKeepsSelectionInGroupSection(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(nil, "", []string{"work", "personal"})
	if got := len(groupsRowIndices(m, groupsRowHost)); got != 0 {
		t.Fatalf("host rows = %d, want 0", got)
	}
	total := len(groupsRows(m))
	if total < 2 {
		t.Fatalf("group rows = %d, want at least 2", total)
	}

	for step := 1; step <= total+1; step++ {
		m = drive(m, pressRune('j'))
		want := step % total
		if m.groupsCursor != want {
			t.Fatalf("after %d j presses: groupsCursor = %d, want %d", step, m.groupsCursor, want)
		}
		if m.assignmentSection() != 1 {
			t.Fatalf("after %d j presses: section = %d, want 1", step, m.assignmentSection())
		}
		if m.hostCursor() != 0 {
			t.Fatalf("after %d j presses: hostCursor = %d, want 0", step, m.hostCursor())
		}
		if got := m.selectedHostName(); got != "" {
			t.Fatalf("after %d j presses: selectedHostName = %q, want empty", step, got)
		}
	}

	m.groupsCursor = 0
	if got := drive(m, pressRune('k')).groupsCursor; got != total-1 {
		t.Errorf("k at first group: groupsCursor = %d, want %d", got, total-1)
	}
}

func TestGroupsCursor_SingleRowMovementIsNoOp(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(nil, "", nil)
	if got := len(groupsRows(m)); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}

	got := drive(m, pressRune('j'), pressRune('k'), pressDown(), pressUp(), pressRune('j'))
	if got.groupsCursor != 0 {
		t.Errorf("groupsCursor = %d, want 0", got.groupsCursor)
	}
	if got.assignmentSection() != 1 || got.hostCursor() != 0 || got.groupCursor() != 0 {
		t.Errorf("accessors = section %d host %d group %d, want 1/0/0",
			got.assignmentSection(), got.hostCursor(), got.groupCursor())
	}
}

func TestGroupsCursor_SingleHostSingleGroupAlternates(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}}, "", nil)
	if got := len(groupsRows(m)); got != 2 {
		t.Fatalf("rows = %d, want 2", got)
	}

	for i, msg := range []tea.Msg{pressRune('j'), pressRune('j'), pressRune('k'), pressRune('k')} {
		m = drive(m, msg)
		want := (i + 1) % 2
		if m.groupsCursor != want {
			t.Fatalf("move %d: groupsCursor = %d, want %d", i+1, m.groupsCursor, want)
		}
	}
}

func TestGroupsCursor_AccessorsAgreeWithSelectedRow(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}, "beta": {}, "gamma": {}}, "", []string{"work", "personal"})
	rows := groupsRows(m)
	if len(rows) < 5 {
		t.Fatalf("rows = %d, want at least 5", len(rows))
	}

	for i, row := range rows {
		m.groupsCursor = i
		selected, ok := m.selectedGroupsRow()
		if !ok || selected != row {
			t.Fatalf("cursor %d: selectedGroupsRow = %+v (ok=%v), want %+v", i, selected, ok, row)
		}
		switch row.kind {
		case groupsRowHost:
			if m.assignmentSection() != 0 {
				t.Errorf("cursor %d (host row): section = %d, want 0", i, m.assignmentSection())
			}
			if m.hostCursor() != row.index {
				t.Errorf("cursor %d: hostCursor = %d, want %d", i, m.hostCursor(), row.index)
			}
			if m.groupCursor() != 0 {
				t.Errorf("cursor %d: groupCursor = %d, want 0 on a host row", i, m.groupCursor())
			}
			if got := m.selectedHostName(); got != row.host {
				t.Errorf("cursor %d: selectedHostName = %q, want %q", i, got, row.host)
			}
		case groupsRowGroup:
			if m.assignmentSection() != 1 {
				t.Errorf("cursor %d (group row): section = %d, want 1", i, m.assignmentSection())
			}
			if m.groupCursor() != row.index {
				t.Errorf("cursor %d: groupCursor = %d, want %d", i, m.groupCursor(), row.index)
			}
			if m.hostCursor() != 0 {
				t.Errorf("cursor %d: hostCursor = %d, want 0 on a group row", i, m.hostCursor())
			}
			if got := m.selectedHostName(); got != "" {
				t.Errorf("cursor %d: selectedHostName = %q, want empty on a group row", i, got)
			}
		}
	}
}

func TestGroupsCursor_SelectedHostNameFollowsPrioritizedOrder(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}, "beta": {}, "gamma": {}}, "gamma", nil)

	for i, want := range []string{"gamma", "alpha", "beta"} {
		m.selectGroupsHostRow(i)
		if got := m.selectedHostName(); got != want {
			t.Errorf("host row %d: selectedHostName = %q, want %q", i, got, want)
		}
		if m.hostCursor() != i {
			t.Errorf("host row %d: hostCursor = %d", i, m.hostCursor())
		}
	}
}

func TestGroupsCursor_PlaceAndFocusByName(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}, "beta": {}}, "", []string{"work", "personal"})

	m.placeHostCursor("beta")
	if got := m.selectedHostName(); got != "beta" {
		t.Errorf("placeHostCursor(beta): selectedHostName = %q", got)
	}

	m.placeGroupCursor("work")
	row, ok := m.selectedGroupsRow()
	if !ok || row.kind != groupsRowGroup || row.group != "work" {
		t.Errorf("placeGroupCursor(work): row = %+v (ok=%v)", row, ok)
	}

	before := m.groupsCursor
	m.placeHostCursor("nosuchhost")
	m.placeGroupCursor("nosuchgroup")
	if m.groupsCursor != before {
		t.Errorf("unknown names moved cursor to %d, want %d", m.groupsCursor, before)
	}

	m.focusGroupsHostSection()
	if m.assignmentSection() != 0 || m.hostCursor() != 0 {
		t.Errorf("focusGroupsHostSection: section = %d hostCursor = %d, want 0/0", m.assignmentSection(), m.hostCursor())
	}

	m.focusGroupsGroupSection()
	if m.assignmentSection() != 1 || m.groupCursor() != 0 {
		t.Errorf("focusGroupsGroupSection: section = %d groupCursor = %d, want 1/0", m.assignmentSection(), m.groupCursor())
	}
}

func TestGroupsCursor_ClampPullsCursorBackInRange(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}, "beta": {}}, "", []string{"work", "personal"})

	m.groupsCursor = 99
	m.clampGroupsCursor()
	if want := len(groupsRows(m)) - 1; m.groupsCursor != want {
		t.Errorf("groupsCursor = %d after clamp, want %d", m.groupsCursor, want)
	}

	m.groupsCursor = -3
	m.clampGroupsCursor()
	if m.groupsCursor != 0 {
		t.Errorf("groupsCursor = %d after clamping a negative index, want 0", m.groupsCursor)
	}

	m.groupsCursor = len(groupsRows(m)) - 1
	m.hostInfo = &app.HostInfo{Hosts: map[string]config.HostAssignment{"alpha": {}}}
	m.groupNames = nil
	m.clampGroupsCursor()
	if want := len(groupsRows(m)) - 1; m.groupsCursor != want {
		t.Errorf("groupsCursor = %d after the row list shrank, want %d", m.groupsCursor, want)
	}
	if _, ok := m.selectedGroupsRow(); !ok {
		t.Error("selectedGroupsRow not ok after clamp")
	}
}

func TestGroupsCursor_ResizeKeepsSelection(t *testing.T) {
	t.Parallel()
	m := groupsCursorModel(map[string]config.HostAssignment{"alpha": {}, "beta": {}}, "", []string{"work", "personal"})
	m.selectGroupsGroupRow(1)
	wantCursor := m.groupsCursor
	wantRow, _ := m.selectedGroupsRow()

	got := drive(m, tea.WindowSizeMsg{Width: 60, Height: 10})

	if got.height != 10 || got.width != 60 {
		t.Fatalf("size = %dx%d, want 60x10", got.width, got.height)
	}
	if got.groupsCursor != wantCursor {
		t.Errorf("groupsCursor = %d after resize, want %d", got.groupsCursor, wantCursor)
	}
	row, ok := got.selectedGroupsRow()
	if !ok || row != wantRow {
		t.Errorf("selected row = %+v (ok=%v) after resize, want %+v", row, ok, wantRow)
	}
	if got.assignmentSection() != 1 || got.groupCursor() != 1 {
		t.Errorf("accessors = section %d groupCursor %d after resize, want 1/1", got.assignmentSection(), got.groupCursor())
	}
}
