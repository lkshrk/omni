package tui

import (
	"slices"
	"testing"

	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/dots"
)

// Relative order every row context shares: claim/adopt, group, ignore, then the destructive action.
func assertCanonicalHintOrder(t *testing.T, canonical []string, items []hintItem) {
	t.Helper()
	last := -1
	for _, item := range items {
		idx := slices.Index(canonical, item.key)
		if idx < 0 {
			continue
		}
		if idx <= last {
			t.Fatalf("hint keys %v diverge from canonical order %v", hintKeys(items), canonical)
		}
		last = idx
	}
}

func hintKeyIndex(items []hintItem, key string) int {
	return slices.IndexFunc(items, func(h hintItem) bool { return h.key == key })
}

func assertHintPrecedes(t *testing.T, items []hintItem, first, second string) {
	t.Helper()
	firstIdx := hintKeyIndex(items, first)
	secondIdx := hintKeyIndex(items, second)
	if firstIdx < 0 {
		t.Fatalf("hint keys %v missing %q", hintKeys(items), first)
	}
	if secondIdx < 0 {
		t.Fatalf("hint keys %v missing %q", hintKeys(items), second)
	}
	if firstIdx > secondIdx {
		t.Fatalf("hint keys %v put %q after %q", hintKeys(items), first, second)
	}
}

func dotsGroupedIgnorableModel() Model {
	m := baseModel(nil)
	m.mode = viewDots
	m.dotsLoaded = true
	m.dotsCursor = 0
	setDotsRepoForTest(&m, "/repo/dotfiles")
	m.dotsEntries = []app.DotStatus{{
		Name:       "nvim",
		TargetPath: "~/.config/nvim",
		State:      dots.StateSynced,
		Actions:    []dots.Action{dots.ActionIgnore, dots.ActionRemove},
	}}
	m.dotMemberships = map[string][]string{"nvim": {"base"}}
	return m
}

func TestAgentsNativeHintOrder_AdoptThenIgnoreThenRemove(t *testing.T) {
	m, _ := agentsNativeModel(t)
	m.agentsCursor = agentsNativeAdoptableIdx

	items := agentsNativeHintItems(m)
	assertHintPrecedes(t, items, m.keys.AgentsNativeAdopt.Help().Key, m.keys.AgentsNativeIgnore.Help().Key)
	assertHintPrecedes(t, items, m.keys.AgentsNativeIgnore.Help().Key, m.keys.AgentsRemove.Help().Key)
}

func TestAgentsNativeHintOrder_UnadoptableKeepsRemoveLast(t *testing.T) {
	m, _ := agentsNativeModel(t)
	m.agentsCursor = agentsNativeBlockedIdx

	items := agentsNativeHintItems(m)
	if hintKeyIndex(items, m.keys.AgentsNativeAdopt.Help().Key) >= 0 {
		t.Fatalf("unadoptable row offers adopt: %v", hintKeys(items))
	}
	assertHintPrecedes(t, items, m.keys.AgentsNativeIgnore.Help().Key, m.keys.AgentsRemove.Help().Key)
}

func TestAgentsNativeHintOrder_IgnoredRowOffersOnlyUnignore(t *testing.T) {
	m, _ := agentsNativeModel(t)
	m.agentsCursor = agentsNativeIgnoredIdx

	items := agentsNativeHintItems(m)
	if got := hintKeys(items); len(got) != 1 || got[0] != m.keys.AgentsNativeIgnore.Help().Key {
		t.Fatalf("ignored native hints = %v, want only %q", got, m.keys.AgentsNativeIgnore.Help().Key)
	}
}

func TestAgentsPackageHintOrder_UpdateBeforeRemove(t *testing.T) {
	m, _ := agentsRowOpModel(t)
	m.agentsCursor = 0

	items := agentsRowHintItems(m)
	assertHintPrecedes(t, items, m.keys.AgentsUpdate.Help().Key, m.keys.AgentsRemove.Help().Key)
}

func TestToolInlineHintOrder_GroupThenIgnoreThenDelete(t *testing.T) {
	t.Parallel()
	for name, tool := range map[string]*app.ToolView{
		"installed":          {Name: "ripgrep", Provider: "brew", Installed: true, Tracked: true},
		"not installed":      {Name: "fd", Provider: "brew", Installed: false, Tracked: true},
		"installed outdated": {Name: "jq", Provider: "brew", Installed: true, Tracked: true, Outdated: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := baseModel([]*app.ToolView{tool})
			items := toolInlineHints(m, tool)
			assertHintPrecedes(t, items, m.keys.MoveGroup.Help().Key, m.keys.Ignore.Help().Key)
			assertHintPrecedes(t, items, m.keys.Ignore.Help().Key, m.keys.Delete.Help().Key)
		})
	}
}

