package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lkshrk/omni/internal/config"
	"github.com/lkshrk/omni/internal/executor"
)

func agentsRenderSettings(host string) string {
	return fmt.Sprintf(`{"version":%d,
  "agents":{
    "targets":["claude","codex"],
    "packages":[
      {"source":"lkshrk/linear-ai","ref":"v2.0.0"},
      {"source":"anthropics/claude-plugins-official","path":"plugins/code-simplifier","targets":["claude"]},
      {"source":"acme/work-only"}
    ],
    "mcp":[{"name":"litellm-tools","transport":"http","url":"https://mcp.example.test/","targets":["claude"],
      "client":{"claude":{"headersHelper":"~/bin/headers"}}}],
    "lsp":[{"name":"gopls","command":"gopls","extensionToLanguage":{".go":"go"}}],
    "ignored":[]
  },
  "groups":[
    {"name":%q,"special":"host","agents":["code-simplifier"]},
    {"name":"ai","agents":["linear-ai","litellm-tools","gopls"]},
    {"name":"work","agents":["work-only"]}
  ],
  "hosts":{%q:["ai"]}
}`, config.CurrentVersion, host, host)
}

const agentsRenderGolden = agentsRenderedMarker + `
name: omni
version: 1.0.0
targets:
  - claude
  - codex
dependencies:
  apm:
    - git: lkshrk/linear-ai
      ref: v2.0.0
    - git: anthropics/claude-plugins-official
      path: plugins/code-simplifier
      targets:
        - claude
  mcp:
    - name: litellm-tools
      targets: [claude]
      registry: false
      transport: http
      url: https://mcp.example.test/
      extra:
        headersHelper: ~/bin/headers
  lsp:
    - name: gopls
      command: gopls
      extensionToLanguage:
        .go: go
`

func loadRenderConfig(t *testing.T, body string) (*config.RootConfig, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	writeFile(t, path, body)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, path
}

func TestRenderAgentsManifestGolden(t *testing.T) {
	host := currentMachineGroupName()
	cfg, _ := loadRenderConfig(t, agentsRenderSettings(host))
	got, err := renderAgentsManifest(cfg, currentHostname())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != agentsRenderGolden {
		t.Fatalf("rendered manifest mismatch:\n%s", agentsManifestDiff([]byte(agentsRenderGolden), got))
	}
}

func TestRenderAgentsManifestOmitsUnselectedAndEmptyTargets(t *testing.T) {
	host := currentMachineGroupName()
	cfg, _ := loadRenderConfig(t, fmt.Sprintf(`{"version":%d,
  "agents":{"mcp":[{"name":"nowhere","transport":"stdio","command":"x","targets":[]},{"name":"elsewhere","transport":"stdio","command":"y"}],"ignored":[]},
  "groups":[{"name":%q,"special":"host","agents":["nowhere"]},{"name":"other","agents":["elsewhere"]}],
  "hosts":{%q:[]}}`, config.CurrentVersion, host, host))
	got, err := renderAgentsManifest(cfg, currentHostname())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "targets: []") {
		t.Fatalf("rendered = %s, want an explicit empty targets list kept", got)
	}
	if strings.Contains(string(got), "elsewhere") || strings.Contains(string(got), "\ntargets:") {
		t.Fatalf("rendered = %s, want the unselected server and the absent manifest targets left out", got)
	}
}

type renderedSyncEnv struct {
	app      *App
	live     string
	settings string
	mock     *executor.MockExecutor
}

func setupRenderedSync(t *testing.T, responses ...executor.MockCall) renderedSyncEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings := filepath.Join(t.TempDir(), "settings.json")
	writeFile(t, settings, agentsRenderSettings(currentMachineGroupName()))
	mock := &executor.MockExecutor{Responses: responses}
	a := New(settings)
	a.StateDir = t.TempDir()
	a.SetFallbackExecutor(mock)
	return renderedSyncEnv{app: a, live: filepath.Join(home, ".apm", "apm.yml"), settings: settings, mock: mock}
}

func apmInstalled() []executor.MockCall {
	return []executor.MockCall{
		{Stdout: "APM CLI version " + apmVersionPin + "\n"},
		{Stdout: "installed\n"},
	}
}

