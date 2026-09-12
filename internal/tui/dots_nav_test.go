package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/dots"
)

// listAvailableHeight subtracts the title row, both separators and the status bar.
const dotsNavViewport = 10

func dotsNavKeys() map[string]tea.Msg {
	return map[string]tea.Msg{
		"home":   tea.KeyPressMsg{Code: tea.KeyHome},
		"G":      pressRune('G'),
		"end":    tea.KeyPressMsg{Code: tea.KeyEnd},
		"ctrl+d": pressCtrlD(),
		"ctrl+u": pressCtrlU(),
		"ctrl+f": pressCtrlF(),
		"ctrl+b": pressCtrlB(),
		"pgdown": tea.KeyPressMsg{Code: tea.KeyPgDown},
		"pgup":   tea.KeyPressMsg{Code: tea.KeyPgUp},
	}
}

func dotsNavModel(rows int) Model {
	m := baseModel(nil)
	m.mode = viewDots
	m.dotsLoaded = true
	m.height = dotsNavViewport + 4
	setDotsRepoForTest(&m, "/repo/dotfiles")
	entries := make([]app.DotStatus, rows)
	for i := range entries {
		name := fmt.Sprintf("dot%02d", i)
		entries[i] = app.DotStatus{
			Name:       name,
			TargetPath: "~/.config/" + name,
			State:      dots.StateSynced,
		}
	}
	m.dotsEntries = entries
	return m
}

func TestDotsNav_ViewportMatchesTestAssumption(t *testing.T) {
	t.Parallel()
	m := dotsNavModel(20)
	if got := sectionedTabViewport(m, dotsSectionedTab(m)); got != dotsNavViewport {
		t.Fatalf("dots viewport = %d, want %d; the paging expectations below are derived from it", got, dotsNavViewport)
	}
}

func TestDotsNav_JumpKeysMoveCursor(t *testing.T) {
	t.Parallel()
	keys := dotsNavKeys()
	tests := []struct {
		name       string
		msgs       []tea.Msg
		wantCursor int
	}{
		{"ctrl+d moves a half page down", []tea.Msg{keys["ctrl+d"]}, 5},
		{"ctrl+f moves a full page down", []tea.Msg{keys["ctrl+f"]}, 10},
		{"pgdown moves a full page down", []tea.Msg{keys["pgdown"]}, 10},
		{"G lands on the last row", []tea.Msg{keys["G"]}, 19},
		{"end lands on the last row", []tea.Msg{keys["end"]}, 19},
		{"home lands on the first row", []tea.Msg{keys["G"], keys["home"]}, 0},
		{"ctrl+u moves a half page up", []tea.Msg{keys["G"], keys["ctrl+u"]}, 14},
		{"ctrl+b moves a full page up", []tea.Msg{keys["G"], keys["ctrl+b"]}, 9},
		{"pgup moves a full page up", []tea.Msg{keys["G"], keys["pgup"]}, 9},
		{"half page down then up returns", []tea.Msg{keys["ctrl+d"], keys["ctrl+d"], keys["ctrl+u"]}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := drive(dotsNavModel(20), tt.msgs...)
			if got.dotsCursor != tt.wantCursor {
				t.Fatalf("dotsCursor = %d, want %d", got.dotsCursor, tt.wantCursor)
			}
		})
	}
}

func TestDotsNav_PagingClampsWhileStepWraps(t *testing.T) {
	t.Parallel()
	keys := dotsNavKeys()
	tests := []struct {
		name       string
		msgs       []tea.Msg
		wantCursor int
	}{
		{"ctrl+u clamps at the first row", []tea.Msg{keys["ctrl+u"]}, 0},
		{"ctrl+b clamps at the first row", []tea.Msg{keys["ctrl+b"]}, 0},
		{"pgup clamps at the first row", []tea.Msg{keys["pgup"]}, 0},
		{"ctrl+d clamps at the last row", []tea.Msg{keys["ctrl+d"], keys["ctrl+d"], keys["ctrl+d"], keys["ctrl+d"], keys["ctrl+d"]}, 19},
		{"ctrl+f clamps at the last row", []tea.Msg{keys["ctrl+f"], keys["ctrl+f"], keys["ctrl+f"]}, 19},
		{"pgdown clamps at the last row", []tea.Msg{keys["pgdown"], keys["pgdown"], keys["pgdown"]}, 19},
		{"up still wraps to the last row", []tea.Msg{pressUp()}, 19},
		{"down still wraps to the first row", []tea.Msg{keys["G"], pressDown()}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := drive(dotsNavModel(20), tt.msgs...)
			if got.dotsCursor != tt.wantCursor {
				t.Fatalf("dotsCursor = %d, want %d", got.dotsCursor, tt.wantCursor)
			}
		})
	}
}

