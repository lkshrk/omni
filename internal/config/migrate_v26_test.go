package config_test

import (
	"strings"
	"testing"

	"github.com/lkshrk/omni/internal/config"
)

func TestMigrateV25ToV26KeepsContent(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json",
		`{"version":25,"tools":{"ripgrep":{"providers":[{"provider":"pacman"}]}},`+validAgentsIgnoredBlock+`}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load v25 config: %v", err)
	}
	if cfg.Version != 26 {
		t.Fatalf("version = %d, want 26", cfg.Version)
	}
	if spec, ok := cfg.Tools["ripgrep"]; !ok || len(spec.Providers) != 1 || spec.Providers[0].Provider != "pacman" {
		t.Fatalf("tools = %#v, want ripgrep via pacman preserved", cfg.Tools)
	}
	if cfg.Agents == nil || len(cfg.Agents.Ignored) != 1 {
		t.Fatalf("cfg.Agents = %#v, want the ignored entry preserved", cfg.Agents)
	}
}

func TestMigrateV25ToV26RejectsRetiredAgentsBlock(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json",
		`{"version":25,"agents":{"ai":{"skills":[]}}}`)
	if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), `"$.agents.ai"`) {
		t.Fatalf("Load error = %v, want removed field $.agents.ai", err)
	}
}

func TestV26AcceptsAURProvider(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json",
		`{"version":26,"tools":{"walker":{"providers":[{"provider":"aur","package":"walker-bin"}]}}}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load v26 config with aur: %v", err)
	}
	if got := cfg.Tools["walker"].Providers; len(got) != 1 || got[0].Provider != "aur" || got[0].Package != "walker-bin" {
		t.Fatalf("walker providers = %#v, want aur/walker-bin", got)
	}
	if !strings.Contains(config.SchemaURL, ".v26.") {
		t.Fatalf("SchemaURL = %q, want v26", config.SchemaURL)
	}
}