func installRan(mock *executor.MockExecutor) bool {
	return slices.ContainsFunc(mock.Calls, func(call executor.MockCall) bool {
		return len(call.Args) >= 2 && call.Args[0] == "install" && call.Args[1] == "-g"
	})
}

func TestAgentsSyncAllRendersSettingsThenInstalls(t *testing.T) {
	env := setupRenderedSync(t, apmInstalled()...)
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(env.live)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != agentsRenderGolden {
		t.Fatalf("live manifest mismatch:\n%s", agentsManifestDiff([]byte(agentsRenderGolden), got))
	}
	if !installRan(env.mock) {
		t.Fatalf("calls = %#v, want apm install -g after rendering", env.mock.Calls)
	}
	if _, err := os.Stat(filepath.Join(env.app.StateDir, renderStateName)); err != nil {
		t.Fatalf("snapshot after install missing: %v", err)
	}
}

func TestAgentsSyncAllRefusesHandEditWithoutAdoptOrForce(t *testing.T) {
	env := setupRenderedSync(t, apmInstalled()...)
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(agentsRenderGolden, "    - git: lkshrk/linear-ai", "    - git: acme/hand-added\n    - git: lkshrk/linear-ai", 1)
	writeFile(t, env.live, edited)
	calls := len(env.mock.Calls)

	_, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{})
	var drift *AgentsManifestDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("err = %v, want AgentsManifestDriftError", err)
	}
	if !strings.Contains(drift.Diff, "-package acme/hand-added") {
		t.Fatalf("diff = %s, want the hand-added line reported", drift.Diff)
	}
	if strings.Contains(err.Error(), "acme/hand-added") {
		t.Fatalf("error %q must not echo manifest content", err)
	}
	if got, _ := os.ReadFile(env.live); string(got) != edited {
		t.Fatal("a refused sync must leave the hand edit in place")
	}
	if len(env.mock.Calls) != calls {
		t.Fatalf("calls = %#v, want apm untouched on drift", env.mock.Calls[calls:])
	}
}

func TestAgentsSyncAllForceOverwritesHandEdit(t *testing.T) {
	responses := append(apmInstalled(), executor.MockCall{Stdout: "installed\n"})
	env := setupRenderedSync(t, responses...)
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, env.live, agentsRenderGolden+"# hand edit\n")
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(env.live); string(got) != agentsRenderGolden {
		t.Fatalf("live = %s, want the rendered manifest back", got)
	}
}

func TestAgentsSyncAllAdoptDeclaresHandAddedEntries(t *testing.T) {
	responses := append(apmInstalled(), executor.MockCall{Stdout: "installed\n"})
	env := setupRenderedSync(t, responses...)
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(agentsRenderGolden, "  mcp:\n", "    - git: acme/hand-added\n      ref: v1.0.0\n    - git: acme/work-only\n  mcp:\n", 1)
	edited = strings.Replace(edited, "  lsp:\n", "    - name: local-docs\n      registry: false\n      transport: stdio\n      command: docs-mcp\n  lsp:\n", 1)
	writeFile(t, env.live, edited)

	res, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(res.Notices, func(n string) bool { return strings.Contains(n, "hand-added, work-only, local-docs") }) {
		t.Fatalf("notices = %q, want the adopted entries named", res.Notices)
	}
	fragment, err := os.ReadFile(filepath.Join(filepath.Dir(env.settings), "settings.d", "agents.json"))
	if err != nil {
		t.Fatalf("agents.json not written: %v", err)
	}
	if !strings.Contains(string(fragment), `"acme/hand-added"`) || !strings.Contains(string(fragment), `"local-docs"`) {
		t.Fatalf("agents.json = %s, want the new package and MCP server declared", fragment)
	}
	if strings.Count(string(fragment), `"acme/work-only"`) != 1 {
		t.Fatalf("agents.json = %s, want the already declared package not duplicated", fragment)
	}
	cfg, err := config.Load(env.settings)
	if err != nil {
		t.Fatal(err)
	}
	var hostGroup *config.GroupConfig
	for _, group := range cfg.Groups {
		if group.BaseName() == currentMachineGroupName() {
			hostGroup = group
		}
	}
	if hostGroup == nil || !slices.Equal(hostGroup.Agents, []string{"code-simplifier", "hand-added", "work-only", "local-docs"}) {
		t.Fatalf("host group = %#v, want the adopted names appended", hostGroup)
	}
	live, _ := os.ReadFile(env.live)
	for _, want := range []string{"git: acme/hand-added", "ref: v1.0.0", "git: acme/work-only", "name: local-docs"} {
		if !strings.Contains(string(live), want) {
			t.Fatalf("live = %s, want %q rendered after adopting", live, want)
		}
	}
}

