//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vttest"

	"github.com/lkshrk/omni/internal/config"
)

// Opening the TUI repairs dotfile links before the user presses anything, and a local
// file newer than its repo source is adopted rather than left alone.
func TestTUILaunchSyncAdoptsAModifiedDotEntry(t *testing.T) {
	bin := buildOmniBinary(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cache := filepath.Join(root, "cache")
	configPath := filepath.Join(root, "settings.json")
	repo := filepath.Join(home, "dotfiles")
	target := filepath.Join(home, ".config", "nvim", "init.lua")
	source := filepath.Join(repo, "dotfiles", "nvim", ".config", "nvim", "init.lua")
	env := isolatedTUIEnv(t, home, cache)

	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	initDotsRepo(t, repo, env)
	writeIntegrationFile(t, source, "repo version\n")
	runCommand(t, repo, env, "git", "add", ".")
	runCommand(t, repo, env, "git", "commit", "-m", "add nvim dotfile")
	writeIntegrationFile(t, target, "local version\n")
	// Uncommitted repo state, so the pre-sync backup has something to preserve.
	writeIntegrationFile(t, filepath.Join(repo, "notes.md"), "unsaved\n")
	repoTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(source, repoTime, repoTime); err != nil {
		t.Fatalf("set repo timestamp: %v", err)
	}
	localTime := repoTime.Add(time.Hour)
	if err := os.Chtimes(target, localTime, localTime); err != nil {
		t.Fatalf("set local timestamp: %v", err)
	}
	if err := config.Save(configPath, &config.RootConfig{
		Version:  config.CurrentVersion,
		Settings: config.Settings{DotsRepo: repo},
		Hosts:    map[string][]string{"testhost": {}},
		Groups: []*config.GroupConfig{{
			Name:    "testhost",
			Special: "host",
			Dots:    []config.DotEntry{{Name: "nvim", Path: target}},
		}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	runOmniCommand(t, bin, root, env, "--config", configPath, "--cache-dir", cache, "hosts", "ensure", "testhost")

	runTUI(t, bin, root, env, []string{"--config", configPath, "--cache-dir", cache}, func(term *vttest.Terminal) string {
		waitForRequiredScreen(t, term, 6*time.Second, screenHas("Dashboard", "Tools"), "TUI did not render main tabs")
		writeTUIKeys(t, term, "\t", "\t")
		return waitForRequiredScreen(t, term, 10*time.Second, func(text string) bool {
			return strings.Contains(text, "nvim") && strings.Contains(strings.ToLower(text), "synced")
		}, "launch sync did not report the entry as synced")
	})

	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("target is %s, want a symlink into the repo", info.Mode())
	}
	link, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("read target link: %v", err)
	}
	resolved := link
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Clean(filepath.Join(filepath.Dir(target), resolved))
	}
	if wantSource, gotSource := mustEvalSymlinks(t, source), mustEvalSymlinks(t, resolved); gotSource != wantSource {
		t.Fatalf("target links to %q, want %q", gotSource, wantSource)
	}
	assertRegularFileContent(t, source, "local version\n")

	// The backup lands on its own ref, never on the checked-out branch.
	log := runCommandOutput(t, repo, env, "git", "log", "--oneline", "refs/heads/omni/backup")
	if !strings.Contains(log, "dots: pre-sync nvim") {
		t.Fatalf("no pre-sync backup commit before the repo was rewritten:\n%s", log)
	}
	if backed := runCommandOutput(t, repo, env, "git", "show", "refs/heads/omni/backup:notes.md"); !strings.Contains(backed, "unsaved") {
		t.Fatalf("backup commit does not carry the uncommitted repo state: %q", backed)
	}
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %q: %v", path, err)
	}
	return resolved
}
