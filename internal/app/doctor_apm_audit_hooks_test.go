package app

import (
	"path/filepath"
	"strings"
	"testing"
)

// Shapes apm 0.34 writes for a user-scope Stop hook, after omni's $HOME rewrite.
const (
	apmHookSidecarFixture = `{
  "Stop": [
    {
      "matcher": "*",
      "hooks": [{"type": "command", "command": "$HOME/.claude/hooks/hk/scripts/stop.sh"}],
      "_apm_source": "_local/hk"
    }
  ]
}
`
	apmHookSettingsFixture = `{
  "theme": "dark",
  "hooks": {
    "Stop": [
      {
        "hooks": [{"command": "$HOME/.claude/hooks/hk/scripts/stop.sh", "type": "command"}],
        "matcher": "*"
      },
      {"matcher": "*", "hooks": [{"type": "command", "command": "my-own-hook"}]}
    ]
  }
}
`
)

func writeHookFixtures(t *testing.T, settings string) string {
	t.Helper()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "apm-hooks.json"), apmHookSidecarFixture)
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), settings)
	return home
}

func TestAPMHookDriftReplayArtifactIsDropped(t *testing.T) {
	home := writeHookFixtures(t, apmHookSettingsFixture)
	for _, detail := range []string{"modified: .claude/settings.json", "modified: .claude/apm-hooks.json"} {
		if !apmHookDriftIsReplayArtifact(home, detail) {
			t.Errorf("%q: consistent sidecar and settings should be the replay artifact", detail)
		}
	}
}

func TestAPMHookDriftKeepsRealEdits(t *testing.T) {
	for name, settings := range map[string]string{
		"edited APM hook command": strings.Replace(apmHookSettingsFixture, "scripts/stop.sh", "scripts/other.sh", 1),
		"removed APM hook":        `{"hooks": {"Stop": [{"matcher": "*", "hooks": [{"type": "command", "command": "my-own-hook"}]}]}}`,
		"unreadable settings":     `{not json`,
	} {
		t.Run(name, func(t *testing.T) {
			home := writeHookFixtures(t, settings)
			for _, detail := range []string{"modified: .claude/settings.json", "modified: .claude/apm-hooks.json"} {
				if apmHookDriftIsReplayArtifact(home, detail) {
					t.Errorf("%q was dropped though the live config disagrees with the sidecar", detail)
				}
			}
		})
	}
}

func TestAPMHookDriftOnlyAppliesToModifiedHookFiles(t *testing.T) {
	home := writeHookFixtures(t, apmHookSettingsFixture)
	for _, detail := range []string{
		"unintegrated: .claude/settings.json",
		"orphaned: .claude/apm-hooks.json",
		"modified: .claude/skills/x/SKILL.md",
		"modified: .claude/CLAUDE.md",
	} {
		if apmHookDriftIsReplayArtifact(home, detail) {
			t.Errorf("%q must stay a real finding", detail)
		}
	}
}

func TestAPMHookDriftNeedsOwnedSidecarEntries(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "apm-hooks.json"), `{"Stop": [{"matcher": "*", "hooks": []}]}`)
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"hooks": {"Stop": [{"matcher": "*", "hooks": []}]}}`)
	if apmHookDriftIsReplayArtifact(home, "modified: .claude/settings.json") {
		t.Fatal("a sidecar with no APM-owned entries cannot vouch for the live config")
	}
}
