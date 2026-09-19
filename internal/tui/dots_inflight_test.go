package tui

import (
	"testing"

	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/dots"
)

func dotsInFlightConflictModel() Model {
	m := baseModel(nil)
	m.mode = viewDots
	m.dotsLoaded = true
	setDotsRepoForTest(&m, "/repo")
	m.dotsEntries = []app.DotStatus{
		{Name: "gitconfig", Health: app.HealthConflict, State: dots.StateConflict, Actions: []dots.Action{dots.ActionUseRepo, dots.ActionUseLocal, dots.ActionRemove}},
	}
	return m
}

func TestDotsResolveKey_ArmsWhileLaunchSyncInFlight(t *testing.T) {
	t.Parallel()

	m := dotsInFlightConflictModel()
	m.beginDotsOperation("Syncing dots…")
	if !m.dotsLoading {
		t.Fatal("setup: dotsLoading should be true after beginDotsOperation")
	}
	gen := m.dotsOpGen

	got := drive(m, pressRune('u'))

	if got.dotsOverwriteIdx != 0 {
		t.Errorf("dotsOverwriteIdx = %d, want 0 (u arms even with an operation in flight)", got.dotsOverwriteIdx)
	}
	if got.statusMsg != "Press u again to use repo for gitconfig" {
		t.Errorf("statusMsg = %q, want the use-repo arming prompt", got.statusMsg)
	}
	if got.dotsOpGen != gen {
		t.Errorf("dotsOpGen = %d, want %d (arming must not start a new operation)", got.dotsOpGen, gen)
	}
	if got.progressText != "Syncing dots…" {
		t.Errorf("progressText = %q, want the running label to survive the arming status", got.progressText)
	}
	if !got.dotsLoading {
		t.Error("dotsLoading should stay true while the launch sync is in flight")
	}

	confirmed := drive(got, pressRune('u'))
	if confirmed.dotsOpGen != gen+1 {
		t.Errorf("dotsOpGen after confirm = %d, want %d (second u starts a new operation over the in-flight one)", confirmed.dotsOpGen, gen+1)
	}
	if confirmed.progressText != "Using repo for gitconfig…" {
		t.Errorf("progressText after confirm = %q, want the resolve text", confirmed.progressText)
	}
}

func TestDotsRowActionKey_InFlightWithNoRowsLoaded(t *testing.T) {
	t.Parallel()

	emptyLoadingModel := func() Model {
		m := baseModel(nil)
		m.mode = viewDots
		setDotsRepoForTest(&m, "/repo")
		m.beginDotsOperation("Syncing dots…")
		return m
	}

	t.Run("row action keys report that dots are loading", func(t *testing.T) {
		for name, r := range map[string]rune{
			"DotUseRepo": 'u', "DotUseLocal": 'l', "DotDelete": 'd',
			"DotIgnore": 'x', "DotVariant": 'v', "Sync": 's', "MoveGroup": 'g',
		} {
			t.Run(name, func(t *testing.T) {
				m := emptyLoadingModel()
				gen := m.dotsOpGen
				got := drive(m, pressRune(r))

				if got.statusMsg != dotsRowsLoadingStatus {
					t.Errorf("statusMsg = %q, want %q", got.statusMsg, dotsRowsLoadingStatus)
				}
				if got.statusIsErr {
					t.Error("the loading status should not be flagged as an error")
				}
				if got.dotsOverwriteIdx != -1 || got.dotsLocalIdx != -1 || got.dotsConfirmIdx != -1 || got.dotsIgnoreIdx != -1 || got.dotsVariantIdx != -1 {
					t.Errorf("a confirmation armed against an empty list: %#v", []int{got.dotsOverwriteIdx, got.dotsLocalIdx, got.dotsConfirmIdx, got.dotsIgnoreIdx, got.dotsVariantIdx})
				}
				if got.dotsOpGen != gen || !got.dotsLoading {
					t.Errorf("dotsOpGen=%d loading=%v, want %d/true (in-flight operation untouched)", got.dotsOpGen, got.dotsLoading, gen)
				}
				if got.mode != viewDots {
					t.Errorf("mode = %v, want viewDots (g must not open the group picker)", got.mode)
				}
			})
		}
	})

	t.Run("non-row keys fall through without the loading status", func(t *testing.T) {
		for name, r := range map[string]rune{"SyncAll/DotRefresh": 'R', "DotAdd": 'a'} {
			t.Run(name, func(t *testing.T) {
				got := drive(emptyLoadingModel(), pressRune(r))
				if got.statusMsg == dotsRowsLoadingStatus {
					t.Errorf("%s was caught by the row-action guard", name)
				}
			})
		}
	})

	t.Run("empty list without an operation in flight stays silent", func(t *testing.T) {
		m := baseModel(nil)
		m.mode = viewDots
		m.dotsLoaded = true
		setDotsRepoForTest(&m, "/repo")

		got := drive(m, pressRune('u'), pressRune('l'))
		if got.statusMsg != "" {
			t.Errorf("statusMsg = %q, want empty (guard requires dotsLoading)", got.statusMsg)
		}
		if got.dotsOverwriteIdx != -1 || got.dotsLocalIdx != -1 {
			t.Errorf("overwriteIdx=%d localIdx=%d, want -1/-1", got.dotsOverwriteIdx, got.dotsLocalIdx)
		}
	})
}

