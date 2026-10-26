package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Mirrors the real ~/.apm/apm.yml: targets are hoisted to manifest level, never per entry.
const agentsServicesManifest = `name: omni
version: 1.0.0
targets:
- claude
- codex
dependencies:
  mcp:
  - name: litellm-tools
    registry: false
    transport: http
    url: https://api.invalid/mcp/
  - name: ghost-binary
    registry: false
    transport: stdio
    command: omni-test-absent-binary
  - name: neverinstalled
    registry: false
    transport: stdio
    command: omni-test-absent-binary
  lsp:
  - name: gopls
    command: omni-test-absent-binary
    extensionToLanguage:
      .go: go
  - name: pyright
    command: pyright-langserver
`

const agentsServicesLock = `dependencies: []
mcp_servers:
- litellm-tools
- ghost-binary
- stray-server
mcp_configs:
  litellm-tools:
    name: litellm-tools
    transport: http
    url: https://api.invalid/mcp/
  ghost-binary:
    name: ghost-binary
    transport: stdio
    command: omni-test-absent-binary
  stray-server:
    name: stray-server
    transport: stdio
    command: omni-test-absent-binary
lsp_servers:
- gopls
- stray-lsp
lsp_configs:
  gopls:
    name: gopls
    command: omni-test-absent-binary
  stray-lsp:
    name: stray-lsp
    command: omni-test-absent-binary
`

func serviceRow(t *testing.T, rows []AgentsServiceRow, name string) AgentsServiceRow {
	t.Helper()
	for _, row := range rows {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("no row named %q in %#v", name, rows)
	return AgentsServiceRow{}
}

func TestAgentsStatusJoinsMCPAndLSPSurfaces(t *testing.T) {
	a := setupAgentsWorkspace(t, agentsServicesManifest, agentsServicesLock)
	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}

	if len(status.MCP) != 4 {
		t.Fatalf("mcp rows = %#v", status.MCP)
	}
	remote := serviceRow(t, status.MCP, "litellm-tools")
	if remote.Status != AgentsPackageInstalled || remote.Detail != "http" {
		t.Fatalf("url-form row = %+v", remote)
	}
	if row := serviceRow(t, status.MCP, "ghost-binary"); row.Status != AgentsPackageUnavailable || row.SyncActionable {
		t.Fatalf("absent command row = %+v", row)
	}
	if row := serviceRow(t, status.MCP, "neverinstalled"); row.Status != AgentsPackageMissing || !row.SyncActionable {
		t.Fatalf("missing MCP row = %+v", row)
	}
	if got := serviceRow(t, status.MCP, "stray-server").Status; got != AgentsPackageOrphaned {
		t.Fatalf("lock-only status = %q", got)
	}

	if len(status.LSP) != 3 {
		t.Fatalf("lsp rows = %#v", status.LSP)
	}
	if row := serviceRow(t, status.LSP, "gopls"); row.Status != AgentsPackageUnavailable || row.SyncActionable {
		t.Fatalf("LSP absent command row = %+v", row)
	}
	if row := serviceRow(t, status.LSP, "pyright"); row.Status != AgentsPackageMissing || !row.SyncActionable {
		t.Fatalf("missing LSP row = %+v", row)
	}
	if got := serviceRow(t, status.LSP, "stray-lsp").Status; got != AgentsPackageOrphaned {
		t.Fatalf("lsp orphan status = %q", got)
	}
	if status.SyncActionable != 2 {
		t.Fatalf("sync actionable = %d, want two missing declared services", status.SyncActionable)
	}
}

func TestAgentsStatusTargetsComeFromTheManifest(t *testing.T) {
	a := setupAgentsWorkspace(t, agentsServicesManifest, agentsServicesLock)
	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}
	mcp := serviceRow(t, status.MCP, "litellm-tools")
	if len(mcp.Targets) != 2 || mcp.Targets[0] != "claude" || mcp.Targets[1] != "codex" {
		t.Fatalf("mcp targets = %v", mcp.Targets)
	}
	lsp := serviceRow(t, status.LSP, "gopls")
	if len(lsp.Targets) != 1 || lsp.Targets[0] != "claude" {
		t.Fatalf("lsp targets = %v", lsp.Targets)
	}
}

func TestAgentsStatusResolvableCommandIsInstalled(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "omni-test-present-binary")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "targets:\n- claude\ndependencies:\n  lsp:\n  - name: present\n    command: " + bin + "\n"
	lock := "dependencies: []\nlsp_servers:\n- present\nlsp_configs:\n  present:\n    name: present\n    command: " + bin + "\n"
	a := setupAgentsWorkspace(t, manifest, lock)
	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}
	row := serviceRow(t, status.LSP, "present")
	if row.Status != AgentsPackageInstalled || row.Detail != "omni-test-present-binary" {
		t.Fatalf("resolvable command row = %+v", row)
	}
}

