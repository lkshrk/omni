package tui

import "testing"

func TestShrinkColumnsNoOverflowLeavesWidthsAlone(t *testing.T) {
	t.Parallel()
	a, b := 20, 10
	shrinkColumns(0, colStep(&a, 5), colStep(&b, 5))
	if a != 20 || b != 10 {
		t.Fatalf("widths = %d, %d, want 20, 10", a, b)
	}
	shrinkColumns(-4, colStep(&a, 5), colStep(&b, 5))
	if a != 20 || b != 10 {
		t.Fatalf("negative overflow changed widths to %d, %d", a, b)
	}
}

func TestShrinkColumnsTakesFromEarlierStepsFirst(t *testing.T) {
	t.Parallel()
	first, second := 20, 20
	shrinkColumns(6, colStep(&first, 10), colStep(&second, 10))
	if first != 14 || second != 20 {
		t.Fatalf("widths = %d, %d, want 14, 20", first, second)
	}
}

func TestShrinkColumnsSpillsToLaterStepsAtFloor(t *testing.T) {
	t.Parallel()
	first, second := 20, 20
	shrinkColumns(15, colStep(&first, 12), colStep(&second, 10))
	if first != 12 || second != 13 {
		t.Fatalf("widths = %d, %d, want 12, 13", first, second)
	}
}

func TestShrinkColumnsStopsAtFloorsWhenOverflowExceedsSlack(t *testing.T) {
	t.Parallel()
	first, second := 20, 20
	shrinkColumns(100, colStep(&first, 12), colStep(&second, 10))
	if first != 12 || second != 10 {
		t.Fatalf("widths = %d, %d, want 12, 10", first, second)
	}
}

func TestShrinkColumnsRepeatedColumnSurrendersInTwoStages(t *testing.T) {
	t.Parallel()
	repeated, other := 20, 20
	shrinkColumns(25, colStep(&repeated, 15), colStep(&other, 12), colStep(&repeated, 1))
	if repeated != 3 || other != 12 {
		t.Fatalf("widths = %d, %d, want 3, 12", repeated, other)
	}
}

func TestShrinkColumnsFloorOfZeroCollapsesColumn(t *testing.T) {
	t.Parallel()
	collapsible := 8
	shrinkColumns(8, colStep(&collapsible, 0))
	if collapsible != 0 {
		t.Fatalf("width = %d, want 0", collapsible)
	}
}
