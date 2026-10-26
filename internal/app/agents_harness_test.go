package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexFixture = `[agents]
model = "gpt"

[mcp_servers.codebase-memory-mcp]
command = "codebase-memory-mcp"

[mcp_servers.codebase-memory-mcp.env]
TOKEN = "x"

[mcp_servers.codebase-memory-mcp.tools.query_graph]
enabled = true

[mcp_servers.litellm-tools]
url = "https://api.invalid/mcp/"

[mcp_servers.litellm-tools.http_headers]
x-key = "y"

  [mcp_servers.padded]
command = "padded"

[mcp_servers.commented]  # inline note
command = "commented"

[mcp_servers."quoted.name"]
command = "quoted"

[mcp_servers.disabled]
enabled = false

[plugins."context-mode@context-mode"]
enabled = true
`

func writeHarness(t *testing.T, home, rel, content string) {
	t.Helper()
	writeFile(t, filepath.Join(home, rel), content)
}

func TestReadHarnessDeploymentsWithoutFilesIsEmpty(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	got := readHarnessDeployments(t.TempDir())
	if len(got.MCP) != 0 || len(got.LSP) != 0 || len(got.MCPConfigs) != 0 || len(got.Notices) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestReadHarnessDeploymentsReadsClaudeSurfaces(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	writeHarness(t, home, ".claude.json", `{
  "numStartups": 42,
  "mcpServers": {
    "litellm-tools": {"url": "https://api.invalid/mcp/"},
    "context-mode": {"command": "node", "args": ["./start.mjs"]}
  },
  "lspServers": {"gopls": {"command": "gopls"}}
}`)
	got := readHarnessDeployments(home)
	if len(got.Notices) != 0 {
		t.Fatalf("notices = %v", got.Notices)
	}
	if len(got.MCP) != 2 || len(got.MCP["litellm-tools"]) != 1 || got.MCP["litellm-tools"][0] != harnessClaude {
		t.Fatalf("mcp = %#v", got.MCP)
	}
	if len(got.LSP["gopls"]) != 1 || got.LSP["gopls"][0] != harnessClaude {
		t.Fatalf("lsp = %#v", got.LSP)
	}
	cfg := got.MCPConfigs["context-mode"][harnessClaude]
	if cfg.Command != "node" || len(cfg.Args) != 1 || cfg.Args[0] != "./start.mjs" {
		t.Fatalf("config = %+v", cfg)
	}
	if got.MCPConfigs["litellm-tools"][harnessClaude].URL != "https://api.invalid/mcp/" {
		t.Fatalf("url config = %+v", got.MCPConfigs["litellm-tools"])
	}
}

func TestReadHarnessDeploymentsParsesCodexTableHeadersOnly(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	writeHarness(t, home, ".codex/config.toml", codexFixture)
	got := readHarnessDeployments(home)
	if len(got.Notices) != 0 {
		t.Fatalf("notices = %v", got.Notices)
	}
	want := []string{"codebase-memory-mcp", "litellm-tools", "padded", "commented", "quoted.name", "disabled"}
	if len(got.MCP) != len(want) {
		t.Fatalf("mcp names = %#v", got.MCP)
	}
	for _, name := range want {
		if len(got.MCP[name]) != 1 || got.MCP[name][0] != harnessCodex {
			t.Fatalf("missing codex entry %q in %#v", name, got.MCP)
		}
	}
	for name := range got.MCP {
		if strings.HasSuffix(name, ".env") || strings.HasSuffix(name, ".http_headers") || strings.Contains(name, ".tools.") {
			t.Fatalf("nested table parsed as a server: %q", name)
		}
	}
}

func TestReadHarnessDeploymentsMergesLabelsAcrossFiles(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	writeHarness(t, home, ".claude.json", `{"mcpServers": {"litellm-tools": {"url": "https://api.invalid/mcp/"}}}`)
	writeHarness(t, home, ".codex/config.toml", "[mcp_servers.litellm-tools]\nurl = \"https://api.invalid/mcp/\"\n")
	writeHarness(t, home, ".omp/agent/mcp.json", `{"mcpServers":{"litellm-tools":{"url":"https://omp.invalid/mcp"}}}`)
	got := readHarnessDeployments(home)
	labels := got.MCP["litellm-tools"]
	if len(labels) != 3 || labels[0] != harnessClaude || labels[1] != harnessCodex || labels[2] != harnessOmp {
		t.Fatalf("labels = %v", labels)
	}
	if got.MCPConfigs["litellm-tools"][harnessClaude].URL != "https://api.invalid/mcp/" || got.MCPConfigs["litellm-tools"][harnessOmp].URL != "https://omp.invalid/mcp" {
		t.Fatalf("per-harness drift evidence lost: %#v", got.MCPConfigs)
	}
}

func TestReadHarnessDeploymentsDegradesToNotices(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	writeHarness(t, home, ".claude.json", "{not json")
	got := readHarnessDeployments(home)
	if len(got.MCP) != 0 || len(got.Notices) != 1 || !strings.Contains(got.Notices[0], "could not be parsed") {
		t.Fatalf("got %#v", got)
	}
}

func TestReadHarnessDeploymentsSkipsOversizedClaudeConfig(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(path, make([]byte, harnessClaudeMaxKiB+1), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readHarnessDeployments(home)
	if len(got.MCP) != 0 || len(got.Notices) != 1 || !strings.Contains(got.Notices[0], "exceeds 4MiB") {
		t.Fatalf("got %#v", got)
	}
}

func TestReadHarnessDeploymentsNoticesUnreadableFile(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	writeHarness(t, home, ".claude.json", "{}")
	if err := os.Chmod(filepath.Join(home, ".claude.json"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, ".claude.json"), 0o600) })
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	got := readHarnessDeployments(home)
	if len(got.Notices) != 1 || !strings.Contains(got.Notices[0], "unreadable") {
		t.Fatalf("notices = %v", got.Notices)
	}
}