func TestAgentsStatusWithoutServicesIsEmpty(t *testing.T) {
	a := setupAgentsWorkspace(t, agentsTestManifest, agentsTestLock)
	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.MCP) != 0 || len(status.LSP) != 0 {
		t.Fatalf("mcp %#v lsp %#v", status.MCP, status.LSP)
	}
	if len(status.Packages) != 5 {
		t.Fatalf("package join changed: %#v", status.Packages)
	}
}

func TestAgentsStatusSurfacesUnmanagedHarnessEntries(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	a := setupAgentsWorkspace(t, agentsServicesManifest, agentsServicesLock)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers": {"hand-added": {"command": "x"}, "litellm-tools": {"url": "https://api.invalid/mcp/"}}, "lspServers": {"hand-lsp": {}}}`)
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), "[mcp_servers.hand-added]\ncommand = \"x\"\n\n[mcp_servers.codex-only]\ncommand = \"y\"\n")
	writeFile(t, filepath.Join(home, ".omp", "agent", "mcp.json"), `{"mcpServers":{"hand-added":{"command":"x"},"omp-only":{"command":"z"},"litellm-tools":{"url":"https://api.invalid/mcp/"}}}`)

	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}
	unmanaged := serviceRow(t, status.MCP, "hand-added")
	if unmanaged.Status != AgentsPackageOrphaned || unmanaged.Detail != "unmanaged" {
		t.Fatalf("unmanaged row = %+v", unmanaged)
	}
	if got := agentsTargetsForTest(unmanaged); got != "claude,codex,omp" {
		t.Fatalf("unmanaged targets = %q", got)
	}
	if got := agentsTargetsForTest(serviceRow(t, status.MCP, "codex-only")); got != "codex" {
		t.Fatalf("codex-only targets = %q", got)
	}
	if got := serviceRow(t, status.MCP, "omp-only"); got.Status != AgentsPackageOrphaned || got.Detail != "unmanaged" || agentsTargetsForTest(got) != "omp" || strings.Join(got.Harnesses, ",") != "omp" {
		t.Fatalf("omp-only row = %+v", got)
	}
	if got := serviceRow(t, status.LSP, "hand-lsp"); got.Status != AgentsPackageOrphaned || got.Detail != "unmanaged" {
		t.Fatalf("unmanaged lsp row = %+v", got)
	}

	// A name apm already owns must not be duplicated by the harness overlay.
	var seen int
	for _, row := range status.MCP {
		if row.Name == "litellm-tools" {
			seen++
		}
	}
	if seen != 1 || serviceRow(t, status.MCP, "litellm-tools").Status != AgentsPackageInstalled {
		t.Fatalf("locked name duplicated by harness overlay: %#v", status.MCP)
	}
}

func TestAgentsStatusDetectsOmpMCPDrift(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	a := setupAgentsWorkspace(t, strings.Replace(agentsServicesManifest, "- codex", "- omp", 1), agentsServicesLock)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".omp", "agent", "mcp.json"), `{"mcpServers":{"litellm-tools":{"type":"http","url":"https://changed.invalid/mcp/"}}}`)
	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}
	row := serviceRow(t, status.MCP, "litellm-tools")
	if row.Status != AgentsPackageDrifted || !row.SyncActionable || strings.Join(row.Harnesses, ",") != "omp" {
		t.Fatalf("omp drift row = %+v", row)
	}
}

