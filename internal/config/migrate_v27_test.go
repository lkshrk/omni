package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lkshrk/omni/internal/config"
)

const v27AgentsBlock = `"agents":{
  "targets":["claude","codex"],
  "packages":[
    {"source":"lkshrk/linear-ai","ref":"v2.0.0"},
    {"source":"anthropics/claude-plugins-official","path":"plugins/code-simplifier","targets":["claude"]}
  ],
  "mcp":[{"name":"litellm-tools","transport":"http","url":"https://mcp.example.test/","targets":["claude"],
    "client":{"claude":{"headersHelper":"~/bin/headers"}}}],
  "lsp":[{"name":"gopls","command":"gopls","extensionToLanguage":{".go":"go"}}],
  "marketplaces":[{"name":"caveman","source":"JuliusBrussee/caveman"}],
  "ignored":[{"host":"work","target":"claude","kind":"plugin","id":"acme/tool"}]
}`

func TestMigrateV26ToV27PreservesAgentsIgnored(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json", `{"version":26,`+validAgentsIgnoredBlock+`}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load v26 config: %v", err)
	}
	if cfg.Version != 27 {
		t.Fatalf("version = %d, want 27", cfg.Version)
	}
	want := config.AgentIgnoreEntry{Host: "work", Target: "claude", Kind: "plugin", ID: "acme/tool", Reason: "local pin"}
	if cfg.Agents == nil || len(cfg.Agents.Ignored) != 1 || cfg.Agents.Ignored[0] != want {
		t.Fatalf("cfg.Agents = %#v, want the ignored entry preserved", cfg.Agents)
	}
	if cfg.Agents.Declares() {
		t.Fatal("an ignored-only block must not declare agent state")
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 27`) || !strings.Contains(string(data), ".v27.schema.json") {
		t.Fatalf("saved config = %s, want v27 with the v27 schema", data)
	}
}

func TestV27LoadsAgentDeclarationsAndGroupAgents(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json", fmt.Sprintf(`{"version":%d,%s,
  "groups":[{"name":"ai","agents":["linear-ai","code-simplifier","litellm-tools","gopls"]}]}`, config.CurrentVersion, v27AgentsBlock))
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if errs := config.ValidateRoot(cfg, config.ProviderValidation{}); len(errs) > 0 {
		t.Fatalf("ValidateRoot = %v, want none", errs)
	}
	agents := cfg.Agents
	if !agents.Declares() || len(agents.Packages) != 2 || len(agents.MCP) != 1 || len(agents.LSP) != 1 || len(agents.Marketplaces) != 1 {
		t.Fatalf("agents = %#v, want every declaration decoded", agents)
	}
	if got := agents.EntryNames(); strings.Join(got, ",") != "linear-ai,code-simplifier,litellm-tools,gopls" {
		t.Fatalf("EntryNames = %v", got)
	}
	if got := cfg.Groups[0].Agents; len(got) != 4 {
		t.Fatalf("group agents = %v, want 4 names", got)
	}
	found, err := config.HasRemovedAgentConfig(path)
	if err != nil || found {
		t.Fatalf("HasRemovedAgentConfig = %v, %v; want false, nil", found, err)
	}
}

func TestPreV27AgentsBlockAcceptsOnlyIgnored(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		version     int
		block, path string
	}{
		"v23 package without legacy keys": {23, `{"packages":[{"source":"a/b","ref":"v1"}]}`, `$.agents.packages`},
		"v23 package with skills":         {23, `{"packages":[{"source":"a/b","skills":["x"]}]}`, `$.agents.packages`},
		"v23 marketplace":                 {23, `{"marketplaces":[{"name":"m","source":"a/b"}]}`, `$.agents.marketplaces`},
		"v26 declarations":                {26, `{"mcp":[],"ignored":[]}`, `$.agents.mcp`},
		"legacy mcp_servers at v27":       {config.CurrentVersion, `{"mcp_servers":[]}`, `$.agents.mcp_servers`},
		"null field at v27":               {config.CurrentVersion, `{"targets":null}`, `$.agents.targets`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := writeSettings(t, t.TempDir(), "settings.json",
				fmt.Sprintf(`{"version":%d,"agents":%s}`, tc.version, tc.block))
			if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), `"`+tc.path+`"`) {
				t.Fatalf("Load error = %v, want removed field %s", err, tc.path)
			}
			found, err := config.HasRemovedAgentConfig(path)
			if err != nil || !found {
				t.Fatalf("HasRemovedAgentConfig = %v, %v; want true, nil", found, err)
			}
		})
	}
}

func TestDuplicateAgentEntriesInOneFragmentFailValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "settings.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	main := writeSettings(t, dir, "settings.json", fmt.Sprintf(`{"version":%d,"$include":["settings.d/agents.json"]}`, config.CurrentVersion))
	writeSettings(t, dir, "settings.d/agents.json", `{"agents":{"packages":[{"source":"a/tool"},{"source":"b/tool"}],
  "marketplaces":[{"name":"m","source":"x/a"},{"name":"m","source":"x/b"}],"ignored":[]}}`)
	cfg, err := config.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Agents.Packages) != 2 || len(cfg.Agents.Marketplaces) != 2 {
		t.Fatalf("agents = %#v, want both duplicates kept for validation", cfg.Agents)
	}
	paths := map[string]bool{}
	for _, err := range config.ValidateRoot(cfg, config.ProviderValidation{}) {
		paths[err.Path] = true
	}
	if !paths["$.agents.packages[1]"] || !paths["$.agents.marketplaces[1].name"] {
		t.Fatalf("validation paths = %v, want both duplicates reported", paths)
	}
}

func TestV27RejectsUnknownAgentEntryField(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json",
		fmt.Sprintf(`{"version":%d,"agents":{"lsp":[{"name":"gopls","command":"gopls","extensionToLanguage":{".go":"go"},"colour":"red"}]}}`, config.CurrentVersion))
	if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), "agents.lsp") || !strings.Contains(err.Error(), "colour") {
		t.Fatalf("Load error = %v, want the unknown lsp field reported", err)
	}
}

func TestValidateRootAgents(t *testing.T) {
	t.Parallel()
	targets := []string{"claude"}
	cases := map[string]struct {
		agents config.AgentsConfig
		groups []*config.GroupConfig
		path   string
	}{
		"duplicate derived name": {
			agents: config.AgentsConfig{Packages: []config.AgentPackage{{Source: "a/tool"}, {Source: "b/tool"}}},
			path:   "$.agents.packages[1]",
		},
		"name shared across kinds": {
			agents: config.AgentsConfig{
				Packages: []config.AgentPackage{{Source: "a/gopls"}},
				LSP:      []config.AgentLSPServer{{Name: "gopls", Command: "gopls", ExtensionToLanguage: map[string]string{".go": "go"}}},
			},
			path: "$.agents.lsp[0].name",
		},
		"package without source or path": {
			agents: config.AgentsConfig{Packages: []config.AgentPackage{{Name: "x"}}},
			path:   "$.agents.packages[0]",
		},
		"stdio without command": {
			agents: config.AgentsConfig{MCP: []config.AgentMCPServer{{Name: "m", Transport: "stdio"}}},
			path:   "$.agents.mcp[0].command",
		},
		"client outside targets": {
			agents: config.AgentsConfig{MCP: []config.AgentMCPServer{{Name: "m", Transport: "http", URL: "https://x.test", Targets: &targets,
				Client: map[string]map[string]any{"codex": {"required": true}}}}},
			path: `$.agents.mcp[0].client."codex"`,
		},
		"conflicting client fields": {
			agents: config.AgentsConfig{MCP: []config.AgentMCPServer{{Name: "m", Transport: "http", URL: "https://x.test",
				Client: map[string]map[string]any{"claude": {"k": "a"}, "codex": {"k": "b"}}}}},
			path: "$.agents.mcp[0].client",
		},
		"marketplace source starting with a dash": {
			agents: config.AgentsConfig{Marketplaces: []config.AgentMarketplace{{Name: "m", Source: "--upload-pack=x"}}},
			path:   "$.agents.marketplaces[0].source",
		},
		"lsp without extension map": {
			agents: config.AgentsConfig{LSP: []config.AgentLSPServer{{Name: "gopls", Command: "gopls"}}},
			path:   "$.agents.lsp[0].extensionToLanguage",
		},
		"group references unknown entry": {
			agents: config.AgentsConfig{Packages: []config.AgentPackage{{Source: "a/tool"}}},
			groups: []*config.GroupConfig{{Name: "ai", Agents: []string{"missing"}}},
			path:   "$.groups[0].agents[0]",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			agents := tc.agents
			cfg := &config.RootConfig{Version: config.CurrentVersion, Agents: &agents, Groups: tc.groups}
			errs := config.ValidateRoot(cfg, config.ProviderValidation{})
			for _, err := range errs {
				if err.Path == tc.path && !err.Warn {
					return
				}
			}
			t.Fatalf("ValidateRoot = %v, want an error at %s", errs, tc.path)
		})
	}
}

func TestAgentMCPServerRenderedExtraMergesClients(t *testing.T) {
	t.Parallel()
	server := config.AgentMCPServer{
		Extra:  map[string]any{"required": true},
		Client: map[string]map[string]any{"claude": {"headersHelper": "~/h"}, "codex": {"http_headers_helper": "~/h", "required": true}},
	}
	extra, err := server.RenderedExtra()
	if err != nil {
		t.Fatal(err)
	}
	if len(extra) != 3 || extra["headersHelper"] != "~/h" || extra["http_headers_helper"] != "~/h" || extra["required"] != true {
		t.Fatalf("RenderedExtra = %#v", extra)
	}
}

func TestAgentPackageEntryName(t *testing.T) {
	t.Parallel()
	cases := map[string]config.AgentPackage{
		"linear-ai":       {Source: "lkshrk/linear-ai"},
		"code-simplifier": {Source: "anthropics/claude-plugins-official", Path: "plugins/code-simplifier/"},
		"omp-skills":      {Path: "~/Dev/dotfiles/apm/omp-skills"},
		"repo":            {Source: "git@github.com:owner/repo.git"},
		"custom":          {Name: "custom", Source: "owner/repo"},
	}
	for want, pkg := range cases {
		if got := pkg.EntryName(); got != want {
			t.Errorf("EntryName(%#v) = %q, want %q", pkg, got, want)
		}
	}
}

func TestAgentsMergeAcrossIncludesAndRoutedGroupWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "settings.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	main := writeSettings(t, dir, "settings.json", fmt.Sprintf(`{"version":%d,
  "$include":["settings.d/groups.json","settings.d/agents.json"],
  "agents":{"packages":[{"source":"a/tool","ref":"v1"}],"ignored":[]}}`, config.CurrentVersion))
	writeSettings(t, dir, "settings.d/groups.json", `{"groups":[{"name":"ai","agents":["tool"]}]}`)
	writeSettings(t, dir, "settings.d/agents.json", `{"agents":{"packages":[{"source":"a/tool","ref":"v2"},{"source":"b/other"}],"ignored":[]}}`)
	cfg, err := config.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Agents.Packages; len(got) != 2 || got[0].Ref != "v2" || got[1].EntryName() != "other" {
		t.Fatalf("merged packages = %#v, want the fragment's v2 to win and other appended", got)
	}
	err = config.WriteConfig(main, func() (*config.RootConfig, error) { return config.Load(main) }, nil, func(cfg *config.RootConfig) error {
		cfg.Groups[0].Agents = append(cfg.Groups[0].Agents, "other")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	groups, err := os.ReadFile(filepath.Join(dir, "settings.d", "groups.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(groups), `"other"`) {
		t.Fatalf("groups.json = %s, want the new group agent routed to the fragment that owns the group", groups)
	}
	reloaded, err := config.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Groups[0].Agents; len(got) != 2 {
		t.Fatalf("reloaded group agents = %v, want tool and other", got)
	}
}

func writeFragmentChain(t *testing.T, main, fragment string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "settings.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, dir, "settings.d/agents.json", fragment)
	return writeSettings(t, dir, "settings.json", main), dir
}

func TestLegacyAgentsBlockInVersionlessFragmentStaysRetired(t *testing.T) {
	t.Parallel()
	for name, fragment := range map[string]string{
		"package":     `{"agents":{"packages":[{"source":"a/b","agents":["claude-code"]}]}}`,
		"marketplace": `{"agents":{"marketplaces":[{"name":"acme","owner":"acme","repo":"market"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			main, _ := writeFragmentChain(t, `{"version":23,"$include":["settings.d/agents.json"]}`, fragment)
			if _, err := config.Load(main); err == nil || !strings.Contains(err.Error(), "removed in v24") {
				t.Fatalf("Load error = %v, want the v24 tombstone", err)
			}
			if found, err := config.HasRemovedAgentConfig(main); err != nil || !found {
				t.Fatalf("HasRemovedAgentConfig = %v, %v; want true, nil", found, err)
			}
		})
	}
}

func TestAgentsKeyCaseVariantIsRejected(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json",
		fmt.Sprintf(`{"version":%d,"Agents":{"packages":[{"source":"o/r","bogus":1}],"ignored":[]}}`, config.CurrentVersion))
	if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), `"Agents"`) || !strings.Contains(err.Error(), "lowercase") {
		t.Fatalf("Load error = %v, want the case variant rejected", err)
	}
}

