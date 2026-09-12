package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func wheelDown() tea.Msg { return tea.MouseWheelMsg{Button: tea.MouseWheelDown} }
func wheelUp() tea.Msg   { return tea.MouseWheelMsg{Button: tea.MouseWheelUp} }

type wheelParityDirection struct {
	name  string
	wheel tea.Msg
	key   tea.Msg
}

func wheelParityDirections() []wheelParityDirection {
	return []wheelParityDirection{
		{"down", wheelDown(), pressDown()},
		{"up", wheelUp(), pressUp()},
	}
}

type wheelParityTab struct {
	name      string
	newModel  func(*testing.T) Model
	rowCount  func(Model) int
	cursor    func(Model) int
	setCursor func(*Model, int)
}

func wheelParityTabs() []wheelParityTab {
	return []wheelParityTab{
		{
			name:      "tools",
			newModel:  func(*testing.T) Model { return baseModel(threeTools()) },
			rowCount:  func(m Model) int { return len(m.visibleTools) },
			cursor:    func(m Model) int { return m.cursor },
			setCursor: func(m *Model, i int) { m.cursor = i },
		},
		{
			name:      "dots",
			newModel:  func(*testing.T) Model { return dotsNavModel(20) },
			rowCount:  func(m Model) int { return len(dotsVisibleRows(m)) },
			cursor:    func(m Model) int { return m.dotsCursor },
			setCursor: func(m *Model, i int) { m.dotsCursor = i },
		},
		{
			name:      "agents",
			newModel:  func(t *testing.T) Model { return agentsSectionedModel(t) },
			rowCount:  func(m Model) int { return m.agentsRowCount() },
			cursor:    func(m Model) int { return m.agentsCursor },
			setCursor: func(m *Model, i int) { m.agentsCursor = i },
		},
		{
			name:      "status",
			newModel:  func(*testing.T) Model { return wheelParityStatusModel() },
			rowCount:  func(m Model) int { return len(statusRows(m)) },
			cursor:    func(m Model) int { return m.statusCursor },
			setCursor: func(m *Model, i int) { m.statusCursor = i },
		},
		{
			name:      "groups",
			newModel:  func(*testing.T) Model { return hostsModel() },
			rowCount:  func(m Model) int { return len(groupsRows(m)) },
			cursor:    func(m Model) int { return m.groupsCursor },
			setCursor: func(m *Model, i int) { m.groupsCursor = i },
		},
	}
}

func wheelParityStatusModel() Model {
	m := baseModel(threeTools())
	m.mode = viewStatus
	return m
}

func TestWheelStepMatchesKeyOnEveryListTab(t *testing.T) {
	t.Parallel()
	for _, tab := range wheelParityTabs() {
		t.Run(tab.name, func(t *testing.T) {
			t.Parallel()
			rows := tab.rowCount(tab.newModel(t))
			if rows < 3 {
				t.Fatalf("%s fixture has %d rows, want at least 3 so first, middle and last differ", tab.name, rows)
			}
			for _, start := range []int{0, rows / 2, rows - 1} {
				for _, dir := range wheelParityDirections() {
					wheeled := tab.newModel(t)
					tab.setCursor(&wheeled, start)
					keyed := tab.newModel(t)
					tab.setCursor(&keyed, start)
					byWheel := tab.cursor(drive(wheeled, dir.wheel))
					byKey := tab.cursor(drive(keyed, dir.key))
					if byWheel != byKey {
						t.Errorf("from row %d of %d, wheel %s = %d but key %s = %d", start, rows, dir.name, byWheel, dir.name, byKey)
					}
				}
			}
		})
	}
}

