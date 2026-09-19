package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/lkshrk/omni/internal/app"
)

var testTableColumns = []tableColumn{
	{key: "name", seed: 10, align: rowCellAlignLeft},
	{key: "detail", align: rowCellAlignRight},
	{key: "version", seed: 4, cap: 8, align: rowCellAlignRight},
}

func testTableValues(rows [][3]string) func(int, string) string {
	return func(i int, key string) string {
		switch key {
		case "name":
			return rows[i][0]
		case "detail":
			return rows[i][1]
		default:
			return rows[i][2]
		}
	}
}

func TestMeasureTableColumnsWidensToWidestCell(t *testing.T) {
	t.Parallel()
	rows := [][3]string{{"short", "a detail", "1.0"}, {"a much longer name", "d", "2.0"}}
	got := measureTableColumns(testTableColumns, len(rows), testTableValues(rows))
	if got["name"] != len("a much longer name") {
		t.Errorf("name = %d, want %d", got["name"], len("a much longer name"))
	}
	if got["detail"] != len("a detail") {
		t.Errorf("detail = %d, want %d", got["detail"], len("a detail"))
	}
}

func TestMeasureTableColumnsHoldsSeedWhenContentIsNarrower(t *testing.T) {
	t.Parallel()
	rows := [][3]string{{"ab", "", "1"}}
	got := measureTableColumns(testTableColumns, len(rows), testTableValues(rows))
	if got["name"] != 10 {
		t.Errorf("name = %d, want the seed 10", got["name"])
	}
	if got["detail"] != 0 {
		t.Errorf("detail = %d, want 0 for an unseeded empty column", got["detail"])
	}
}

func TestMeasureTableColumnsAppliesCap(t *testing.T) {
	t.Parallel()
	rows := [][3]string{{"n", "d", "1.2.3 -> 4.5.6.7.8"}}
	got := measureTableColumns(testTableColumns, len(rows), testTableValues(rows))
	if got["version"] != 8 {
		t.Errorf("version = %d, want the cap 8", got["version"])
	}
}

func TestMeasureTableColumnsWithNoRowsKeepsSeeds(t *testing.T) {
	t.Parallel()
	got := measureTableColumns(testTableColumns, 0, func(int, string) string { return "" })
	if got["name"] != 10 || got["version"] != 4 || got["detail"] != 0 {
		t.Errorf("widths = %v, want seeds only", got)
	}
}

func TestTableWidthsFitFollowsTheDeclaredLadder(t *testing.T) {
	t.Parallel()
	widths := tableWidths{"name": 20, "detail": 20}
	widths.fit(8, shrinkStep("detail", 15), shrinkStep("name", 10))
	if widths["detail"] != 15 || widths["name"] != 17 {
		t.Errorf("widths = %v, want detail 15 and name 17", widths)
	}
}

func TestTableWidthsFitIgnoresUnknownColumns(t *testing.T) {
	t.Parallel()
	widths := tableWidths{"name": 20}
	widths.fit(5, shrinkStep("absent", 1), shrinkStep("name", 10))
	if widths["name"] != 15 {
		t.Errorf("name = %d, want 15", widths["name"])
	}
}

func TestTableWidthsFitIsANoOpWhenTheRowAlreadyFits(t *testing.T) {
	t.Parallel()
	widths := tableWidths{"name": 20}
	widths.fit(0, shrinkStep("name", 1))
	widths.fit(-3, shrinkStep("name", 1))
	if widths["name"] != 20 {
		t.Errorf("name = %d, want 20 untouched", widths["name"])
	}
}

func TestTableWidthsCellRespectsAlignmentAndZeroWidth(t *testing.T) {
	t.Parallel()
	widths := tableWidths{"name": 6, "detail": 0}
	left := widths.cell(testTableColumns[0], "ab", defaultPalette().styleNormal)
	if left.width != 6 || left.align != rowCellAlignLeft {
		t.Errorf("left cell = %+v, want width 6 aligned left", left)
	}
	if collapsed := widths.cell(testTableColumns[1], "ignored", defaultPalette().styleNormal); collapsed.width != 0 || collapsed.text != "" {
		t.Errorf("zero-width column produced %+v, want an empty cell", collapsed)
	}
}

