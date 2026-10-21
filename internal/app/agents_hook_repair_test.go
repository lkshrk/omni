package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func linkedSettings(t *testing.T, raw string) (home, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinked dotfiles are POSIX-only")
	}
	home = t.TempDir()
	target = filepath.Join(home, "dotfiles", "settings.json")
	writeFile(t, target, `{"old":true}`)
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(raw, link); err != nil {
		t.Fatal(err)
	}
	return home, link, target
}

func simulateAPMRename(t *testing.T, link, content string) {
	t.Helper()
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	writeFile(t, link, content)
}

func TestRestoreSettingsLinkKeepsRelativeTarget(t *testing.T) {
	const raw = "../dotfiles/settings.json"
	home, link, target := linkedSettings(t, raw)
	captured := captureSettingsLink(home)
	simulateAPMRename(t, link, `{"new":true}`)

	if err := captured.restore(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(link); err != nil || got != raw {
		t.Fatalf("readlink = %q, %v; want %q", got, err, raw)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("target perm = %v, %v", info, err)
	}
	if content, _ := os.ReadFile(link); string(content) != `{"new":true}` {
		t.Fatalf("content through link = %s", content)
	}
	entries, _ := os.ReadDir(filepath.Dir(link))
	if len(entries) != 1 {
		t.Fatalf("leftover files in .claude: %v", entries)
	}
}

func TestRestoreSettingsLinkMissingTargetIsNoop(t *testing.T) {
	home, link, _ := linkedSettings(t, "../dotfiles/absent.json")
	captured := captureSettingsLink(home)
	simulateAPMRename(t, link, `{"new":true}`)

	if err := captured.restore(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("settings.json changed: %v, %v", info, err)
	}
}

func TestRestoreSettingsLinkLeavesIntactLink(t *testing.T) {
	home, link, target := linkedSettings(t, "../dotfiles/settings.json")
	if err := captureSettingsLink(home).restore(); err != nil {
		t.Fatal(err)
	}
	if got, _ := filepath.EvalSymlinks(link); got != target {
		t.Fatalf("link resolves to %q", got)
	}
}

func TestDeployHookPluginManifests(t *testing.T) {
	home := t.TempDir()
	modules := filepath.Join(home, ".apm", "apm_modules")
	manifest := `{"name":"codex"}`
	writeFile(t, filepath.Join(modules, "openai", "codex-plugin-cc", "plugins", "codex", ".claude-plugin", "plugin.json"), manifest)
	lock := apmLockfile{Dependencies: []apmLockDep{
		{RepoURL: "openai/codex-plugin-cc", VirtualPath: "plugins/codex", DeployedFiles: []string{
			".claude/commands/review.md",
			".claude/hooks/codex/scripts/lib/app-server.mjs",
			".claude/hooks/../escape/x.sh",
			`.claude/hooks/a\b/x.sh`,
		}},
		{RepoURL: "x/no-module", DeployedFiles: []string{".claude/hooks/x/run.sh"}},
	}}

	if err := deployHookPluginManifests(home, lock, modules); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(home, ".claude", "hooks", "codex", ".claude-plugin", "plugin.json")); err != nil || string(raw) != manifest {
		t.Fatalf("manifest = %s, %v", raw, err)
	}
	for _, dir := range []string{"x", `a\b`} {
		if _, err := os.Stat(filepath.Join(home, ".claude", "hooks", dir)); !os.IsNotExist(err) {
			t.Fatalf("hooks/%s written: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "escape")); !os.IsNotExist(err) {
		t.Fatalf("manifest escaped hooks dir: %v", err)
	}
}

func TestDoctorDuplicateHooks(t *testing.T) {
	home := t.TempDir()
	entry := `{"hooks":[{"type":"command","command":"a"}]}`
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"hooks":{"Stop":[`+entry+`,`+entry+`],"SessionStart":[`+entry+`]}}`)
	result := &DoctorResult{}
	doctorDuplicateHooks(result, home)
	if len(result.Checks) != 1 || result.Checks[0].Status != DoctorStatusWarn || len(result.Checks[0].Details) != 1 {
		t.Fatalf("checks = %+v", result.Checks)
	}

	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"hooks":{"Stop":[`+entry+`]}}`)
	result = &DoctorResult{}
	doctorDuplicateHooks(result, home)
	if len(result.Checks) != 1 || result.Checks[0].Status != DoctorStatusOK {
		t.Fatalf("checks = %+v", result.Checks)
	}
}