func TestMCPManifestPreservesExplicitEmptyTargets(t *testing.T) {
	for _, selection := range []string{"", "    targets: []\n", "    targets: [omp]\n"} {
		t.Run(strings.TrimSpace(selection), func(t *testing.T) {
			setupAgentsWorkspace(t, "dependencies:\n  mcp:\n  - name: probe\n    registry: false\n    transport: stdio\n    command: sh\n"+selection, "")
			manifest, _, err := readAPMWorkspace()
			if err != nil {
				t.Fatal(err)
			}
			dep := manifest.Dependencies.MCP[0]
			if (dep.Targets == nil) != (selection == "") {
				t.Fatal("target selection presence was lost while reading")
			}
			rendered, err := encodeAPMManifest(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if selection == "    targets: []\n" && !strings.Contains(rendered, "targets: []") {
				t.Fatal("explicit empty target selection was omitted while writing")
			}
			if selection == "" && strings.Contains(rendered, "targets:") {
				t.Fatal("omitted target selection became explicit while writing")
			}
		})
	}
}

func TestAgentsStatusMCPDriftRespectsServerTargets(t *testing.T) {
	for _, tc := range []struct {
		name, active, selected, claudeURL, ompURL, targets string
		want                                               AgentsPackageStatus
	}{
		{"omp drift with matching Claude", "[claude, omp]", "", "https://api.invalid", "https://changed.invalid", "claude,omp", AgentsPackageDrifted},
		{"omp only ignores unrelated Claude", "[claude, omp]", "    targets: [omp]\n", "https://changed.invalid", "https://api.invalid", "omp", AgentsPackageInstalled},
		{"explicit empty targets", "[claude, omp]", "    targets: []\n", "https://changed.invalid", "https://changed.invalid", "", AgentsPackageInstalled},
		{"intersect active targets", "[claude]", "    targets: [omp]\n", "https://changed.invalid", "https://changed.invalid", "", AgentsPackageInstalled},
		{"unset targets inherit active", "[claude]", "", "https://api.invalid", "https://changed.invalid", "claude", AgentsPackageInstalled},
		{"unset active targets select server", "", "    targets: [omp]\n", "https://changed.invalid", "https://api.invalid", "omp", AgentsPackageInstalled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PI_CODING_AGENT_DIR", "")
			manifest := "dependencies:\n  mcp:\n  - name: probe\n    registry: false\n    transport: http\n    url: https://api.invalid\n" + tc.selected
			if tc.active != "" {
				manifest = "targets: " + tc.active + "\n" + manifest
			}
			a := setupAgentsWorkspace(t, manifest, "mcp_servers: [probe]\nmcp_configs:\n  probe:\n    transport: http\n    url: https://api.invalid\n")
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"probe":{"url":"`+tc.claudeURL+`"}}}`)
			writeFile(t, filepath.Join(home, ".omp", "agent", "mcp.json"), `{"mcpServers":{"probe":{"url":"`+tc.ompURL+`"}}}`)
			status, err := a.AgentsStatus()
			if err != nil {
				t.Fatal(err)
			}
			row := serviceRow(t, status.MCP, "probe")
			if row.Status != tc.want || row.SyncActionable != (tc.want == AgentsPackageDrifted) || strings.Join(row.Targets, ",") != tc.targets {
				t.Fatalf("status=%s actionable=%v targets=%v", row.Status, row.SyncActionable, row.Targets)
			}
			if tc.selected == "    targets: []\n" && row.Targets == nil {
				t.Fatal("explicit empty selection became implicit all-target selection")
			}
		})
	}
}

func TestOmpMCPDriftExpandsNativeEnvironmentReferences(t *testing.T) {
	t.Setenv("OMNI_TEST_OMP_VALUE", "value")
	t.Setenv("OMNI_TEST_OMP_EMPTY", "")
	t.Setenv("OMNI_TEST_OMP_UNSET", "")
	if err := os.Unsetenv("OMNI_TEST_OMP_UNSET"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, locked, deployed string
		want                   bool
	}{
		{"set", "${env:OMNI_TEST_OMP_VALUE}", "${OMNI_TEST_OMP_VALUE}", false},
		{"unset", "${env:OMNI_TEST_OMP_UNSET}", "${OMNI_TEST_OMP_UNSET}", false},
		{"empty", "${env:OMNI_TEST_OMP_EMPTY}", "${OMNI_TEST_OMP_EMPTY}", false},
		{"unset default", "fallback", "${OMNI_TEST_OMP_UNSET:-fallback}", false},
		{"empty default", "fallback", "${OMNI_TEST_OMP_EMPTY:-fallback}", false},
		{"set default", "value", "${OMNI_TEST_OMP_VALUE:-fallback}", false},
		{"changed", "${env:OMNI_TEST_OMP_VALUE}", "changed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range []string{"command", "args", "url"} {
				cfg := apmServiceConfig{}
				deployed := harnessMCPConfig{}
				switch field {
				case "command":
					cfg.Command, deployed.Command = tc.locked, tc.deployed
				case "args":
					cfg.Args, deployed.Args = []string{tc.locked}, []string{tc.deployed}
				case "url":
					cfg.URL, deployed.URL = "https://example.invalid/"+tc.locked, "https://example.invalid/"+tc.deployed
				}
				if got := agentsServiceDriftedOnHarness(cfg, deployed, harnessOmp); got != tc.want {
					t.Fatalf("%s drift=%v, want %v", field, got, tc.want)
				}
			}
		})
	}
}

func TestAgentsStatusPropagatesHarnessNotices(t *testing.T) {
	a := setupAgentsWorkspace(t, agentsServicesManifest, agentsServicesLock)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".claude.json"), "{not json")
	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Notices) != 1 || !strings.Contains(status.Notices[0], "claude:") {
		t.Fatalf("notices = %v", status.Notices)
	}
}

func agentsTargetsForTest(row AgentsServiceRow) string { return strings.Join(row.Targets, ",") }

func driftFixture(t *testing.T, lockConfig, claudeConfig string) AgentsStatus {
	t.Helper()
	manifest := "targets:\n- claude\ndependencies:\n  mcp:\n  - name: probe\n    registry: false\n    transport: stdio\n"
	lock := "dependencies: []\nmcp_servers:\n- probe\nmcp_configs:\n  probe:\n    name: probe\n" + lockConfig
	a := setupAgentsWorkspace(t, manifest, lock)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if claudeConfig != "" {
		writeFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers": {"probe": `+claudeConfig+`}}`)
	}
	status, err := a.AgentsStatus()
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func TestAgentsStatusDetectsDeployedMCPDrift(t *testing.T) {
	t.Setenv("OMNI_TEST_MCP_ARG", "--real")
	for name, tc := range map[string]struct {
		lock, claude string
		want         AgentsPackageStatus
	}{
		"tampered command": {"    command: sh\n", `{"command": "TAMPERED"}`, AgentsPackageDrifted},
		"tampered arg":     {"    command: sh\n    args:\n    - --one\n", `{"command": "sh", "args": ["--two"]}`, AgentsPackageDrifted},
		"dropped arg":      {"    command: sh\n    args:\n    - --one\n", `{"command": "sh"}`, AgentsPackageDrifted},
		"changed url":      {"    transport: http\n    url: https://api.invalid/mcp/\n", `{"url": "https://elsewhere.invalid/"}`, AgentsPackageDrifted},
		"matching":         {"    command: sh\n", `{"command": "sh"}`, AgentsPackageInstalled},
		"env set":          {"    command: sh\n    args:\n    - ${env:OMNI_TEST_MCP_ARG}\n", `{"command": "sh", "args": ["--real"]}`, AgentsPackageInstalled},
		"env unset":        {"    command: sh\n    args:\n    - ${env:OMNI_TEST_MCP_ABSENT}\n", `{"command": "sh", "args": ["--anything"]}`, AgentsPackageInstalled},
		"not on claude":    {"    command: sh\n", "", AgentsPackageInstalled},
	} {
		t.Run(name, func(t *testing.T) {
			status := driftFixture(t, tc.lock, tc.claude)
			row := serviceRow(t, status.MCP, "probe")
			if row.Status != tc.want || row.SyncActionable != (tc.want == AgentsPackageDrifted) {
				t.Fatalf("row = %+v, want status %q actionable=%v", row, tc.want, tc.want == AgentsPackageDrifted)
			}
			wantActionable := 0
			if tc.want == AgentsPackageDrifted {
				wantActionable = 1
			}
			if status.SyncActionable != wantActionable {
				t.Fatalf("aggregate actionable = %d for row %+v", status.SyncActionable, row)
			}
		})
	}
}