func TestAgentsSyncAllAdoptRefusesUnrepresentableEntry(t *testing.T) {
	env := setupRenderedSync(t, apmInstalled()...)
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(agentsRenderGolden, "  lsp:\n", "    - name: registry-one\n      registry: true\n      transport: stdio\n  lsp:\n", 1)
	writeFile(t, env.live, edited)
	_, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{Adopt: true})
	if err == nil || !strings.Contains(err.Error(), "registry MCP server") {
		t.Fatalf("err = %v, want the registry server refused", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(env.settings), "settings.d", "agents.json")); !os.IsNotExist(statErr) {
		t.Fatalf("agents.json stat = %v, want no settings written on a refused adopt", statErr)
	}
}

func TestAgentsSyncAllDryRunReportsDriftWithoutWriting(t *testing.T) {
	env := setupRenderedSync(t, executor.MockCall{Stdout: "APM CLI version " + apmVersionPin + "\n"}, executor.MockCall{Stdout: "planned\n"})
	writeFile(t, env.live, "name: hand\ndependencies: {}\n")
	res, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Warning, "--adopt or --force") {
		t.Fatalf("warning = %q, want the drift reported", res.Warning)
	}
	if got, _ := os.ReadFile(env.live); string(got) != "name: hand\ndependencies: {}\n" {
		t.Fatalf("live = %q, want a dry run to leave it untouched", got)
	}
}

func TestAgentsSyncAllWithoutDeclarationsKeepsTemplatePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	writeFile(t, filepath.Join(cfgDir, "omni", "apm.yml"), "name: from-template\n")
	settings := filepath.Join(t.TempDir(), "settings.json")
	writeFile(t, settings, fmt.Sprintf(`{"version":%d,"agents":{"ignored":[]}}`, config.CurrentVersion))
	a := New(settings)
	a.StateDir = t.TempDir()
	a.SetFallbackExecutor(&executor.MockExecutor{Responses: apmInstalled()})
	if _, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".apm", "apm.yml")); string(got) != "name: from-template\n" {
		t.Fatalf("live = %q, want the host template copied as before", got)
	}
}

func syncOnce(t *testing.T, env renderedSyncEnv) {
	t.Helper()
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatal(err)
	}
}

func adoptEdited(t *testing.T, edit func(string) string) (renderedSyncEnv, AgentsSyncAllResult, error) {
	t.Helper()
	env := setupRenderedSync(t, append(apmInstalled(), executor.MockCall{Stdout: "installed\n"})...)
	syncOnce(t, env)
	writeFile(t, env.live, edit(agentsRenderGolden))
	res, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{Adopt: true})
	return env, res, err
}

func addPackageLines(lines string) func(string) string {
	return func(manifest string) string { return strings.Replace(manifest, "  mcp:\n", lines+"  mcp:\n", 1) }
}

func addMCPLines(lines string) func(string) string {
	return func(manifest string) string { return strings.Replace(manifest, "  lsp:\n", lines+"  lsp:\n", 1) }
}

func TestAgentsSyncAllFailedInstallIsNotReportedAsHandEdit(t *testing.T) {
	env := setupRenderedSync(t,
		executor.MockCall{Stdout: "APM CLI version " + apmVersionPin + "\n"},
		executor.MockCall{Stderr: "network down\n", Err: errors.New("exit status 1")},
		executor.MockCall{Stdout: "installed\n"},
	)
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err == nil {
		t.Fatal("want the failed install reported")
	}
	settings, _ := os.ReadFile(env.settings)
	writeFile(t, env.settings, strings.Replace(string(settings), `"agents":["linear-ai",`, `"agents":["work-only","linear-ai",`, 1))
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatalf("second sync = %v, want omni's own earlier render accepted", err)
	}
	if got, _ := os.ReadFile(env.live); !strings.Contains(string(got), "git: acme/work-only") {
		t.Fatalf("live = %s, want the settings change rendered", got)
	}
}