func TestDotsNav_EmptyListIsNoOp(t *testing.T) {
	t.Parallel()
	for name, msg := range dotsNavKeys() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := drive(dotsNavModel(0), msg)
			if got.dotsCursor != 0 {
				t.Fatalf("dotsCursor = %d, want 0 on an empty dots list", got.dotsCursor)
			}
		})
	}
}

func TestDotsNav_SingleRowStaysPut(t *testing.T) {
	t.Parallel()
	for name, msg := range dotsNavKeys() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := drive(dotsNavModel(1), msg)
			if got.dotsCursor != 0 {
				t.Fatalf("dotsCursor = %d, want 0 on a single-row dots list", got.dotsCursor)
			}
		})
	}
}

func TestDotsNav_JumpKeysClearConfirmState(t *testing.T) {
	t.Parallel()
	states := map[string]func(*Model){
		"delete":    func(m *Model) { m.dotsConfirmIdx = 0 },
		"overwrite": func(m *Model) { m.dotsOverwriteIdx = 0 },
		"local":     func(m *Model) { m.dotsLocalIdx = 0 },
		"ignore":    func(m *Model) { m.dotsIgnoreIdx = 0 },
		"variant":   func(m *Model) { m.dotsVariantIdx = 0; m.dotsVariantMode = dotsVariantCreate },
	}
	for stateName, arm := range states {
		for keyName, msg := range dotsNavKeys() {
			t.Run(stateName+"/"+keyName, func(t *testing.T) {
				t.Parallel()
				m := dotsNavModel(20)
				arm(&m)
				got := drive(m, msg)
				if got.dotsConfirmIdx != -1 || got.dotsOverwriteIdx != -1 || got.dotsLocalIdx != -1 ||
					got.dotsIgnoreIdx != -1 || got.dotsVariantIdx != -1 {
					t.Fatalf("confirm state still armed: confirm=%d overwrite=%d local=%d ignore=%d variant=%d",
						got.dotsConfirmIdx, got.dotsOverwriteIdx, got.dotsLocalIdx, got.dotsIgnoreIdx, got.dotsVariantIdx)
				}
				if got.dotsVariantMode != dotsVariantNone {
					t.Fatalf("dotsVariantMode = %v, want dotsVariantNone", got.dotsVariantMode)
				}
			})
		}
	}
}

func dotsNavExpandedModel() Model {
	m := dotsNavModel(0)
	beta := app.DotStatus{
		Name:       "beta",
		TargetPath: "~/.config/beta",
		State:      dots.StateSynced,
		IsDir:      true,
		Children: []app.DotChild{
			{Name: "one", RelPath: "one", Path: "~/.config/beta/one"},
			{Name: "two", RelPath: "two", Path: "~/.config/beta/two"},
		},
	}
	m.dotsEntries = []app.DotStatus{
		{Name: "alpha", TargetPath: "~/.config/alpha", State: dots.StateSynced},
		beta,
		{Name: "gamma", TargetPath: "~/.config/gamma", State: dots.StateSynced},
	}
	m.dotsExpandedName = beta.Name
	m.dotsExpandedState = app.DotStatusState(beta)
	m.dotsCursor = 2
	return m
}

func TestDotsNav_JumpCollapsesExpandedEntry(t *testing.T) {
	t.Parallel()
	keys := dotsNavKeys()
	tests := []struct {
		name       string
		msg        tea.Msg
		wantCursor int
		wantName   string
	}{
		{"end leaves the cursor on the last collapsed row", keys["end"], 2, "gamma"},
		{"G leaves the cursor on the last collapsed row", keys["G"], 2, "gamma"},
		{"home leaves the cursor on the first collapsed row", keys["home"], 0, "alpha"},
		{"ctrl+f leaves the cursor on the last collapsed row", keys["ctrl+f"], 2, "gamma"},
		{"ctrl+b leaves the cursor on the first collapsed row", keys["ctrl+b"], 0, "alpha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := dotsNavExpandedModel()
			if got := len(dotsVisibleRows(m)); got != 5 {
				t.Fatalf("expanded visible rows = %d, want 5", got)
			}
			got := drive(m, tt.msg)
			if got.dotsExpandedName != "" {
				t.Fatalf("dotsExpandedName = %q, want the entry collapsed", got.dotsExpandedName)
			}
			rows := dotsVisibleRows(got)
			if len(rows) != 3 {
				t.Fatalf("collapsed visible rows = %d, want 3", len(rows))
			}
			if got.dotsCursor != tt.wantCursor {
				t.Fatalf("dotsCursor = %d, want %d", got.dotsCursor, tt.wantCursor)
			}
			if rows[got.dotsCursor].entry.Name != tt.wantName {
				t.Fatalf("cursor row = %q, want %q", rows[got.dotsCursor].entry.Name, tt.wantName)
			}
		})
	}
}
