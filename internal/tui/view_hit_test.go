package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/lkshrk/omni/internal/app"
)

func namedTools(n int) []*app.ToolView {
	out := make([]*app.ToolView, n)
	for i := range out {
		out[i] = &app.ToolView{Name: fmt.Sprintf("tool-%02d", i), Provider: "brew"}
	}
	return out
}

func installedNamedTools(n int) []*app.ToolView {
	out := namedTools(n)
	for _, t := range out {
		t.Installed = true
		t.Tracked = true
	}
	return out
}

func clickListModel(tools []*app.ToolView) Model {
	m := baseModel(tools)
	m.width = 100
	m.height = 24
	m.applyFilter()
	return m
}

func frameLines(m Model) []string {
	return strings.Split(stripANSIEscapeSequences(m.View().Content), "\n")
}

func frameLineOf(t *testing.T, m Model, want string) int {
	t.Helper()
	lines := frameLines(m)
	found := -1
	for y, line := range lines {
		if strings.Contains(line, want) {
			if found >= 0 {
				t.Fatalf("%q appears on both y=%d and y=%d; frame:\n%s", want, found, y, strings.Join(lines, "\n"))
			}
			found = y
		}
	}
	if found < 0 {
		t.Fatalf("%q not in frame:\n%s", want, strings.Join(lines, "\n"))
	}
	return found
}

func clickAt(m Model, y int) Model {
	return drive(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y})
}

func TestListRowClick_SelectsClickedRow(t *testing.T) {
	t.Parallel()
	m := clickListModel(namedTools(12))
	m.cursor = 5

	for _, idx := range []int{0, 2, 7, 11} {
		name := fmt.Sprintf("tool-%02d", idx)
		y := frameLineOf(t, m, name)
		got := clickAt(m, y)
		if got.cursor != idx {
			t.Errorf("click on %s at y=%d: cursor = %d, want %d", name, y, got.cursor, idx)
		}
		if got.mode != viewList {
			t.Errorf("click on %s: mode = %v, want viewList", name, got.mode)
		}
	}
}

func TestListRowClick_SelectedRowDetailLinesKeepRow(t *testing.T) {
	t.Parallel()
	m := clickListModel(namedTools(12))
	m.cursor = 5

	rowY := frameLineOf(t, m, "tool-05")
	detailY := frameLineOf(t, m, "no description available")
	hintY := frameLineOf(t, m, "i install")
	if detailY != rowY+1 || hintY != rowY+2 {
		t.Fatalf("selected block = row %d, detail %d, hint %d; want contiguous from %d", rowY, detailY, hintY, rowY)
	}

	for _, y := range []int{detailY, hintY} {
		got := clickAt(m, y)
		if got.cursor != 5 {
			t.Errorf("click on selected block line y=%d: cursor = %d, want 5", y, got.cursor)
		}
		if got.cursorHidden {
			t.Errorf("click on selected block line y=%d hid the cursor", y)
		}
	}
}

func TestListRowClick_MissesLeaveCursorUnchanged(t *testing.T) {
	t.Parallel()
	m := clickListModel(namedTools(12))
	m.cursor = 4

	ruleY := 1
	if line := frameLines(m)[ruleY]; !strings.HasPrefix(line, "───") {
		t.Fatalf("y=%d is %q, want the header rule", ruleY, line)
	}
	sectionY := frameLineOf(t, m, "Available")
	pastEndY := frameLineOf(t, m, "tool-11") + 1
	if line := frameLines(m)[pastEndY]; strings.TrimSpace(line) != "" {
		t.Fatalf("y=%d is %q, want a blank line past the list", pastEndY, line)
	}

	for _, y := range []int{ruleY, sectionY, pastEndY} {
		got := clickAt(m, y)
		if got.cursor != 4 {
			t.Errorf("click on non-row y=%d: cursor = %d, want 4", y, got.cursor)
		}
		if got.mode != viewList {
			t.Errorf("click on non-row y=%d: mode = %v, want viewList", y, got.mode)
		}
	}
}

func TestListRowClick_NeverInvokesRowAction(t *testing.T) {
	t.Parallel()
	missing := clickListModel(namedTools(12))
	missing.cursor = 5
	rowY := frameLineOf(t, missing, "tool-03")

	clicked, cmd := missing.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: rowY})
	got := clicked.(Model)
	if cmd != nil {
		t.Error("click emitted a command")
	}
	if got.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", got.cursor)
	}
	if got.rowOpKey != "" || got.statusMsg != "" || len(got.upgradingKeys) != 0 {
		t.Errorf("click started work: rowOpKey=%q status=%q upgrading=%d", got.rowOpKey, got.statusMsg, len(got.upgradingKeys))
	}
	if got.listConfirm.action != "" {
		t.Errorf("click armed confirmation %q", got.listConfirm.action)
	}

	installing, installCmd := missing.Update(pressRune('i'))
	inst := installing.(Model)
	if installCmd == nil || inst.rowOpKey == "" {
		t.Fatalf("i on the same row did not install: cmd=%v rowOpKey=%q", installCmd != nil, inst.rowOpKey)
	}

	installed := clickListModel(installedNamedTools(12))
	installed.cursor = 5
	armed, _ := installed.Update(pressRune('d'))
	armedModel := armed.(Model)
	if armedModel.listConfirm.action == "" {
		t.Fatal("d did not arm a confirmation, so the click comparison proves nothing")
	}

	deleteRowY := frameLineOf(t, installed, "tool-03")
	clickedInstalled, clickCmd := installed.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: deleteRowY})
	ci := clickedInstalled.(Model)
	if clickCmd != nil || ci.listConfirm.action != "" {
		t.Errorf("click on an installed row armed %q (cmd=%v)", ci.listConfirm.action, clickCmd != nil)
	}

	cleared := clickAt(armedModel, deleteRowY)
	if cleared.listConfirm.action != "" {
		t.Errorf("click left confirmation %q armed", cleared.listConfirm.action)
	}
}