func TestAgentsSyncAllWritesRenderedManifestPrivately(t *testing.T) {
	env := setupRenderedSync(t, apmInstalled()...)
	syncOnce(t, env)
	info, err := os.Stat(env.live)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600 for a manifest that may carry header values", info.Mode().Perm())
	}
}

func TestAgentsManifestDiffPrintsNoValues(t *testing.T) {
	live := []byte(`# old token: ghp_COMMENT
dependencies:
  apm:
    - https://x-access-token:ghp_URL@github.com/o/r.git
  mcp:
    - name: x
      url: https://mcp.example.com/?api_key=QUERY
      args: [--api-key, ARG]
      headers:
        Authorization: Bearer s3cret # was Bearer COMMENT2
      env:
        TOKEN: abc123
`)
	diff := agentsManifestDiff(live, []byte("dependencies: {}\n"))
	for _, secret := range []string{"ghp_COMMENT", "ghp_URL", "QUERY", "ARG", "s3cret", "COMMENT2", "abc123"} {
		if strings.Contains(diff, secret) {
			t.Fatalf("diff leaks %q:\n%s", secret, diff)
		}
	}
	if !strings.Contains(diff, "-mcp x targets=- fields=args,env,headers,url #") || !strings.Contains(diff, "-package https://…") {
		t.Fatalf("diff = %s, want entry identities and field names kept", diff)
	}
	if got := agentsManifestDiff([]byte("a: 1\n---\nb: 2\n"), []byte("a: 1\n")); !strings.Contains(got, "more than one YAML document") {
		t.Fatalf("multi-document diff = %q", got)
	}
	changed := agentsManifestDiff([]byte("dependencies:\n  mcp:\n    - name: x\n      env: {A: one}\n"), []byte("dependencies:\n  mcp:\n    - name: x\n      env: {A: two}\n"))
	if !strings.Contains(changed, "-mcp x") || strings.Contains(changed, "one") || strings.Contains(changed, "two") {
		t.Fatalf("value-only diff = %q, want the entry flagged without its values", changed)
	}
	if got := agentsManifestDiff([]byte("custom: {token: SECRET}\n"), []byte("custom: {token: OTHER}\n")); strings.Contains(got, "SECRET") || !strings.Contains(got, "fields=<field>") {
		t.Fatalf("top-level diff = %q, want unknown keys hashed", got)
	}
	for _, source := range []string{"https://u:SEC/RET3@h/x", "https://u:S%zz@h", "oauth2:SECRET6@gitlab.com/o/r", "SECRET7@github.com/o/r", "https://h/sse/x?k=SECRET8#frag"} {
		if got := summarySource(source); strings.Contains(got, "SEC") {
			t.Errorf("summarySource(%q) = %q leaks", source, got)
		}
	}
}

func TestAgentsSyncAllAdoptRefusesTakenName(t *testing.T) {
	env, _, err := adoptEdited(t, addPackageLines("    - git: other/linear-ai\n"))
	if err == nil || !strings.Contains(err.Error(), `"linear-ai"`) {
		t.Fatalf("err = %v, want the taken name refused", err)
	}
	if got, _ := os.ReadFile(env.live); !strings.Contains(string(got), "other/linear-ai") {
		t.Fatal("a refused adopt must leave the hand edit in place")
	}
}

