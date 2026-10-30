package tui

import (
	"strings"
	"testing"

	"github.com/lkshrk/omni/internal/app"
)

func agentsKindsModel(t *testing.T) Model {
	t.Helper()
	m := baseModel(nil)
	m.width, m.height = 160, 80
	m.mode = viewSkills
	m.cursorHidden = true
	m.agentsRowsKnown = true
	m.agentsRows = []app.AgentsPackageRow{
		{Name: "bundlepkg", Source: "acme/bundlepkg", Version: "1.0.0", PackageType: "apm_package", Status: app.AgentsPackageInstalled,
			DeployedPaths: []string{".claude/skills/a/SKILL.md", ".claude/skills/b/SKILL.md"},
			Provides:      []app.AgentsProvidedChild{{Kind: "mcp", Name: "m1"}}},
		{Name: "pluginpkg", Source: "acme/pluginpkg", Version: "2.0.0", PackageType: "marketplace_plugin", Status: app.AgentsPackageInstalled},
		{Name: "skillpkg", Source: "acme/skillpkg", Version: "3.0.0", PackageType: "claude_skill", Status: app.AgentsPackageInstalled,
			DeployedPaths: []string{".claude/skills/skillpkg/SKILL.md"}},
		{Name: "manyskills", Source: "acme/manyskills", PackageType: "skill_bundle", Status: app.AgentsPackageInstalled,
			DeployedPaths: []string{".claude/skills/x/SKILL.md", ".claude/skills/y/SKILL.md", ".agents/skills/z/SKILL.md"}},
		{Name: "oddpkg", Source: "acme/oddpkg", PackageType: "mystery", Status: app.AgentsPackageInstalled},
	}
	m.agentsMCPRows = []app.AgentsServiceRow{
		{Name: "managed-mcp", Detail: "http", Status: app.AgentsPackageInstalled},
		{Name: "handmade-mcp", Detail: "unmanaged", Unmanaged: true, Status: app.AgentsPackageOrphaned},
	}
	m.agentsLSPRows = []app.AgentsServiceRow{{Name: "gopls", Detail: "gopls", Status: app.AgentsPackageInstalled}}
	m.agentsNativeRows = []app.AgentsNativeRow{{Target: "claude", Kind: "plugin", Identity: "native@mkt", Ignored: true}}
	return m
}

func agentsSectionTitleOrder(t *testing.T, view string, titles ...string) {
	t.Helper()
	last := -1
	for _, title := range titles {
		at := strings.Index(view, "\n"+title)
		if at < 0 {
			at = strings.Index(view, title)
		}
		if at <= last {
			t.Fatalf("section %q at %d, want after %d:\n%s", title, at, last, view)
		}
		last = at
	}
}

func TestAgentsSectionsFollowKindOrderAndHideEmpty(t *testing.T) {
	m := agentsKindsModel(t)
	m.agentsRows[3].UpdateAvailable, m.agentsRows[3].LatestVersion = true, "2.1.0"
	view := stripANSIEscapeSequences(m.viewSkillsBody())
	agentsSectionTitleOrder(t, view, "Updates available", "Plugins", "Skills", "MCP servers", "LSP servers", "Bundles", agentsNativeSectionTitle)

	m = agentsKindsModel(t)
	m.agentsRows = m.agentsRows[2:4]
	m.agentsLSPRows = nil
	m.agentsNativeRows = nil
	view = stripANSIEscapeSequences(m.viewSkillsBody())
	agentsSectionTitleOrder(t, view, "Skills", "MCP servers")
	for _, hidden := range []string{"Updates available", "Plugins", "LSP servers", "Bundles", agentsNativeSectionTitle} {
		if strings.Contains(view, hidden) {
			t.Fatalf("empty section %q rendered:\n%s", hidden, view)
		}
	}
}

func TestAgentsRowsShowContentsPerKind(t *testing.T) {
	m := agentsKindsModel(t)
	view := stripANSIEscapeSequences(m.viewSkillsBody())
	for name, contents := range map[string]string{
		"bundlepkg":    "2 skills · 1 mcp",
		"pluginpkg":    "plugin",
		"skillpkg":     "skill",
		"manyskills":   "3 skills",
		"oddpkg":       "package",
		"managed-mcp":  "mcp",
		"handmade-mcp": "mcp",
		"gopls":        "lsp",
		"native@mkt":   "plugin · ignored",
	} {
		line := agentsRowLineContaining(t, view, name)
		if !strings.Contains(line, " "+contents+" ") && !strings.HasSuffix(strings.TrimRight(line, " "), " "+contents) {
			t.Fatalf("%s row %q lacks contents %q", name, line, contents)
		}
	}
	unmanaged := agentsRowLineContaining(t, view, "handmade-mcp")
	if !strings.Contains(unmanaged, "unmanaged") {
		t.Fatalf("unmanaged state lost from row %q", unmanaged)
	}
	if managed := agentsRowLineContaining(t, view, "managed-mcp "); strings.Contains(managed, "unmanaged") {
		t.Fatalf("managed row %q reads as unmanaged", managed)
	}
	if line := agentsRowLineContaining(t, view, "pluginpkg"); !strings.Contains(line, "acme") {
		t.Fatalf("publisher missing from %q", line)
	}
}

