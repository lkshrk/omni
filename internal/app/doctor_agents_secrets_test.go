package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const secretsTestLock = `mcp_configs:
  gh-server:
    command: gh-mcp
    env:
      GH: ${GH}
      MODE: fast
  remote:
    url: https://example.test/mcp
    headers:
      Authorization: Bearer ${TOKEN}
mcp_target_servers:
  claude: [gh-server]
  omp: [remote]
  opencode: [gh-server, remote]
  codex: [gh-server]
`

func secretsTestSetup(t *testing.T) (string, apmLockfile) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	var lock apmLockfile
	if err := yaml.Unmarshal([]byte(secretsTestLock), &lock); err != nil {
		t.Fatal(err)
	}
	return home, lock
}

func writeSecretsTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runSecretLiterals(t *testing.T, home string, lock apmLockfile) DoctorCheck {
	t.Helper()
	result := &DoctorResult{}
	(&App{}).doctorAgentsSecretLiterals(result, home, lock)
	if len(result.Checks) != 1 {
		t.Fatalf("checks = %+v", result.Checks)
	}
	got := result.Checks[0]
	if got.ID != "agents" || got.Label != "MCP secrets" {
		t.Fatalf("check identity = %+v", got)
	}
	return got
}

func assertNoSecretOutput(t *testing.T, check DoctorCheck, secrets ...string) {
	t.Helper()
	for _, line := range append([]string{check.Message}, check.Details...) {
		for _, secret := range secrets {
			if strings.Contains(line, secret) {
				t.Fatalf("output leaks %q: %+v", secret, check)
			}
		}
	}
}

func TestDoctorAgentsSecretLiteralsClaudeReferenceOK(t *testing.T) {
	home, lock := secretsTestSetup(t)
	writeSecretsTestFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"gh-server":{"env":{"GH":"${GH}","MODE":"fast"}}}}`)
	got := runSecretLiterals(t, home, lock)
	if got.Status != DoctorStatusOK || got.Message != "MCP env and header values are references" {
		t.Fatalf("check = %+v", got)
	}
}

func TestDoctorAgentsSecretLiteralsClaudeLiteralWarns(t *testing.T) {
	home, lock := secretsTestSetup(t)
	writeSecretsTestFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"gh-server":{"env":{"GH":"ghp_x","MODE":"fast"}}}}`)
	got := runSecretLiterals(t, home, lock)
	if got.Status != DoctorStatusWarn || got.Message != "possibly resolved secret values in client configs" {
		t.Fatalf("check = %+v", got)
	}
	if !slices.Equal(got.Details, []string{"gh-server (claude) env GH"}) {
		t.Fatalf("details = %q", got.Details)
	}
	assertNoSecretOutput(t, got, "ghp_x")
}

func TestDoctorAgentsSecretLiteralsCodexEnvTable(t *testing.T) {
	home, lock := secretsTestSetup(t)
	codex := filepath.Join(home, ".codex", "config.toml")
	writeSecretsTestFile(t, codex, "[mcp_servers.gh-server]\ncommand = \"gh-mcp\"\nenv_vars = [\"GH\"]\n\n[mcp_servers.gh-server.env]\nMODE = \"fast\"\n")
	if got := runSecretLiterals(t, home, lock); got.Status != DoctorStatusOK {
		t.Fatalf("lock literal check = %+v", got)
	}

	writeSecretsTestFile(t, codex, "[mcp_servers.gh-server.env]\nGH = \"ghp_x\" # pasted\nMODE = \"fast\"\n\n[mcp_servers.other.env]\nGH = \"ghp_y\"\n")
	got := runSecretLiterals(t, home, lock)
	if got.Status != DoctorStatusWarn || !slices.Equal(got.Details, []string{"gh-server (codex) env GH"}) {
		t.Fatalf("check = %+v", got)
	}
	assertNoSecretOutput(t, got, "ghp_x", "ghp_y")
}

func TestDoctorAgentsSecretLiteralsLockLiteralOK(t *testing.T) {
	home, lock := secretsTestSetup(t)
	writeSecretsTestFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"gh-server":{"env":{"GH":"${GH}","MODE":"fast"}}}}`)
	writeSecretsTestFile(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"mcp":{"gh-server":{"environment":{"GH":"{env:GH}","MODE":"fast"}},"remote":{"headers":{"Authorization":"Bearer {env:TOKEN}"}}}}`)
	if got := runSecretLiterals(t, home, lock); got.Status != DoctorStatusOK {
		t.Fatalf("check = %+v", got)
	}
}

func TestDoctorAgentsSecretLiteralsHeadersPerClientForm(t *testing.T) {
	home, lock := secretsTestSetup(t)
	writeSecretsTestFile(t, filepath.Join(home, ".omp", "agent", "mcp.json"), `{"mcpServers":{"remote":{"headers":{"Authorization":"Bearer sk-live"}}}}`)
	writeSecretsTestFile(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"mcp":{"remote":{"headers":{"Authorization":"${TOKEN}"}}}}`)
	got := runSecretLiterals(t, home, lock)
	want := []string{"remote (omp) headers Authorization", "remote (opencode) headers Authorization"}
	if got.Status != DoctorStatusWarn || !slices.Equal(got.Details, want) {
		t.Fatalf("check = %+v", got)
	}
	assertNoSecretOutput(t, got, "sk-live")
}

func TestDoctorAgentsSecretLiteralsUnparseableConfig(t *testing.T) {
	home, lock := secretsTestSetup(t)
	writeSecretsTestFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"gh-server":{"env":{"GH":"ghp_x"`)
	got := runSecretLiterals(t, home, lock)
	if got.Status != DoctorStatusWarn || len(got.Details) != 1 || !strings.Contains(got.Details[0], "claude") {
		t.Fatalf("check = %+v", got)
	}
	assertNoSecretOutput(t, got, "ghp_x")
}

func TestReadAPMLockfileParseErrorOmitsContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeSecretsTestFile(t, filepath.Join(home, ".apm", "apm.lock.yaml"), "mcp_configs:\n  remote:\n    headers: \"Bearer abc\"\n")
	_, err := readAPMLockfile()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if strings.Contains(err.Error(), "Bearer abc") || !strings.Contains(err.Error(), "apm.lock.yaml:3") {
		t.Fatalf("error = %q", err)
	}
}