func TestMergeKeepsRepeatedDuplicatesOfAnEarlierKey(t *testing.T) {
	t.Parallel()
	main, _ := writeFragmentChain(t,
		fmt.Sprintf(`{"version":%d,"$include":["settings.d/agents.json"],"agents":{"packages":[{"source":"a/x"}],"ignored":[]}}`, config.CurrentVersion),
		`{"agents":{"packages":[{"source":"b/x"},{"source":"c/x"}],"ignored":[]}}`)
	cfg, err := config.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Agents.Packages) != 2 {
		t.Fatalf("packages = %#v, want the second fragment duplicate kept for validation", cfg.Agents.Packages)
	}
}

func TestAgentsNumbersStayIntegers(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json", fmt.Sprintf(`{"version":%d,"agents":{
  "mcp":[{"name":"m","transport":"stdio","command":"x","extra":{"startup_timeout_ms":1000000}}],
  "lsp":[{"name":"l","command":"l","extensionToLanguage":{".l":"l"},"initializationOptions":{"limit":2000000,"ratio":0.5}}],
  "ignored":[]}}`, config.CurrentVersion))
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Agents.MCP[0].Extra["startup_timeout_ms"]; got != int64(1000000) {
		t.Fatalf("extra value = %#v, want int64", got)
	}
	options := cfg.Agents.LSP[0].InitializationOptions.(map[string]any)
	if options["limit"] != int64(2000000) || options["ratio"] != 0.5 {
		t.Fatalf("initializationOptions = %#v", options)
	}
}

func TestEnsureAgentsFragmentKeepsDotSlashIncludeAndExistingBlock(t *testing.T) {
	t.Parallel()
	main, _ := writeFragmentChain(t,
		fmt.Sprintf(`{"version":%d,"$include":["./settings.d/agents.json"]}`, config.CurrentVersion),
		`{"note":"kept"}`)
	if err := config.EnsureAgentsFragment(main); err != nil {
		t.Fatal(err)
	}
	if got := rawKeys(t, main)["$include"]; strings.Count(string(got), "agents.json") != 1 {
		t.Fatalf("$include = %s, want the dot-slash include recognised", got)
	}
	unincluded := filepath.Join(t.TempDir(), "settings.json")
	writeSettings(t, filepath.Dir(unincluded), "settings.json", fmt.Sprintf(`{"version":%d}`, config.CurrentVersion))
	if err := os.MkdirAll(filepath.Join(filepath.Dir(unincluded), "settings.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Dir(unincluded), "settings.d/agents.json", `{"agents":{"packages":[{"source":"a/kept"}],"ignored":[]}}`)
	if err := config.EnsureAgentsFragment(unincluded); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(unincluded)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents == nil || len(cfg.Agents.Packages) != 1 {
		t.Fatalf("agents = %#v, want the existing fragment block kept", cfg.Agents)
	}
}

func TestDocumentedMCPExampleValidatesCleanly(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json", fmt.Sprintf(`{"version":%d,"agents":{
  "mcp":[{"name":"litellm-tools","transport":"http","url":"https://mcp.example.com/","targets":["claude","codex"],
    "client":{"claude":{"headersHelper":"~/h"},"codex":{"http_headers_helper":"~/h"}}}],
  "ignored":[]},"groups":[{"name":"ai","agents":["litellm-tools"]}]}`, config.CurrentVersion))
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if errs := config.ValidateRoot(cfg, config.ProviderValidation{}); len(errs) > 0 {
		t.Fatalf("ValidateRoot = %v, want the documented example clean so doctor passes", errs)
	}
}

func TestAgentsExponentNumbersNormalise(t *testing.T) {
	t.Parallel()
	path := writeSettings(t, t.TempDir(), "settings.json", fmt.Sprintf(`{"version":%d,"agents":{
  "mcp":[{"name":"m","transport":"stdio","command":"x","extra":{"a":1e6,"b":1E-7,"c":1000000.0}}],"ignored":[]}}`, config.CurrentVersion))
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	extra := cfg.Agents.MCP[0].Extra
	if extra["a"] != int64(1000000) || extra["b"] != 1e-7 || extra["c"] != int64(1000000) {
		t.Fatalf("extra = %#v, want exponent forms normalised to numbers", extra)
	}
}
