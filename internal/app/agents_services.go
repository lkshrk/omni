package app

import (
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/lkshrk/omni/internal/apm"
)

// LSP deploys only to these targets.
var agentsLSPTargets = []string{"claude", "copilot"}

type AgentsServiceRow struct {
	Name           string
	Detail         string
	Command        string
	URLHost        string
	Harnesses      []string
	Targets        []string
	Status         AgentsPackageStatus
	SyncActionable bool
	Unmanaged      bool
}

type AgentsStatus struct {
	Packages       []AgentsPackageRow
	MCP            []AgentsServiceRow
	LSP            []AgentsServiceRow
	Notices        []string
	SyncActionable int
}

// AgentsStatus reports every declared, locked, and harness-deployed agent surface; it reads files only, never the APM CLI.
func (a *App) AgentsStatus() (AgentsStatus, error) {
	manifest, lock, err := readAPMWorkspace()
	if err != nil {
		return AgentsStatus{}, err
	}
	var harness harnessDeployments
	if home, err := os.UserHomeDir(); err == nil {
		harness = readHarnessDeployments(home)
	}
	resolves := memoizedLookPath()
	mcp := mcpServiceInput(manifest, lock)
	mcp.deployed, mcp.deployedConfigs, mcp.resolves = harness.MCP, harness.MCPConfigs, resolves
	lsp := lspServiceInput(manifest, lock)
	lsp.deployed, lsp.resolves = harness.LSP, resolves
	packages := joinAPMPackages(manifest, lock)
	var ownership agentsOwnershipEvidence
	if dir, err := apm.GlobalWorkspaceDir(); err == nil {
		ownership = readAPMModuleManifests(dir, packages)
	}
	mcpRows, lspRows := reconcileAgentsOwnedChildren(packages, manifest, ownership, mcp, lsp)
	syncActionable := 0
	for i := range packages {
		if packages[i].SyncActionable {
			syncActionable++
		}
	}
	for _, rows := range [][]AgentsServiceRow{mcpRows, lspRows} {
		for i := range rows {
			if rows[i].SyncActionable {
				syncActionable++
			}
		}
	}
	return AgentsStatus{
		Packages:       packages,
		MCP:            mcpRows,
		LSP:            lspRows,
		Notices:        harness.Notices,
		SyncActionable: syncActionable,
	}, nil
}

type agentsServiceInput struct {
	declared        []agentsServiceDecl
	locked          []string
	configs         map[string]apmServiceConfig
	targets         []string
	deployed        map[string][]string
	deployedConfigs map[string]map[string]harnessMCPConfig
	resolves        func(string) bool
}

type agentsServiceDecl struct {
	name    string
	detail  string
	command string
	url     string
	remote  bool
	targets []string
}

func mcpServiceInput(manifest apmManifest, lock apmLockfile) agentsServiceInput {
	declared := make([]agentsServiceDecl, 0, len(manifest.Dependencies.MCP))
	for _, dep := range manifest.Dependencies.MCP {
		var targets []string
		if dep.Targets != nil {
			targets = *dep.Targets
		}
		declared = append(declared, agentsServiceDecl{
			name:    dep.Name,
			detail:  dep.Transport,
			command: dep.Command,
			url:     dep.URL,
			remote:  dep.URL != "",
			targets: targets,
		})
	}
	return agentsServiceInput{declared: declared, locked: lock.MCPServers, configs: lock.MCPConfigs, targets: manifest.Targets}
}

func lspServiceInput(manifest apmManifest, lock apmLockfile) agentsServiceInput {
	declared := make([]agentsServiceDecl, 0, len(manifest.Dependencies.LSP))
	for _, dep := range manifest.Dependencies.LSP {
		declared = append(declared, agentsServiceDecl{
			name:    dep.Name,
			detail:  path.Base(dep.Command),
			command: dep.Command,
		})
	}
	// nil = defaults (all targets); empty non-nil = declared targets, none of which LSP deploys to.
	var targets []string
	if len(manifest.Targets) > 0 {
		targets = make([]string, 0, len(agentsLSPTargets))
		for _, target := range manifest.Targets {
			if slices.Contains(agentsLSPTargets, target) {
				targets = append(targets, target)
			}
		}
	}
	return agentsServiceInput{declared: declared, locked: lock.LSPServers, configs: lock.LSPConfigs, targets: targets}
}