func TestWheelWrapsAtBothEndsOnEveryListTab(t *testing.T) {
	t.Parallel()
	for _, tab := range wheelParityTabs() {
		t.Run(tab.name, func(t *testing.T) {
			t.Parallel()
			last := tab.rowCount(tab.newModel(t)) - 1
			if last < 1 {
				t.Fatalf("%s fixture has %d rows, want at least 2", tab.name, last+1)
			}

			top := tab.newModel(t)
			tab.setCursor(&top, 0)
			if got := tab.cursor(drive(top, wheelUp())); got != last {
				t.Errorf("wheel up from the first row = %d, want %d (wrapped like k)", got, last)
			}

			bottom := tab.newModel(t)
			tab.setCursor(&bottom, last)
			if got := tab.cursor(drive(bottom, wheelDown())); got != 0 {
				t.Errorf("wheel down from the last row = %d, want 0 (wrapped like j)", got)
			}
		})
	}
}

func TestWheelMatchesKeyAcrossTheGroupsSectionSeam(t *testing.T) {
	t.Parallel()
	rows := groupsRows(hostsModel())
	lastHost := -1
	for i, row := range rows {
		if row.kind == groupsRowHost {
			lastHost = i
		}
	}
	if lastHost < 0 || lastHost == len(rows)-1 {
		t.Fatalf("groups fixture has no host/group seam: %d rows, last host at %d", len(rows), lastHost)
	}

	for _, start := range []int{lastHost, lastHost + 1} {
		for _, dir := range wheelParityDirections() {
			wheeled := hostsModel()
			wheeled.groupsCursor = start
			keyed := hostsModel()
			keyed.groupsCursor = start
			byWheel := drive(wheeled, dir.wheel)
			byKey := drive(keyed, dir.key)
			if byWheel.groupsCursor != byKey.groupsCursor {
				t.Errorf("from row %d, wheel %s = %d but key %s = %d", start, dir.name, byWheel.groupsCursor, dir.name, byKey.groupsCursor)
			}
			if byWheel.assignmentSection() != byKey.assignmentSection() {
				t.Errorf("from row %d, wheel %s selected section %d but key %s selected %d",
					start, dir.name, byWheel.assignmentSection(), dir.name, byKey.assignmentSection())
			}
		}
	}
}

func TestWheelMatchesKeyOnAnExpandedDotsEntry(t *testing.T) {
	t.Parallel()
	if got := len(dotsVisibleRows(dotsNavExpandedModel())); got != 5 {
		t.Fatalf("expanded visible rows = %d, want 5", got)
	}
	for start := 0; start < 5; start++ {
		for _, dir := range wheelParityDirections() {
			wheeled := dotsNavExpandedModel()
			wheeled.dotsCursor = start
			keyed := dotsNavExpandedModel()
			keyed.dotsCursor = start
			byWheel := drive(wheeled, dir.wheel)
			byKey := drive(keyed, dir.key)
			if byWheel.dotsCursor != byKey.dotsCursor {
				t.Errorf("from row %d, wheel %s left dotsCursor = %d but key %s left %d",
					start, dir.name, byWheel.dotsCursor, dir.name, byKey.dotsCursor)
			}
			if byWheel.dotsExpandedName != byKey.dotsExpandedName {
				t.Errorf("from row %d, wheel %s left dotsExpandedName = %q but key %s left %q",
					start, dir.name, byWheel.dotsExpandedName, dir.name, byKey.dotsExpandedName)
			}
		}
	}
}

func TestWheelUpFromTheFirstDashboardRowWrapsToTheLast(t *testing.T) {
	t.Parallel()
	m := wheelParityStatusModel()
	last := len(statusRows(m)) - 1
	if last < 1 {
		t.Fatalf("dashboard has %d rows, want at least 2", last+1)
	}
	if got := drive(m, wheelUp()).statusCursor; got != last {
		t.Fatalf("statusCursor after wheel up from row 0 = %d, want %d", got, last)
	}
}

