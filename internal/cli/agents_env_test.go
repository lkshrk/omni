package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentsEnvPrintsVariableNamesPerClient(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	if err := os.MkdirAll(filepath.Join(home, ".apm"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := `mcp_configs:
  litellm-tools:
    headers:
      Authorization: Bearer ${LITELLM_API}
  github:
    env:
      TOKEN: ${GITHUB_PERSONAL_ACCESS_TOKEN}
mcp_target_servers:
  claude: [litellm-tools, github]
  codex: [litellm-tools]
  omp: []
`
	if err := os.WriteFile(filepath.Join(home, ".apm", "apm.lock.yaml"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "settings.json")
	if err := os.WriteFile(cfgPath, []byte(`{"version":25}`), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runRootCommand(t, "--config", cfgPath, "--cache-dir", t.TempDir(), "--state-dir", t.TempDir(), "agents", "env")
	if err != nil {
		t.Fatalf("env failed: %v\n%s", err, output)
	}
	want := "claude: GITHUB_PERSONAL_ACCESS_TOKEN LITELLM_API\ncodex: LITELLM_API\n"
	if output != want {
		t.Fatalf("output = %q, want %q", output, want)
	}
}

func TestAgentsEnvReportsNothingReferenced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	cfgPath := filepath.Join(home, "settings.json")
	if err := os.WriteFile(cfgPath, []byte(`{"version":25}`), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runRootCommand(t, "--config", cfgPath, "--cache-dir", t.TempDir(), "--state-dir", t.TempDir(), "agents", "env")
	if err != nil {
		t.Fatalf("env failed: %v\n%s", err, output)
	}
	if want := "No MCP environment variables referenced.\n"; output != want {
		t.Fatalf("output = %q, want %q", output, want)
	}
}