func joinAPMServices(in agentsServiceInput) []AgentsServiceRow {
	locked := make(map[string]bool, len(in.locked))
	for _, name := range in.locked {
		locked[name] = true
	}

	rows := make([]AgentsServiceRow, 0, len(in.declared)+len(in.locked))
	declared := make(map[string]bool, len(in.declared))
	for _, dep := range in.declared {
		if dep.name == "" {
			continue
		}
		declared[dep.name] = true
		targets := agentsServiceTargets(in.targets, dep.targets)
		cfg := in.configs[dep.name]
		row := AgentsServiceRow{
			Name:           dep.name,
			Detail:         dep.detail,
			Command:        path.Base(firstNonEmpty(dep.command, cfg.Command)),
			URLHost:        apmURLHost(firstNonEmpty(dep.url, cfg.URL)),
			Harnesses:      in.deployed[dep.name],
			Targets:        targets,
			Status:         AgentsPackageMissing,
			SyncActionable: true,
		}
		if row.Command == "." {
			row.Command = ""
		}
		if locked[dep.name] {
			row.Status, row.SyncActionable = agentsServiceJoinedStatus(dep, cfg, in.deployedConfigs[dep.name], targets, in.resolves)
		}
		if dep.targets != nil && len(targets) == 0 {
			row.Status, row.SyncActionable = AgentsPackageInstalled, false
		}
		if row.Detail == "" {
			row.Detail = agentsServiceDetail(cfg)
		}
		rows = append(rows, row)
	}

	for _, name := range in.locked {
		if name == "" || declared[name] {
			continue
		}
		cfg := in.configs[name]
		rows = append(rows, AgentsServiceRow{
			Name:      name,
			Detail:    agentsServiceDetail(cfg),
			Command:   path.Base(cfg.Command),
			URLHost:   apmURLHost(cfg.URL),
			Harnesses: in.deployed[name],
			Targets:   in.targets,
			Status:    AgentsPackageOrphaned,
		})
	}

	// apm only ever prunes names it locked itself, so a hand-added harness entry is invisible to every apm command.
	for _, name := range slices.Sorted(maps.Keys(in.deployed)) {
		if declared[name] || locked[name] {
			continue
		}
		rows = append(rows, AgentsServiceRow{
			Name:      name,
			Detail:    "unmanaged",
			Unmanaged: true,
			Harnesses: in.deployed[name],
			Targets:   in.deployed[name],
			Status:    AgentsPackageOrphaned,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if ri, rj := agentsPackageStatusRank(rows[i].Status), agentsPackageStatusRank(rows[j].Status); ri != rj {
			return ri < rj
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func agentsServiceTargets(active, selected []string) []string {
	if selected == nil {
		return active
	}
	if active == nil {
		return selected
	}
	targets := make([]string, 0, len(selected))
	for _, target := range selected {
		if slices.Contains(active, target) {
			targets = append(targets, target)
		}
	}
	return targets
}

func agentsServiceJoinedStatus(dep agentsServiceDecl, cfg apmServiceConfig, deployedConfigs map[string]harnessMCPConfig, targets []string, resolves func(string) bool) (AgentsPackageStatus, bool) {
	for harness, deployed := range deployedConfigs {
		if targets != nil && !slices.Contains(targets, harness) {
			continue
		}
		if agentsServiceDriftedOnHarness(cfg, deployed, harness) {
			return AgentsPackageDrifted, true
		}
	}
	return agentsServiceInstalledStatus(dep, cfg, resolves), false
}

var ompEnvPlaceholder = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

// Match omp's nonrecursive expansion, including :- defaults for unset or empty values.
func expandOmpEnv(value string) string {
	return ompEnvPlaceholder.ReplaceAllStringFunc(value, func(placeholder string) string {
		name, fallback, hasFallback := strings.Cut(placeholder[2:len(placeholder)-1], ":-")
		resolved, present := os.LookupEnv(name)
		if hasFallback && (!present || resolved == "") {
			return fallback
		}
		if present {
			return resolved
		}
		return placeholder
	})
}

func agentsServiceDriftedOnHarness(cfg apmServiceConfig, deployed harnessMCPConfig, harness string) bool {
	if harness != harnessOmp {
		return agentsServiceDrifted(cfg, deployed)
	}
	// APM's portable spelling is translated before omp loads and expands its config.
	expandLock := func(value string) string {
		return expandOmpEnv(strings.ReplaceAll(value, "${env:", "${"))
	}
	if expandLock(cfg.Command) != expandOmpEnv(deployed.Command) || expandLock(cfg.URL) != expandOmpEnv(deployed.URL) || len(cfg.Args) != len(deployed.Args) {
		return true
	}
	for i, arg := range cfg.Args {
		if expandLock(arg) != expandOmpEnv(deployed.Args[i]) {
			return true
		}
	}
	return false
}

// One PATH walk per command per refresh: the MCP and LSP joins routinely name the same binaries.
func memoizedLookPath() func(string) bool {
	seen := map[string]bool{}
	return func(command string) bool {
		resolved, ok := seen[command]
		if !ok {
			_, err := exec.LookPath(command)
			resolved = err == nil
			seen[command] = resolved
		}
		return resolved
	}
}

// apm neither detects nor repairs a deployed value that no longer matches the lock, so the tab is the only place it can surface.
func agentsServiceDrifted(cfg apmServiceConfig, deployed harnessMCPConfig) bool {
	command, ok := expandAPMEnv(cfg.Command)
	if !ok {
		return false
	}
	url, ok := expandAPMEnv(cfg.URL)
	if !ok {
		return false
	}
	if command != deployed.Command || url != deployed.URL {
		return true
	}
	if len(cfg.Args) != len(deployed.Args) {
		return true
	}
	for i, arg := range cfg.Args {
		expanded, ok := expandAPMEnv(arg)
		if !ok {
			return false
		}
		if expanded != deployed.Args[i] {
			return true
		}
	}
	return false
}

// The lock keeps ${env:VAR} verbatim while claude.json stores it expanded; an unset var means no comparable value, never drift.
func expandAPMEnv(value string) (string, bool) {
	const open = "${env:"
	var out strings.Builder
	for {
		start := strings.Index(value, open)
		if start < 0 {
			out.WriteString(value)
			return out.String(), true
		}
		end := strings.Index(value[start:], "}")
		if end < 0 {
			out.WriteString(value)
			return out.String(), true
		}
		name := value[start+len(open) : start+end]
		resolved, present := os.LookupEnv(name)
		if !present {
			return "", false
		}
		out.WriteString(value[:start])
		out.WriteString(resolved)
		value = value[start+end+1:]
	}
}

// APM writes an entry into the lock without ever checking that its binary exists, so the tab checks it here.
func agentsServiceInstalledStatus(dep agentsServiceDecl, cfg apmServiceConfig, resolves func(string) bool) AgentsPackageStatus {
	if dep.remote || cfg.URL != "" {
		return AgentsPackageInstalled
	}
	command := dep.command
	if command == "" {
		command = cfg.Command
	}
	if strings.TrimSpace(command) == "" {
		return AgentsPackageInstalled
	}
	if resolves == nil {
		resolves = memoizedLookPath()
	}
	if !resolves(command) {
		return AgentsPackageUnavailable
	}
	return AgentsPackageInstalled
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Only the host is shown: a url can carry credentials or a token path.
func apmURLHost(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Hostname()
}

func agentsServiceDetail(cfg apmServiceConfig) string {
	if cfg.Transport != "" {
		return cfg.Transport
	}
	if cfg.Command != "" {
		return path.Base(cfg.Command)
	}
	return ""
}
