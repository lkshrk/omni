package app

import "testing"

func TestAgentsPackageKindAndContents(t *testing.T) {
	cases := []struct {
		name     string
		row      AgentsPackageRow
		kind     AgentsPackageKind
		contents string
	}{
		{
			name: "marketplace plugin",
			row: AgentsPackageRow{PackageType: "marketplace_plugin", DeployedPaths: []string{
				".claude/skills/caveman/SKILL.md", ".claude/skills/other/SKILL.md",
			}},
			kind: AgentsKindPlugin, contents: "plugin",
		},
		{
			name: "single skill deployed to two clients",
			row: AgentsPackageRow{PackageType: "claude_skill", DeployedPaths: []string{
				".claude/skills/zulu/SKILL.md", ".claude/skills/zulu/ref.md", ".agents/skills/zulu/SKILL.md",
			}},
			kind: AgentsKindSkill, contents: "skill",
		},
		{
			name:     "skill without deployed files",
			row:      AgentsPackageRow{PackageType: "claude_skill"},
			kind:     AgentsKindSkill,
			contents: "skill",
		},
		{
			name: "multi-skill bundle",
			row: AgentsPackageRow{PackageType: "skill_bundle", DeployedPaths: []string{
				".claude/skills/a/SKILL.md", ".claude/skills/b/SKILL.md", ".agents/skills/c/SKILL.md", ".agents/skills/a/SKILL.md",
			}},
			kind: AgentsKindSkill, contents: "3 skills",
		},
		{
			name: "apm package with owned mcp children",
			row: AgentsPackageRow{
				PackageType: "apm_package",
				DeployedPaths: []string{
					".claude/skills/a/SKILL.md", ".agents/skills/b/SKILL.md",
					".claude/commands/review.md", ".config/opencode/command/review.md", ".claude/commands/ship.md",
					".claude/agents/planner.md",
					".claude/hooks/pre.sh", ".claude/hooks/post.sh", ".codex/hooks",
				},
				Provides: []AgentsProvidedChild{{Kind: "mcp", Name: "one"}, {Kind: "mcp", Name: "two"}, {Kind: "lsp", Name: "gopls"}, {Kind: "skill", Name: "a"}, {Kind: "binary", Name: "tool"}},
			},
			kind: AgentsKindBundle, contents: "2 skills · 2 commands · 1 agent · hooks · 2 mcp · 1 lsp",
		},
		{
			name:     "apm package with only owned mcp children",
			row:      AgentsPackageRow{PackageType: "apm_package", Provides: []AgentsProvidedChild{{Kind: "mcp", Name: "one"}}},
			kind:     AgentsKindBundle,
			contents: "1 mcp",
		},
		{
			name:     "apm package with nothing recognisable",
			row:      AgentsPackageRow{PackageType: "apm_package", DeployedPaths: []string{".config/opencode/opencode.json"}},
			kind:     AgentsKindBundle,
			contents: "package",
		},
		{
			name:     "unknown type",
			row:      AgentsPackageRow{PackageType: "hook_package", DeployedPaths: []string{".claude/skills/a/SKILL.md"}},
			kind:     AgentsKindBundle,
			contents: "package",
		},
		{
			name:     "missing type",
			row:      AgentsPackageRow{},
			kind:     AgentsKindBundle,
			contents: "package",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.Kind(); got != tc.kind {
				t.Fatalf("kind = %q, want %q", got, tc.kind)
			}
			if got := tc.row.Contents(); got != tc.contents {
				t.Fatalf("contents = %q, want %q", got, tc.contents)
			}
		})
	}
}

func TestAgentsStatusExposesPackageTypeAndDeployedPaths(t *testing.T) {
	a := setupAgentsWorkspace(t, agentsTestManifest, agentsTestLock)
	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]AgentsPackageRow{}
	for _, row := range status.Packages {
		byName[row.Name] = row
	}
	if row := byName["alpha"]; row.PackageType != "marketplace_plugin" || row.Kind() != AgentsKindPlugin {
		t.Fatalf("alpha = %+v", row)
	}
	if row := byName["zulu"]; len(row.DeployedPaths) != 1 || row.DeployedPaths[0] != ".claude/skills/zulu/SKILL.md" {
		t.Fatalf("zulu deployed paths = %v", row.DeployedPaths)
	}
	if row := byName["ghost"]; row.Kind() != AgentsKindBundle || row.Contents() != "package" {
		t.Fatalf("orphaned untyped row = %+v", row)
	}
}