func TestListRowClick_FollowsScrollWindow(t *testing.T) {
	t.Parallel()
	m := clickListModel(namedTools(40))
	m.cursor = 30

	lines := frameLines(m)
	topRowY := listChromeLines(m) + len(toolsSectionedTab(m).pinnedTop)
	if !strings.Contains(lines[topRowY], "tool-17") {
		t.Fatalf("top of scroll window y=%d is %q, want tool-17", topRowY, lines[topRowY])
	}

	for _, name := range []string{"tool-17", "tool-24", "tool-33"} {
		var idx int
		if _, err := fmt.Sscanf(name, "tool-%d", &idx); err != nil {
			t.Fatalf("Sscanf %q: %v", name, err)
		}
		y := frameLineOf(t, m, name)
		got := clickAt(m, y)
		if got.cursor != idx {
			t.Errorf("click on %s at y=%d: cursor = %d, want %d", name, y, got.cursor, idx)
		}
	}
}

func TestListRowClick_SearchModeChrome(t *testing.T) {
	t.Parallel()
	list := clickListModel(namedTools(12))
	list.cursor = 5

	search := drive(list, pressRune('/'))
	search.cursor = 5
	if search.mode != viewSearch {
		t.Fatalf("mode = %v, want viewSearch", search.mode)
	}

	listY := frameLineOf(t, list, "tool-02")
	searchY := frameLineOf(t, search, "tool-02")
	if searchY != listY+2 {
		t.Fatalf("tool-02 at y=%d in search, y=%d in list; want two lines lower for the input and its rule", searchY, listY)
	}

	got := clickAt(search, searchY)
	if got.cursor != 2 {
		t.Errorf("click at y=%d in search: cursor = %d, want 2", searchY, got.cursor)
	}
	if got.mode != viewSearch {
		t.Errorf("mode = %v, want viewSearch to survive the click", got.mode)
	}
}

func TestListRowClick_EmptyListIsNoOp(t *testing.T) {
	t.Parallel()
	m := clickListModel(nil)

	for y := 0; y < 24; y++ {
		probe := m
		if probe.handleListRowClick(5, y) {
			t.Errorf("handleListRowClick(5, %d) on an empty list reported a hit", y)
		}
		if got := clickAt(m, y); got.cursor != 0 {
			t.Errorf("click at y=%d: cursor = %d, want 0", y, got.cursor)
		}
	}
}

func TestListRowClick_PillBarLineIsNotARowHit(t *testing.T) {
	t.Parallel()
	m := clickListModel(namedTools(12))
	m.cursor = 5

	pillY := frameLineOf(t, m, "[all]")
	if m.handleListRowClick(5, pillY) {
		t.Fatalf("pill bar line y=%d resolved to a tool row", pillY)
	}

	tabY := 0
	if !strings.Contains(frameLines(m)[tabY], "Settings") {
		t.Fatalf("y=%d is not the tab bar", tabY)
	}
	if m.handleListRowClick(5, tabY) {
		t.Fatalf("tab bar line y=%d resolved to a tool row", tabY)
	}
}

func TestRowLineIndexRowAt(t *testing.T) {
	t.Parallel()
	idx := rowLineIndex{lines: []int{-1, 0, 1, 1, 2}, start: 1, end: 5, chrome: 3}

	for _, tc := range []struct {
		y    int
		want int
		ok   bool
	}{
		{y: -1},
		{y: 0},
		{y: 2},
		{y: 3, want: 0, ok: true},
		{y: 4, want: 1, ok: true},
		{y: 5, want: 1, ok: true},
		{y: 6, want: 2, ok: true},
		{y: 7},
		{y: 99},
	} {
		got, ok := idx.rowAt(tc.y)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("rowAt(%d) = %d,%v; want %d,%v", tc.y, got, ok, tc.want, tc.ok)
		}
	}

	unowned := rowLineIndex{lines: []int{-1, 0}, end: 2, chrome: 3}
	if _, ok := unowned.rowAt(3); ok {
		t.Error("rowAt resolved a line no row owns")
	}
	if _, ok := (rowLineIndex{}).rowAt(0); ok {
		t.Error("zero rowLineIndex resolved a row")
	}
}

// lines covers every row, including those scrolled past the bottom, so a click
// on the separator or hint bar below the list must not reach one.
func TestListRowClickBelowTheListSelectsNothing(t *testing.T) {
	t.Parallel()
	m := clickListModel(namedTools(40))
	m.height = 14
	frame := frameLines(m)
	lastRow := -1
	for y, line := range frame {
		if strings.Contains(line, "tool-") {
			lastRow = y
		}
	}
	if lastRow < 0 || lastRow+1 >= len(frame) {
		t.Fatalf("no rows rendered, or no lines below them: %d of %d", lastRow, len(frame))
	}
	for y := lastRow + 1; y < len(frame); y++ {
		if got := clickAt(m, y); got.cursor != m.cursor {
			t.Errorf("click at y=%d (%q) moved the cursor to %d", y, frame[y], got.cursor)
		}
	}
}
