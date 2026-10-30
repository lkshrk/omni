package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/dots"
)

const deepChildName = "an-extremely-long-nested-child-file-name.conf"

func dotsColumnsFixture() []app.DotStatus {
	return []app.DotStatus{
		{
			Name: "hypr", TargetPath: "~/.config/hypr", State: dots.StateSynced, IsDir: true, FileCount: 27,
			Children: []app.DotChild{{
				Name: "conf.d", RelPath: "conf.d", Path: "~/.config/hypr/conf.d", IsDir: true, Depth: 1,
				Children: []app.DotChild{{Name: deepChildName, RelPath: "conf.d/" + deepChildName, Path: "~/.config/hypr/conf.d/" + deepChildName, Depth: 2}},
			}},
		},
		{Name: "knowledge-sync-systemd", TargetPath: "~/.config/systemd/user/knowledge-sync.service", State: dots.StateSynced, Counts: app.DotFileCounts{Ignored: 11006}},
		{Name: "omarchy-shell", TargetPath: "~/.config/omarchy/shell", State: dots.StateSynced, IsDir: true, FileCount: 53},
	}
}

func dotsColumnsModel(width int) Model {
	m := baseModel(nil)
	m.mode = viewDots
	m.width = width
	m.height = 40
	m.dotsSyncAvailCached = app.DotsSyncAvailability{Reason: app.DotsSyncAvailabilityReady, Configured: true}
	m.dotsLoaded = true
	m.dotsEntries = dotsColumnsFixture()
	m.dotMemberships = map[string][]string{
		"hypr":                   {"omarchy"},
		"knowledge-sync-systemd": {"omarchy"},
		"omarchy-shell":          {"omarchy"},
	}
	return m
}

func widestTopLevelName(m Model) int {
	widest := dotsNameMinW
	for _, e := range m.dotsEntries {
		widest = max(widest, lipgloss.Width(dotEntryDisplayName(m, e)))
	}
	return widest
}

func TestDotsNameColumnIgnoresCollapsedChildren(t *testing.T) {
	t.Parallel()
	m := dotsColumnsModel(160)
	cols := dotsTableColumnWidths(m.palette, m, filteredDotsEntries(m))
	if want := widestTopLevelName(m); cols.name != want {
		t.Fatalf("name column = %d, want %d (widest top-level name)", cols.name, want)
	}
}

func TestDotsNameColumnGrowsForExpandedChildren(t *testing.T) {
	t.Parallel()
	m := dotsColumnsModel(160)
	hypr := m.dotsEntries[0]
	m.dotsExpandedName = hypr.Name
	m.dotsExpandedState = app.DotStatusState(hypr)
	m.dotsExpandedChildren = map[string]bool{dotsChildExpandKey(hypr.Name, "conf.d"): true}
	cols := dotsTableColumnWidths(m.palette, m, filteredDotsEntries(m))
	deep := hypr.Children[0].Children[0]
	if want := lipgloss.Width(dotChildDisplayName(m, hypr, deep)); cols.name < want {
		t.Fatalf("name column = %d, want at least %d for the visible nested child", cols.name, want)
	}
}

func TestDotsTargetGetsProtectedMinimum(t *testing.T) {
	t.Parallel()
	m := dotsColumnsModel(104)
	contentW := rowAvailableWidth(m.width)
	cols := fitDotsColumnsToWidth(dotsTableColumnWidths(m.palette, m, filteredDotsEntries(m)), contentW)
	if got := dotsTargetWidth(cols, contentW); got < dotsTargetMinW {
		t.Fatalf("target width = %d at content width %d, want at least %d (cols %+v)", got, contentW, dotsTargetMinW, cols)
	}
}

func TestDotsFitProtectsTargetWhileOtherColumnsShrink(t *testing.T) {
	t.Parallel()
	original := dotsTableColumns{name: 50, status: 10, ratio: 5, ignore: 7, group: 20, target: dotsTargetMinW}
	const contentW = 60
	cols := fitDotsColumnsToWidth(original, contentW)
	if cols.name == original.name && cols.group == original.group {
		t.Fatalf("test width does not engage the shrink ladder: %+v", cols)
	}
	if got := dotsTargetWidth(cols, contentW); got < dotsTargetMinW {
		t.Fatalf("target width = %d, want at least %d after shrinking other columns: %+v", got, dotsTargetMinW, cols)
	}
}

func TestDotsFitUsesNameSpareWidthBeforeGroup(t *testing.T) {
	t.Parallel()
	original := dotsTableColumns{name: 30, status: 10, ratio: 5, group: 20, target: dotsTargetMinW}
	contentW := dotsFixedWidth(original) + dotsTargetMinW - 5
	cols := fitDotsColumnsToWidth(original, contentW)
	if cols.name != original.name-5 || cols.group != original.group {
		t.Fatalf("fit = %+v, want name to yield five cells before group", cols)
	}
}

func TestDotsTargetShrinksBelowMinimumOnlyAfterOtherColumnsBottomOut(t *testing.T) {
	t.Parallel()
	original := dotsTableColumns{name: 50, status: 10, ratio: 5, ignore: 7, group: 20, target: dotsTargetMinW}
	const contentW = 20
	cols := fitDotsColumnsToWidth(original, contentW)
	if got := dotsTargetWidth(cols, contentW); got >= dotsTargetMinW {
		t.Fatalf("test width %d leaves target at %d, want a below-minimum boundary case", contentW, got)
	}
	if cols.name > 1 || cols.status > 1 || cols.ratio > 1 || cols.group > 1 || cols.ignore > 1 {
		t.Fatalf("target below minimum while other columns still have width: %+v", cols)
	}
}

func TestDotsViewShowsTargetPathAtReporterWidth(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 104, 140} {
		m := dotsColumnsModel(width)
		out := m.View().Content
		assertNoLineExceedsWidth(t, out, width)
		plain := stripANSIEscapeSequences(out)
		rows := 0
		for _, line := range strings.Split(plain, "\n") {
			if !strings.Contains(line, "synced") {
				continue
			}
			rows++
			if !strings.Contains(line, "~/.") {
				t.Fatalf("width %d: target path missing or collapsed: %q", width, line)
			}
		}
		if rows != len(dotsColumnsFixture()) {
			t.Fatalf("width %d: %d rendered rows, want %d:\n%s", width, rows, len(dotsColumnsFixture()), plain)
		}
	}
}