func TestWheelOnShortListsIsANoOp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		newModel func(*testing.T) Model
		rowCount func(Model) int
		cursor   func(Model) int
		want     int
	}{
		{"tools/empty", func(*testing.T) Model { return baseModel(nil) }, func(m Model) int { return len(m.visibleTools) }, func(m Model) int { return m.cursor }, 0},
		{"tools/single", func(*testing.T) Model { return baseModel(threeTools()[:1]) }, func(m Model) int { return len(m.visibleTools) }, func(m Model) int { return m.cursor }, 1},
		{"dots/empty", func(*testing.T) Model { return dotsNavModel(0) }, func(m Model) int { return len(dotsVisibleRows(m)) }, func(m Model) int { return m.dotsCursor }, 0},
		{"dots/single", func(*testing.T) Model { return dotsNavModel(1) }, func(m Model) int { return len(dotsVisibleRows(m)) }, func(m Model) int { return m.dotsCursor }, 1},
		{"agents/empty", wheelParityEmptyAgentsModel, func(m Model) int { return m.agentsRowCount() }, func(m Model) int { return m.agentsCursor }, 0},
		{"agents/single", wheelParitySingleAgentsModel, func(m Model) int { return m.agentsRowCount() }, func(m Model) int { return m.agentsCursor }, 1},
		{"groups/single", wheelParitySingleGroupsModel, func(m Model) int { return len(groupsRows(m)) }, func(m Model) int { return m.groupsCursor }, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.rowCount(tt.newModel(t)); got != tt.want {
				t.Fatalf("fixture has %d rows, want %d", got, tt.want)
			}
			for _, dir := range wheelParityDirections() {
				if got := tt.cursor(drive(tt.newModel(t), dir.wheel)); got != 0 {
					t.Errorf("cursor after wheel %s = %d, want 0", dir.name, got)
				}
				if got := tt.cursor(drive(tt.newModel(t), dir.key)); got != 0 {
					t.Errorf("cursor after key %s = %d, want 0", dir.name, got)
				}
			}
		})
	}
}

func wheelParityEmptyAgentsModel(t *testing.T) Model {
	m := agentsRowsModel(t)
	m.agentsRows = nil
	return m
}

func wheelParitySingleAgentsModel(t *testing.T) Model {
	m := agentsRowsModel(t)
	m.agentsRows = m.agentsRows[:1]
	return m
}

func wheelParitySingleGroupsModel(*testing.T) Model {
	m := baseModel(nil)
	m.mode = viewGroups
	return m
}

func TestWheelClearsArmedDotsConfirmState(t *testing.T) {
	t.Parallel()
	states := map[string]func(*Model){
		"delete":    func(m *Model) { m.dotsConfirmIdx = 0 },
		"overwrite": func(m *Model) { m.dotsOverwriteIdx = 0 },
		"local":     func(m *Model) { m.dotsLocalIdx = 0 },
		"ignore":    func(m *Model) { m.dotsIgnoreIdx = 0 },
		"variant":   func(m *Model) { m.dotsVariantIdx = 0; m.dotsVariantMode = dotsVariantCreate },
	}
	for stateName, arm := range states {
		for _, dir := range wheelParityDirections() {
			t.Run(stateName+"/"+dir.name, func(t *testing.T) {
				t.Parallel()
				m := dotsNavModel(20)
				arm(&m)
				got := drive(m, dir.wheel)
				if got.dotsConfirmIdx != -1 || got.dotsOverwriteIdx != -1 || got.dotsLocalIdx != -1 ||
					got.dotsIgnoreIdx != -1 || got.dotsVariantIdx != -1 {
					t.Fatalf("confirm state still armed after the wheel: confirm=%d overwrite=%d local=%d ignore=%d variant=%d",
						got.dotsConfirmIdx, got.dotsOverwriteIdx, got.dotsLocalIdx, got.dotsIgnoreIdx, got.dotsVariantIdx)
				}
				if got.dotsVariantMode != dotsVariantNone {
					t.Fatalf("dotsVariantMode = %v, want dotsVariantNone", got.dotsVariantMode)
				}
			})
		}
	}
}
