package tui

import (
	tea "charm.land/bubbletea/v2"
)

// listNav resolves cursor movement for one list tab from the index and size of
// the current navigable set plus the usable viewport height. Single-step
// movement wraps; half-page, page, first and last clamp.
type listNav struct {
	index    int
	count    int
	viewport int
}

func newListNav(index, count, viewport int) listNav {
	return listNav{index: index, count: count, viewport: max(viewport, 1)}
}

func (n listNav) step(delta int) int {
	return cursorMove(n.index, delta, n.count, true)
}

func (n listNav) halfPage(dir int) int {
	return n.byPage(dir, max(n.viewport/2, 1))
}

func (n listNav) page(dir int) int {
	return n.byPage(dir, n.viewport)
}

func (n listNav) byPage(dir, span int) int {
	return clampIndex(n.index+dir*span, n.count)
}

func (n listNav) first() int {
	return 0
}

func (n listNav) last() int {
	return clampIndex(n.count-1, n.count)
}

// Arrows and ctrl chords move the selection while a filter input has focus; the
// printable keys must still reach the input, so j and k are not bound here.
func filterNavStep(msg tea.KeyPressMsg, nav listNav) (int, bool) {
	ctrl := msg.Mod&^lockMods == tea.ModCtrl
	switch {
	case msg.Code == tea.KeyUp || (ctrl && msg.Code == 'p'):
		return nav.step(-1), true
	case msg.Code == tea.KeyDown || (ctrl && msg.Code == 'n'):
		return nav.step(1), true
	case ctrl && msg.Code == 'u':
		return nav.halfPage(-1), true
	case ctrl && msg.Code == 'd':
		return nav.halfPage(1), true
	case msg.Code == tea.KeyPgUp || (ctrl && msg.Code == 'b'):
		return nav.page(-1), true
	case msg.Code == tea.KeyPgDown || (ctrl && msg.Code == 'f'):
		return nav.page(1), true
	case msg.Code == tea.KeyHome:
		return nav.first(), true
	case msg.Code == tea.KeyEnd:
		return nav.last(), true
	}
	return 0, false
}