func TestAgentsSyncAllAdoptKeepsPackageFieldsAndScalarRefs(t *testing.T) {
	env, res, err := adoptEdited(t, addPackageLines("    - acme/scalar@v3\n    - git: acme/long-repo-name\n      name: short\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(env.settings)
	if err != nil {
		t.Fatal(err)
	}
	var scalar, named *config.AgentPackage
	for i := range cfg.Agents.Packages {
		switch cfg.Agents.Packages[i].Source {
		case "acme/scalar":
			scalar = &cfg.Agents.Packages[i]
		case "acme/long-repo-name":
			named = &cfg.Agents.Packages[i]
		}
	}
	if scalar == nil || scalar.Ref != "v3" || named == nil || named.EntryName() != "short" {
		t.Fatalf("packages = %#v, want the scalar ref split and the name kept", cfg.Agents.Packages)
	}
	if slices.ContainsFunc(res.Notices, func(n string) bool { return strings.Contains(n, "other hand edits") }) {
		t.Fatalf("notices = %q, want no replaced-edits notice when every entry was adopted", res.Notices)
	}
}

func TestAgentsSyncAllAdoptRefusesUnknownPackageField(t *testing.T) {
	_, _, err := adoptEdited(t, addPackageLines("    - git: acme/x\n      alias: y\n"))
	if err == nil || !strings.Contains(err.Error(), `"alias"`) {
		t.Fatalf("err = %v, want the unknown package field refused", err)
	}
}

func TestAgentsSyncAllAdoptTakesNumericScalarsAsStrings(t *testing.T) {
	env, res, err := adoptEdited(t, addMCPLines("    - name: local\n      registry: false\n      transport: stdio\n      command: srv\n      args: [--port, 8080]\n      env:\n        PORT: 8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	fragment, _ := os.ReadFile(filepath.Join(filepath.Dir(env.settings), "settings.d", "agents.json"))
	if !strings.Contains(string(fragment), `"8080"`) {
		t.Fatalf("agents.json = %s, want 8080 kept as a string", fragment)
	}
	if !slices.ContainsFunc(res.Notices, func(n string) bool { return strings.Contains(n, "literal values that may be secrets") }) {
		t.Fatalf("notices = %q, want the literal env value flagged", res.Notices)
	}
}

func TestAgentsSyncAllAdoptRefusesInvalidEntryBeforeWriting(t *testing.T) {
	env, _, err := adoptEdited(t, addMCPLines("    - name: broken\n      registry: false\n      transport: stdio\n"))
	if err == nil || !strings.Contains(err.Error(), "would not validate") {
		t.Fatalf("err = %v, want validation to refuse the adoption", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(env.settings), "settings.d", "agents.json")); !os.IsNotExist(statErr) {
		t.Fatalf("agents.json stat = %v, want nothing written", statErr)
	}
}

func TestAgentsSyncAllRejectsFlagsOfTheOtherPath(t *testing.T) {
	env := setupRenderedSync(t, apmInstalled()...)
	if _, err := env.app.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{ForceTemplate: true}); err == nil || !strings.Contains(err.Error(), "--force-template") {
		t.Fatalf("err = %v, want --force-template refused while settings render", err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	writeFile(t, filepath.Join(cfgDir, "omni", "apm.yml"), "name: from-template\n")
	a := New(filepath.Join(t.TempDir(), "settings.json"))
	a.StateDir = t.TempDir()
	a.SetFallbackExecutor(&executor.MockExecutor{Responses: apmInstalled()})
	if _, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{Force: true}); err == nil || !strings.Contains(err.Error(), "--adopt and --force") {
		t.Fatalf("err = %v, want --force refused on the template path", err)
	}
}

func TestAgentsSyncAllRegistersSettingsMarketplaces(t *testing.T) {
	env := setupRenderedSync(t, apmInstalled()[0], executor.MockCall{Stdout: "added\n"}, executor.MockCall{Stdout: "installed\n"})
	settings, _ := os.ReadFile(env.settings)
	writeFile(t, env.settings, strings.Replace(string(settings), `"ignored":[]`, `"marketplaces":[{"name":"caveman","source":"JuliusBrussee/caveman"}],"ignored":[]`, 1))
	syncOnce(t, env)
	if !slices.ContainsFunc(env.mock.Calls, func(call executor.MockCall) bool {
		return slices.Equal(call.Args, []string{"marketplace", "add", "JuliusBrussee/caveman", "--name", "caveman"})
	}) {
		t.Fatalf("calls = %#v, want the declared marketplace registered", env.mock.Calls)
	}
}

func TestAgentsSyncAllAdoptScalarLocalPathAndNoFalseOverwriteNotice(t *testing.T) {
	env, res, err := adoptEdited(t, addPackageLines("    - ~/Dev/local-pkg\n    - acme/scalar@v3\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(env.settings)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(cfg.Agents.Packages, func(p config.AgentPackage) bool { return p.Path == "~/Dev/local-pkg" && p.Source == "" }) {
		t.Fatalf("packages = %#v, want the scalar local path adopted as a path", cfg.Agents.Packages)
	}
	if slices.ContainsFunc(res.Notices, func(n string) bool { return strings.Contains(n, "other hand edits") }) {
		t.Fatalf("notices = %q, want no overwrite notice when every entry was adopted", res.Notices)
	}
}

func TestAgentsSyncAllAdoptRefusesCredentialsAndImplicitRegistry(t *testing.T) {
	_, _, err := adoptEdited(t, addPackageLines("    - git: https://user:ghp_SECRET@github.com/o/r.git\n"))
	if err == nil || !strings.Contains(err.Error(), "credentials") || strings.Contains(err.Error(), "ghp_SECRET") {
		t.Fatalf("err = %v, want the credential URL refused without echoing it", err)
	}
	_, _, err = adoptEdited(t, addMCPLines("    - name: io.github.example/server\n      transport: stdio\n"))
	if err == nil || !strings.Contains(err.Error(), "registry MCP server") {
		t.Fatalf("err = %v, want a server without registry: false treated as a registry server", err)
	}
}

func TestWriteRenderedManifestRefusesDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing", "apm.yml"), filepath.Join(dir, "apm.yml")); err != nil {
		t.Fatal(err)
	}
	if err := writeRenderedManifest(dir, []byte("x: 1\n")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v, want the dangling link refused", err)
	}
	if info, err := os.Lstat(filepath.Join(dir, "apm.yml")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("lstat = %v, %v; want the link left in place", info, err)
	}
}

func TestRenderAgentsManifestKeepsLargeIntegers(t *testing.T) {
	host := currentMachineGroupName()
	cfg, _ := loadRenderConfig(t, fmt.Sprintf(`{"version":%d,
  "agents":{"mcp":[{"name":"m","transport":"stdio","command":"x","extra":{"startup_timeout_ms":1000000}}],"ignored":[]},
  "groups":[{"name":%q,"special":"host","agents":["m"]}],"hosts":{%q:[]}}`, config.CurrentVersion, host, host))
	got, err := renderAgentsManifest(cfg, currentHostname())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "startup_timeout_ms: 1000000") {
		t.Fatalf("rendered = %s, want the integer kept", got)
	}
}

func TestAgentLSPServerMatchesManifestFields(t *testing.T) {
	var settings, manifest []string
	for _, field := range reflect.VisibleFields(reflect.TypeFor[config.AgentLSPServer]()) {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		settings = append(settings, name)
	}
	for _, field := range reflect.VisibleFields(reflect.TypeFor[apmLSPDep]()) {
		if name, _, _ := strings.Cut(field.Tag.Get("yaml"), ","); name != "-" {
			manifest = append(manifest, name)
		}
	}
	slices.Sort(settings)
	slices.Sort(manifest)
	if !slices.Equal(settings, manifest) {
		t.Fatalf("settings LSP fields %v differ from manifest fields %v; render and adopt would drop one", settings, manifest)
	}
}

func TestAgentsSyncAllAdoptWithSettingsBesideTheTemplate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configHome := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	settings := filepath.Join(configHome, "omni", "settings.json")
	writeFile(t, settings, agentsRenderSettings(currentMachineGroupName()))
	writeFile(t, filepath.Join(configHome, "omni", "apm.yml"), "name: template\n")
	a := New(settings)
	a.StateDir = t.TempDir()
	a.SetFallbackExecutor(&executor.MockExecutor{Responses: append(apmInstalled(), executor.MockCall{Stdout: "installed\n"})})
	if _, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(home, ".apm", "apm.yml")
	writeFile(t, live, addPackageLines("    - git: acme/hand-added\n")(agentsRenderGolden))
	done := make(chan error, 1)
	go func() {
		_, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{Adopt: true})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("adopt blocked on the config-root lock the sync already holds")
	}
	if got, _ := os.ReadFile(live); !strings.Contains(string(got), "git: acme/hand-added") {
		t.Fatalf("live = %s, want the adopted package rendered", got)
	}
}