func TestDotsResolveArm_ClearedByResultForCurrentGen(t *testing.T) {
	t.Parallel()

	entries := []app.DotStatus{
		{Name: "gitconfig", Health: app.HealthConflict, State: dots.StateConflict, Actions: []dots.Action{dots.ActionUseRepo, dots.ActionUseLocal, dots.ActionRemove}},
	}

	armed := func(t *testing.T) Model {
		t.Helper()
		m := dotsInFlightConflictModel()
		m.beginDotsOperation("Syncing dots…")
		m = drive(m, pressRune('u'))
		if m.dotsOverwriteIdx != 0 {
			t.Fatalf("setup: dotsOverwriteIdx = %d, want 0", m.dotsOverwriteIdx)
		}
		return m
	}

	t.Run("dotsSyncedMsg with entries clears the arm", func(t *testing.T) {
		m := armed(t)
		got := drive(m, dotsSyncedMsg{gen: m.dotsOpGen, entries: entries})
		if got.dotsOverwriteIdx != -1 {
			t.Errorf("dotsOverwriteIdx = %d, want -1", got.dotsOverwriteIdx)
		}
		if got.statusMsg != "✓ dots synced" {
			t.Errorf("statusMsg = %q, want the sync result status", got.statusMsg)
		}
	})

	t.Run("snapshot cancels the armed confirmation timeout", func(t *testing.T) {
		m := armed(t)
		armedGen := m.confirmGen

		got := drive(m, dotsSyncedMsg{gen: m.dotsOpGen, entries: entries})
		if got.confirmGen == armedGen {
			t.Fatalf("confirmGen = %d, want it advanced past %d (the in-flight timer must be invalidated, not leaked)", got.confirmGen, armedGen)
		}

		got = drive(got, pressRune('u'))
		if got.dotsOverwriteIdx != 0 {
			t.Fatalf("setup: re-arm failed, dotsOverwriteIdx = %d", got.dotsOverwriteIdx)
		}
		got = drive(got, confirmTimeoutMsg{gen: armedGen})
		if got.dotsOverwriteIdx != 0 {
			t.Errorf("the pre-snapshot timeout expired the re-armed confirmation: dotsOverwriteIdx = %d", got.dotsOverwriteIdx)
		}
	})

	t.Run("dotsLoadedMsg with entries clears the arm", func(t *testing.T) {
		m := armed(t)
		got := drive(m, dotsLoadedMsg{gen: m.dotsOpGen, entries: entries})
		if got.dotsOverwriteIdx != -1 {
			t.Errorf("dotsOverwriteIdx = %d, want -1", got.dotsOverwriteIdx)
		}
	})

	t.Run("dotsSyncedMsg without entries leaves the arm standing", func(t *testing.T) {
		m := armed(t)
		got := drive(m, dotsSyncedMsg{gen: m.dotsOpGen})
		if got.dotsOverwriteIdx != 0 {
			t.Errorf("dotsOverwriteIdx = %d, want 0 (nil entries skip applyDotsSnapshot)", got.dotsOverwriteIdx)
		}
		if confirmed := drive(got, pressRune('u')); confirmed.progressText != "Using repo for gitconfig…" {
			t.Errorf("progressText after u = %q, want the arm to still be actionable", confirmed.progressText)
		}
	})

	t.Run("stale-gen result leaves the arm standing", func(t *testing.T) {
		m := armed(t)
		got := drive(m, dotsSyncedMsg{gen: m.dotsOpGen - 1, entries: entries})
		if got.dotsOverwriteIdx != 0 {
			t.Errorf("dotsOverwriteIdx = %d, want 0", got.dotsOverwriteIdx)
		}
		if !got.dotsLoading {
			t.Error("dotsLoading should stay true for a stale-gen result")
		}
	})
}

func TestSetStatusFor_ProgressTextRetention(t *testing.T) {
	t.Parallel()

	t.Run("keeps the running label while activity is in flight", func(t *testing.T) {
		m := dotsInFlightConflictModel()
		m.beginDotsOperation("Syncing dots…")

		setStatus(&m, "something happened", false)

		if m.progressText != "Syncing dots…" {
			t.Errorf("progressText = %q, want it kept", m.progressText)
		}
		if m.statusMsg != "something happened" {
			t.Errorf("statusMsg = %q, want the new status", m.statusMsg)
		}
	})

	t.Run("clears the running label once nothing is in flight", func(t *testing.T) {
		m := dotsInFlightConflictModel()
		m.progressText = "Syncing dots…"
		if m.spinnerActivityActive() {
			t.Fatal("setup: no activity flag should be set")
		}

		setStatus(&m, "something happened", false)

		if m.progressText != "" {
			t.Errorf("progressText = %q, want empty", m.progressText)
		}
	})

	t.Run("dots completion clears its flag before the status, so no label strands", func(t *testing.T) {
		m := dotsInFlightConflictModel()
		m.beginDotsOperation("Syncing dots…")

		got := drive(m, dotsSyncedMsg{gen: m.dotsOpGen, entries: m.dotsEntries})

		if got.dotsLoading {
			t.Fatal("dotsLoading should be false after the sync result")
		}
		if got.progressText != "" {
			t.Errorf("progressText = %q, want empty once the operation finished", got.progressText)
		}
		if got.statusMsg != "✓ dots synced" {
			t.Errorf("statusMsg = %q, want the completion status", got.statusMsg)
		}
	})
}