func TestToolInlineHintOrder_OrphanClaimBeforeIgnore(t *testing.T) {
	t.Parallel()
	tool := &app.ToolView{Name: "grok", Provider: "brew", Installed: true, Tracked: false}
	m := baseModel([]*app.ToolView{tool})

	items := toolInlineHints(m, tool)
	assertHintPrecedes(t, items, m.keys.Claim.Help().Key, m.keys.Ignore.Help().Key)
	assertHintPrecedes(t, items, m.keys.Ignore.Help().Key, m.keys.Delete.Help().Key)
}

func TestDotsRowHintOrder_GroupBeforeIgnore(t *testing.T) {
	t.Parallel()
	m := dotsGroupedIgnorableModel()

	items := dotsRowHintItems(m)
	assertHintPrecedes(t, items, m.keys.MoveGroup.Help().Key, m.keys.DotIgnore.Help().Key)
	assertHintPrecedes(t, items, m.keys.DotIgnore.Help().Key, m.keys.DotDelete.Help().Key)
}

func TestRowActionHintsShareOneCanonicalOrder(t *testing.T) {
	k := DefaultKeyMap()
	toolsCanonical := []string{k.Claim.Help().Key, k.MoveGroup.Help().Key, k.Ignore.Help().Key, k.Delete.Help().Key}
	dotsCanonical := []string{k.MoveGroup.Help().Key, k.DotIgnore.Help().Key, k.DotDelete.Help().Key}
	nativeCanonical := []string{k.AgentsNativeAdopt.Help().Key, k.AgentsNativeIgnore.Help().Key, k.AgentsRemove.Help().Key}
	packageCanonical := []string{k.AgentsUpdate.Help().Key, k.AgentsRemove.Help().Key}

	toolItems := func(tool *app.ToolView) []hintItem { return toolInlineHints(baseModel([]*app.ToolView{tool}), tool) }
	nativeItems := func(cursor int) []hintItem {
		m, _ := agentsNativeModel(t)
		m.agentsCursor = cursor
		return agentsNativeHintItems(m)
	}

	for _, tc := range []struct {
		name      string
		canonical []string
		build     func() []hintItem
	}{
		{"tools installed", toolsCanonical, func() []hintItem {
			return toolItems(&app.ToolView{Name: "ripgrep", Provider: "brew", Installed: true, Tracked: true})
		}},
		{"tools not installed tracked", toolsCanonical, func() []hintItem {
			return toolItems(&app.ToolView{Name: "fd", Provider: "brew", Installed: false, Tracked: true})
		}},
		{"tools orphan", toolsCanonical, func() []hintItem {
			return toolItems(&app.ToolView{Name: "grok", Provider: "brew", Installed: true, Tracked: false})
		}},
		{"tools ignored", toolsCanonical, func() []hintItem {
			tool := &app.ToolView{Name: "hexyl", Provider: "brew", Installed: true, Tracked: true}
			m := baseModel([]*app.ToolView{tool})
			m.ignoreSet = map[string]bool{"hexyl": true}
			return toolInlineHints(m, tool)
		}},
		{"dots entry", dotsCanonical, func() []hintItem { return dotsRowHintItems(dotsGroupedIgnorableModel()) }},
		{"agents native adoptable", nativeCanonical, func() []hintItem { return nativeItems(agentsNativeAdoptableIdx) }},
		{"agents native unadoptable", nativeCanonical, func() []hintItem { return nativeItems(agentsNativeBlockedIdx) }},
		{"agents native ignored", nativeCanonical, func() []hintItem { return nativeItems(agentsNativeIgnoredIdx) }},
		{"agents package", packageCanonical, func() []hintItem {
			m, _ := agentsRowOpModel(t)
			m.agentsCursor = 0
			return agentsRowHintItems(m)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := tc.build()
			if len(items) == 0 {
				t.Fatal("no hints built")
			}
			assertCanonicalHintOrder(t, tc.canonical, items)
		})
	}
}
