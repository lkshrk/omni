package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/lkshrk/omni/internal/app"
)

func recipeScriptTools() []*app.ToolView {
	return []*app.ToolView{
		{Name: "claude-code", Provider: "script", Installed: true, InstalledWith: "script", Version: "2.1.311", Tracked: true},
		{Name: "codebase-memory-mcp", Provider: "script", Installed: true, InstalledWith: "github_release_asset", Version: "0.12.1", Tracked: true},
	}
}

func TestProviderLabelForToolWithPinKeepsDeclaredProviderForRecipeInstall(t *testing.T) {
	t.Parallel()
	tool := recipeScriptTools()[1]
	if got := providerLabelForToolWithPin(tool, "", "", "", "", ""); got != "script" {
		t.Fatalf("providerLabelForToolWithPin = %q, want %q", got, "script")
	}
}

func TestToolColumnsProviderWidthIgnoresRecipeInstalledWith(t *testing.T) {
	t.Parallel()
	cols := newColWidthsWithProviderPins(recipeScriptTools(), nil, nil, nil, nil, nil, "", "", "", 140, nil)
	if want := max(8, len("script")); cols.prov != want {
		t.Fatalf("provider column = %d, want %d (widest rendered label or the 8-cell floor)", cols.prov, want)
	}
}

func TestToolRowsAlignProviderForRecipeInstall(t *testing.T) {
	t.Parallel()
	p := defaultPalette()
	for _, width := range []int{80, 100, 140} {
		tools := recipeScriptTools()
		cols := newColWidthsWithProviderPins(tools, nil, nil, nil, nil, nil, "", "", "", width, nil)
		var columns []int
		for _, tool := range tools {
			line := stripANSIEscapeSequences(renderToolRowWithProviderPin(p, tool, cols, "", nil, nil, "", "", "", "", "", false, false, syncOK))
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("width %d: row %q is %d cells wide", width, tool.Name, w)
			}
			idx := strings.Index(line, "script")
			if idx < 0 {
				t.Fatalf("width %d: row %q does not show script: %q", width, tool.Name, line)
			}
			if strings.Contains(line, "github_release_asset") {
				t.Fatalf("width %d: row %q shows installed_with in the provider column: %q", width, tool.Name, line)
			}
			columns = append(columns, lipgloss.Width(line[:idx]))
		}
		if columns[0] != columns[1] {
			t.Fatalf("width %d: provider starts at %v, want the same column", width, columns)
		}
	}
}

func TestToolRowWrongProviderStillShowsInstalledWith(t *testing.T) {
	t.Parallel()
	tool := &app.ToolView{Name: "jq", Provider: "pacman", Installed: true, InstalledWith: "aur", Version: "1.7.1", Tracked: true}
	cols := newColWidthsWithProviderPins([]*app.ToolView{tool}, nil, nil, nil, nil, nil, "", "", "", 100, func(*app.ToolView) bool { return true })
	line := stripANSIEscapeSequences(renderToolRowWithProviderPin(defaultPalette(), tool, cols, "", nil, nil, "", "", "", "", "", false, false, syncWrongProv))
	if !strings.Contains(line, "aur") {
		t.Fatalf("wrong-provider row = %q, want installed_with aur", line)
	}
}

func TestInlineDetailLinesShowRecipeInstallMethod(t *testing.T) {
	t.Parallel()
	tools := recipeScriptTools()
	m := baseModel([]*app.ToolView{tools[1]})
	m.applyFilter()
	cols := newColWidthsWithProviderPins(m.visibleTools, nil, nil, nil, nil, nil, "", "", "", 100, nil)
	got := stripANSIEscapeSequences(strings.Join(inlineDetailLines(m, 100, cols), "\n"))
	if !strings.Contains(got, "installed with github_release_asset") {
		t.Fatalf("inline detail = %q, want the install method", got)
	}
}

func TestInlineDetailLinesOmitInstallMethodMatchingProvider(t *testing.T) {
	t.Parallel()
	tools := recipeScriptTools()
	m := baseModel([]*app.ToolView{tools[0]})
	m.applyFilter()
	cols := newColWidthsWithProviderPins(m.visibleTools, nil, nil, nil, nil, nil, "", "", "", 100, nil)
	got := stripANSIEscapeSequences(strings.Join(inlineDetailLines(m, 100, cols), "\n"))
	if strings.Contains(got, "installed with") {
		t.Fatalf("inline detail = %q, want no install method line", got)
	}
}

func TestToolColumnsLongNameGivesWayBeforeGroupPills(t *testing.T) {
	t.Parallel()
	tools := []*app.ToolView{
		{Name: "font-jetbrains-mono-nerd-font", Package: "ttf-jetbrains-mono-nerd-basic", Provider: "pacman", Installed: true, Version: "3.4.0-1", Tracked: true},
		{Name: "bats-core", Provider: "pacman", Installed: true, Version: "1.14.0-1", Tracked: true},
		{Name: "pi-coding-agent", Provider: "bun", Installed: true, Outdated: true, Version: "18.12.6", LatestVersion: "18.13.1", Tracked: true},
	}
	cols := newColWidthsWithProviderPins(tools, nil, nil, []string{"test-tooling"}, nil, nil, "", "", "", 90, nil)
	t.Logf("name=%d prov=%d ver=%d group=%d", cols.name, cols.prov, cols.ver, cols.group)
	if want := len("[test-tooling]"); cols.group < want {
		t.Fatalf("group column = %d, want at least %d while the name column (%d) has spare width", cols.group, want, cols.name)
	}
	if cols.name < 24 {
		t.Fatalf("name column = %d, want at least its 24-cell floor", cols.name)
	}
}

func TestToolRowWrongProviderLongerInstalledWithKeepsColumns(t *testing.T) {
	t.Parallel()
	ok := &app.ToolView{Name: "jq", Provider: "pacman", Installed: true, InstalledWith: "pacman", Version: "1.7.1", Tracked: true}
	wrong := &app.ToolView{Name: "fd", Provider: "pacman", Installed: true, InstalledWith: "flatpak", Version: "1.7.2", Tracked: true}
	tools := []*app.ToolView{ok, wrong}
	isWrong := func(tool *app.ToolView) bool { return tool == wrong }
	cols := newColWidthsWithProviderPins(tools, nil, nil, nil, nil, nil, "", "", "", 100, isWrong)
	p := defaultPalette()
	okLine := stripANSIEscapeSequences(renderToolRowWithProviderPin(p, ok, cols, "", nil, nil, "", "", "", "", "", false, false, syncOK))
	wrongLine := stripANSIEscapeSequences(renderToolRowWithProviderPin(p, wrong, cols, "", nil, nil, "", "", "", "", "", false, false, syncWrongProv))
	if !strings.Contains(wrongLine, "flatpak") {
		t.Fatalf("wrong-provider row = %q, want installed_with flatpak", wrongLine)
	}
	okStart := lipgloss.Width(okLine[:strings.Index(okLine, "pacman")])
	wrongStart := lipgloss.Width(wrongLine[:strings.Index(wrongLine, "flatpak")])
	if okStart != wrongStart {
		t.Fatalf("provider starts at %d and %d, want the same column:\n%q\n%q", okStart, wrongStart, okLine, wrongLine)
	}
}
