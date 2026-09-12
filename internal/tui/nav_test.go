package tui

import "testing"

func TestListNavStepWraps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		index int
		count int
		delta int
		want  int
	}{
		{"down from middle", 1, 5, 1, 2},
		{"up from middle", 3, 5, -1, 2},
		{"down from last wraps to first", 4, 5, 1, 0},
		{"up from first wraps to last", 0, 5, -1, 4},
		{"single item down stays", 0, 1, 1, 0},
		{"single item up stays", 0, 1, -1, 0},
		{"empty list down", 0, 0, 1, 0},
		{"empty list up", 0, 0, -1, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := newListNav(tc.index, tc.count, 10).step(tc.delta)
			if got != tc.want {
				t.Errorf("step(%d) from %d of %d = %d, want %d", tc.delta, tc.index, tc.count, got, tc.want)
			}
		})
	}
}

func TestListNavHalfPageClamps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		index    int
		count    int
		viewport int
		dir      int
		want     int
	}{
		{"down half of ten", 0, 40, 10, 1, 5},
		{"up half of ten", 20, 40, 10, -1, 15},
		{"down clamps at last", 38, 40, 10, 1, 39},
		{"up clamps at first", 2, 40, 10, -1, 0},
		{"tiny viewport still moves one", 0, 40, 1, 1, 1},
		{"zero viewport still moves one", 0, 40, 0, 1, 1},
		{"empty list", 0, 0, 10, 1, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := newListNav(tc.index, tc.count, tc.viewport).halfPage(tc.dir)
			if got != tc.want {
				t.Errorf("halfPage(%d) from %d of %d viewport %d = %d, want %d", tc.dir, tc.index, tc.count, tc.viewport, got, tc.want)
			}
		})
	}
}

func TestListNavPageClamps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		index    int
		count    int
		viewport int
		dir      int
		want     int
	}{
		{"down one viewport", 0, 40, 10, 1, 10},
		{"up one viewport", 25, 40, 10, -1, 15},
		{"down clamps at last", 35, 40, 10, 1, 39},
		{"up clamps at first", 5, 40, 10, -1, 0},
		{"empty list", 0, 0, 10, -1, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := newListNav(tc.index, tc.count, tc.viewport).page(tc.dir)
			if got != tc.want {
				t.Errorf("page(%d) from %d of %d viewport %d = %d, want %d", tc.dir, tc.index, tc.count, tc.viewport, got, tc.want)
			}
		})
	}
}

func TestListNavFirstAndLast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		count     int
		wantFirst int
		wantLast  int
	}{
		{"populated", 5, 0, 4},
		{"single item", 1, 0, 0},
		{"empty", 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			nav := newListNav(3, tc.count, 10)
			if got := nav.first(); got != tc.wantFirst {
				t.Errorf("first() of %d = %d, want %d", tc.count, got, tc.wantFirst)
			}
			if got := nav.last(); got != tc.wantLast {
				t.Errorf("last() of %d = %d, want %d", tc.count, got, tc.wantLast)
			}
		})
	}
}