func TestAgentsStatusDriftBeatsUnavailable(t *testing.T) {
	status := driftFixture(t, "    command: omni-test-absent-binary\n", `{"command": "TAMPERED"}`)
	if got := serviceRow(t, status.MCP, "probe").Status; got != AgentsPackageDrifted {
		t.Fatalf("status = %q", got)
	}
}

func TestAgentsServiceRowsSortByStatusRank(t *testing.T) {
	rows := joinAPMServices(agentsServiceInput{
		declared: []agentsServiceDecl{
			{name: "d-missing"},
			{name: "a-installed", command: "sh"},
			{name: "b-drifted", command: "sh"},
			{name: "c-unavailable", command: "omni-test-absent-binary"},
		},
		locked: []string{"a-installed", "b-drifted", "c-unavailable"},
		configs: map[string]apmServiceConfig{
			"a-installed":   {Command: "sh"},
			"b-drifted":     {Command: "sh"},
			"c-unavailable": {Command: "omni-test-absent-binary"},
		},
		deployedConfigs: map[string]map[string]harnessMCPConfig{
			"a-installed": {harnessClaude: {Command: "sh"}},
			"b-drifted":   {harnessClaude: {Command: "TAMPERED"}},
		},
		deployed: map[string][]string{"e-orphaned": {"codex"}},
	})
	want := []AgentsPackageStatus{
		AgentsPackageInstalled, AgentsPackageDrifted, AgentsPackageUnavailable,
		AgentsPackageMissing, AgentsPackageOrphaned,
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %#v", rows)
	}
	for i, status := range want {
		if rows[i].Status != status {
			t.Fatalf("row %d = %+v, want %q", i, rows[i], status)
		}
	}
}