func TestRenderTableRowBodyUsesEachGap(t *testing.T) {
	t.Parallel()
	layout := tableLayout{iconWidth: 1, iconGap: 1, columnGap: 3, minGap: 3}
	left := []rowCell{leftCell("i", 1), leftCell("name", 4)}
	right := []rowCell{rightCell("a", 1), rightCell("b", 1)}
	got := renderTableRowBody(layout, left, right, 40)
	if !strings.HasPrefix(got, "i name") {
		t.Errorf("row = %q, want the icon one space from the name", got)
	}
	if !strings.HasSuffix(got, "a   b") {
		t.Errorf("row = %q, want the right columns three spaces apart", got)
	}
	if lineWidth := len(got); lineWidth != 40 {
		t.Errorf("row width = %d, want 40", lineWidth)
	}
}

func TestRenderTableRowBodyWithOneSideOnly(t *testing.T) {
	t.Parallel()
	layout := tableLayout{iconWidth: 1, iconGap: 1, columnGap: 3, minGap: 3}
	if got := renderTableRowBody(layout, []rowCell{leftCell("only", 4)}, nil, 40); got != "only" {
		t.Errorf("left-only row = %q, want it unpadded", got)
	}
	if got := renderTableRowBody(layout, nil, []rowCell{rightCell("only", 4)}, 40); got != "only" {
		t.Errorf("right-only row = %q, want it unpadded", got)
	}
}

func TestTableLayoutsDifferDeliberately(t *testing.T) {
	t.Parallel()
	if toolsTableLayout().iconGap == agentsTableLayout().iconGap {
		t.Error("tools and agents are expected to space the icon differently")
	}
	if dotsTableLayout().columnGap == toolsTableLayout().columnGap {
		t.Error("dots is expected to pack its columns tighter than tools")
	}
}

// Deferred rows must render exactly what eager rows would; a height model that
// drifts from the writer would blank a visible row instead.
func TestSectionedTabLazyRowsMatchEagerRows(t *testing.T) {
	t.Parallel()
	m := baseModel(manyTools(30))
	m.width, m.height = 100, 20
	m.mode = viewList
	m.applyFilter()

	for _, cursor := range []int{0, 7, 29} {
		m.cursor = cursor
		lazy := toolsSectionedTab(m)
		eager := toolsSectionedTab(m)
		for si := range eager.sections {
			for ri := range eager.sections[si].rows {
				row := &eager.sections[si].rows[ri]
				if row.render != nil {
					row.line = row.render()
					row.render = nil
				}
			}
		}
		if got, want := renderSectionedTab(m, lazy), renderSectionedTab(m, eager); got != want {
			t.Fatalf("cursor %d: deferred and eager rendering differ\ndeferred:\n%s\neager:\n%s", cursor, got, want)
		}
		// The frame also carries the pinned lines, which sit outside the
		// scrolling buffer the height model describes.
		modelled := sectionedTabModelLineCount(eager) + len(eager.pinnedTop)
		if actual := strings.Count(renderSectionedTabUnwindowed(m, eager), "\n"); modelled != actual {
			t.Errorf("cursor %d: height model says %d lines, writer produced %d", cursor, modelled, actual)
		}
	}
}

// Dots writes multi-line strings into single detail entries, so its height must
// be counted from the text rather than by assuming one line per entry.
func TestSectionedTabHeightModelCountsMultiLineEntries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		tab  func() sectionedTab
		m    func() Model
	}{
		{"dots", nil, func() Model { m := dotsNavModel(6); m.width, m.height = 100, 20; return m }},
		{"dots expanded", nil, func() Model { m := dotsNavExpandedModel(); m.width, m.height = 100, 20; return m }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.m()
			tab := dotsSectionedTab(m)
			modelled := sectionedTabModelLineCount(tab) + len(tab.pinnedTop)
			actual := strings.Count(renderSectionedTabUnwindowed(m, tab), "\n")
			if modelled != actual {
				t.Errorf("height model says %d lines, writer produced %d", modelled, actual)
			}
		})
	}
}