func TestAgentsUpdatesSectionHoldsEveryKindWithContents(t *testing.T) {
	m := agentsKindsModel(t)
	for _, i := range []int{0, 1, 2} {
		m.agentsRows[i].UpdateAvailable, m.agentsRows[i].LatestVersion = true, "9.0.0"
	}
	view := stripANSIEscapeSequences(m.viewSkillsBody())
	updates, plugins := strings.Index(view, "Updates available"), strings.Index(view, "Plugins")
	if updates < 0 {
		t.Fatalf("no updates section:\n%s", view)
	}
	if plugins >= 0 {
		t.Fatalf("the only plugin is outdated, yet a Plugins section rendered:\n%s", view)
	}
	skills := strings.Index(view, "Skills")
	for name, contents := range map[string]string{"bundlepkg": "2 skills · 1 mcp", "pluginpkg": "plugin", "skillpkg": "skill"} {
		at := strings.Index(view, name)
		if at < updates || (skills >= 0 && at > skills) {
			t.Fatalf("%s not under Updates available:\n%s", name, view)
		}
		if line := agentsRowLineContaining(t, view, name); !strings.Contains(line, contents) {
			t.Fatalf("%s update row %q lacks contents %q", name, line, contents)
		}
	}
}

func TestAgentsPublisherCollapsesFirstWhenNarrowing(t *testing.T) {
	m := agentsKindsModel(t)
	m.agentsRows[1].Source = "a-rather-long-publisher-name/pluginpkg"
	full := agentsColumnWidths(m)
	if full.publisher == 0 {
		t.Fatalf("publisher column absent at full width: %+v", full)
	}
	collapsed := false
	for width := 160; width >= 40; width-- {
		m.width = width
		cols := agentsColumnWidths(m)
		if cols.publisher == 0 {
			collapsed = true
			if cols.name != full.name || cols.contents != full.contents || cols.version != full.version {
				t.Fatalf("width %d: publisher collapse reached other columns early: %+v vs %+v", width, cols, full)
			}
			break
		}
		if cols.name != full.name || cols.contents != full.contents || cols.version != full.version || cols.targets != full.targets {
			t.Fatalf("width %d: %+v shrank before the publisher collapsed (full %+v)", width, cols, full)
		}
	}
	if !collapsed {
		t.Fatal("publisher never collapsed")
	}
}

func TestAgentsUnmanagedStateSurvivesNarrowRows(t *testing.T) {
	m := agentsKindsModel(t)
	for _, width := range []int{50, 60, 80} {
		m.width = width
		view := stripANSIEscapeSequences(m.viewSkillsBody())
		if !strings.Contains(view, "unmanaged") || !strings.Contains(view, "handmade-mcp") {
			t.Fatalf("width %d: unmanaged row lost state or identity:\n%s", width, view)
		}
	}
}

func TestAgentsUnmanagedStateDoesNotWidenVersionColumn(t *testing.T) {
	m := agentsKindsModel(t)
	withUnmanaged := agentsColumnWidths(m)
	m.agentsMCPRows[1].Unmanaged = false
	withoutUnmanaged := agentsColumnWidths(m)
	if withUnmanaged.version != withoutUnmanaged.version {
		t.Fatalf("version width = %d with unmanaged state, %d without", withUnmanaged.version, withoutUnmanaged.version)
	}
	if withUnmanaged.name != withoutUnmanaged.name {
		t.Fatalf("name width = %d with unmanaged state, %d without", withUnmanaged.name, withoutUnmanaged.name)
	}
}

func TestAgentsViewNeverExceedsTerminalWidth(t *testing.T) {
	for _, width := range []int{24, 32, 40, 60, 80, 100, 120, 200} {
		m := agentsKindsModel(t)
		m.agentsRows[0].Source = "a-very-long-publisher-organisation-name/bundlepkg"
		m.agentsRows[1].UpdateAvailable, m.agentsRows[1].LatestVersion = true, "12.34.56-rc.1"
		m.width = width
		m.cursorHidden = false
		for cursor := 0; cursor < m.agentsRowCount(); cursor++ {
			m.agentsCursor = cursor
			assertNoLineExceedsWidth(t, m.viewSkillsBody(), width)
		}
	}
}
