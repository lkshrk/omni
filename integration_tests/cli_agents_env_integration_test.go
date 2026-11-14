//go:build integration

package integration_test

import (
	"path/filepath"
	"testing"
)

func TestCLIBinaryAgentsEnvListsReferencedNamesOnly(t *testing.T) {
	root, home, cache, env := newCLIBinarySandbox(t)
	writeIntegrationFile(t, filepath.Join(home, ".apm", "apm.lock.yaml"), `mcp_configs:
  litellm-tools:
    headers:
      Authorization: Bearer ${LITELLM_API}
mcp_target_servers:
  claude: [litellm-tools]
`)
	configPath := filepath.Join(root, "settings.json")
	writeIntegrationFile(t, configPath, `{"version":25}`)

	out := runOmniOutput(t, buildOmniBinary(t), root, env, "--config", configPath, "--cache-dir", cache, "agents", "env")
	if want := "claude: LITELLM_API\n"; out != want {
		t.Fatalf("agents env = %q, want %q", out, want)
	}
}