func TestReadHarnessDeploymentsReadsNativeOmpConfig(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	writeHarness(t, home, ".omp/agent/mcp.json", `{
	  "$schema":"https://example.invalid/mcp.schema.json",
	  "disabledServers":["context-mode"],
	  "enabledServers":["litellm-tools"],
	  "mcpServers":{
	    "context-mode":{"type":"stdio","command":"node","args":["context-mode"],"instructions":false,"disabled":null},
	    "litellm-tools":{"type":"http","url":"https://api.invalid/mcp/","headers":{"Authorization":"private"}},
	    "unrelated":{"command":"local","enabled":false},
	    " ":{"command":"ignored"}
	  },
	  "lspServers":{"gopls":{"command":"gopls"}}
	}`)
	got := readHarnessDeployments(home)
	if len(got.Notices) != 0 || len(got.MCP) != 3 || len(got.LSP) != 0 {
		t.Fatalf("inventory = %#v", got)
	}
	for _, name := range []string{"context-mode", "litellm-tools", "unrelated"} {
		if labels := got.MCP[name]; len(labels) != 1 || labels[0] != harnessOmp {
			t.Fatalf("%s labels = %v", name, labels)
		}
	}
	if cfg := got.MCPConfigs["context-mode"][harnessOmp]; cfg.Command != "node" || len(cfg.Args) != 1 || cfg.Args[0] != "context-mode" {
		t.Fatalf("stdio config = %#v", cfg)
	}
	if got.MCPConfigs["litellm-tools"][harnessOmp].URL != "https://api.invalid/mcp/" {
		t.Fatalf("HTTP config = %#v", got.MCPConfigs["litellm-tools"])
	}
}

func TestReadHarnessDeploymentsHonorsOmpAgentDir(t *testing.T) {
	for _, override := range []string{"absolute", "relative", "~/literal", " "} {
		t.Run(override, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			t.Chdir(root)
			dir := override
			if override == "absolute" {
				dir = filepath.Join(root, "custom-agent")
			}
			t.Setenv("PI_CODING_AGENT_DIR", dir)
			writeHarness(t, home, ".omp/agent/mcp.json", `{"mcpServers":{"default":{"command":"ignored"}}}`)
			writeFile(t, filepath.Join(dir, "mcp.json"), `{"mcpServers":{"custom":{"command":"custom"}}}`)
			got := readHarnessDeployments(home)
			if len(got.MCP) != 1 || len(got.MCP["custom"]) != 1 || got.MCP["custom"][0] != harnessOmp {
				t.Fatalf("inventory = %#v", got)
			}
		})
	}
}

func TestReadHarnessDeploymentsMalformedOmpConfig(t *testing.T) {
	for _, content := range []string{`{not json`, `{"mcpServers":[]}`, `{"mcpServers":{"bad":{"args":"not-an-array"}}}`} {
		t.Run(content, func(t *testing.T) {
			t.Setenv("PI_CODING_AGENT_DIR", "")
			home := t.TempDir()
			writeHarness(t, home, ".omp/agent/mcp.json", content)
			writeHarness(t, home, ".claude.json", `{"mcpServers":{"claude-only":{"command":"ok"}}}`)
			got := readHarnessDeployments(home)
			if len(got.MCP) != 1 || len(got.MCP["claude-only"]) != 1 || len(got.Notices) != 1 || !strings.HasPrefix(got.Notices[0], "omp:") || !strings.Contains(got.Notices[0], "could not be parsed") {
				t.Fatalf("inventory = %#v", got)
			}
		})
	}
}