func TestSectionedTabHeightModelCountsAnEmbeddedNewline(t *testing.T) {
	t.Parallel()
	tab := sectionedTab{sections: []sectionedTabSection{{
		title: "S",
		rows:  []sectionedTabRow{{line: "row", details: []string{"first\nsecond\nthird"}}},
	}}}
	m := baseModel(nil)
	m.width, m.height = 80, 40
	modelled := sectionedTabModelLineCount(tab)
	if actual := strings.Count(renderSectionedTabUnwindowed(m, tab), "\n"); modelled != actual {
		t.Errorf("height model says %d lines, writer produced %d", modelled, actual)
	}
}

// A key the spec does not declare looks up as a zero column, losing its cap and
// alignment with nothing to show for it, so the keys the renderer asks for are
// pinned against the spec that defines them.
func TestColumnKeysUsedByRenderersAreDeclared(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cols []tableColumn
		used []string
	}{
		{"agents", agentsTableColumns, []string{"name", "attribute", "version", "targets"}},
		{"agents registry", agentsRegistryColumns, []string{"name", "targets"}},
		{"tools", toolsTableColumns, []string{"name", "prov", "ver", "group"}},
		{"dots", dotsTableColumnSpec, []string{"name", "status", "ratio", "ignore", "group"}},
		{"groups", groupsTableColumns, []string{"name", "mid", "tail"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			declared := columnsByKey(tc.cols)
			for _, key := range tc.used {
				if _, ok := declared[key]; !ok {
					t.Errorf("renderer asks for column %q, which the spec does not declare", key)
				}
			}
			if len(declared) != len(tc.cols) {
				t.Errorf("spec declares %d columns but %d distinct keys", len(tc.cols), len(declared))
			}
		})
	}
}

// Every shrink rung must name a column the spec declares, or it silently does
// nothing when the row needs to give up width.
func TestShrinkLaddersOnlyNameDeclaredColumns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		cols   []tableColumn
		ladder []tableShrinkStep
	}{
		{"tools", toolsTableColumns, toolsShrinkLadder},
		{"dots", dotsTableColumnSpec, dotsShrinkLadder},
		{"agents", agentsTableColumns, agentsShrinkLadder},
		{"agents registry", agentsRegistryColumns, agentsRegistryShrinkLadder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			declared := columnsByKey(tc.cols)
			for _, rung := range tc.ladder {
				if _, ok := declared[rung.key]; !ok {
					t.Errorf("shrink ladder names %q, which the spec does not declare", rung.key)
				}
			}
		})
	}
}

// Agents is the only tab with a footer, so it is the only one where the footer
// length feeds back into the viewport the scroll window is placed against.
func TestSectionedTabHeightModelCoversTheFooteredTab(t *testing.T) {
	t.Parallel()
	m := baseModel(nil)
	m.width, m.height = 100, 24
	m.mode = viewSkills
	m.agentsRowsKnown = true
	// Activity is all the footer carries now, so it is what keeps the footer budget under test.
	m.apmCommand = "omni agents sync"
	for i := range 12 {
		m.agentsRows = append(m.agentsRows, app.AgentsPackageRow{Name: "pkg-" + strconv.Itoa(i)})
	}
	for _, cursor := range []int{0, 5, 11} {
		m.agentsCursor = cursor
		tab := m.agentsSectionedTab()
		if len(tab.footer) == 0 {
			t.Fatal("fixture has no footer, so it cannot exercise the footer budget")
		}
		modelled := sectionedTabModelLineCount(tab) + len(tab.pinnedTop) + len(tab.footer) + 1
		if actual := strings.Count(renderSectionedTabUnwindowed(m, tab), "\n"); modelled != actual {
			t.Errorf("cursor %d: height model says %d lines, writer produced %d", cursor, modelled, actual)
		}
	}
}
