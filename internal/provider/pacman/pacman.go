package pacman

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lkshrk/omni/internal/executor"
	"github.com/lkshrk/omni/internal/provider"
)

type Provider struct {
	exec executor.Executor
}

func New(exec executor.Executor) *Provider {
	return &Provider{exec: exec}
}

func (p *Provider) Name() string        { return "pacman" }
func (p *Provider) Description() string { return "pacman — Arch Linux package manager" }

func (p *Provider) Available(ctx context.Context) (bool, error) {
	_, _, err := p.exec.Run(ctx, "pacman", "--version")
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (p *Provider) Install(ctx context.Context, tool provider.Tool) error {
	pkg := tool.EffectivePackage()
	rawCmd, rawArgs, _ := p.PrivilegeCommand(provider.PrivilegeActionInstall, tool)
	cmd, args := provider.PrivilegedCommand(rawCmd, rawArgs...)
	return provider.RunCmd(ctx, p.exec, "pacman -S "+pkg, cmd, args...)
}

func (p *Provider) Uninstall(ctx context.Context, tool provider.Tool) error {
	pkg := tool.EffectivePackage()
	rawCmd, rawArgs, _ := p.PrivilegeCommand(provider.PrivilegeActionUninstall, tool)
	cmd, args := provider.PrivilegedCommand(rawCmd, rawArgs...)
	return provider.RunCmd(ctx, p.exec, "pacman -Rs "+pkg, cmd, args...)
}

func (p *Provider) Upgrade(ctx context.Context, tool provider.Tool) error {
	// pacman has no upgrade-single-package command; -S --needed installs the newer synced version or no-ops.
	pkg := tool.EffectivePackage()
	rawCmd, rawArgs, _ := p.PrivilegeCommand(provider.PrivilegeActionUpgrade, tool)
	cmd, args := provider.PrivilegedCommand(rawCmd, rawArgs...)
	return provider.RunCmd(ctx, p.exec, "pacman -S "+pkg, cmd, args...)
}

func (p *Provider) PrivilegePlan(_ context.Context, action provider.PrivilegeAction, tool provider.Tool) (provider.PrivilegePlan, error) {
	return provider.SystemPrivilegePlan(p.Name(), action, tool), nil
}

func (p *Provider) PrivilegeCommand(action provider.PrivilegeAction, tool provider.Tool) (string, []string, bool) {
	pkg := tool.EffectivePackage()
	switch action {
	case provider.PrivilegeActionUninstall:
		return "pacman", []string{"-Rs", "--noconfirm", pkg}, true
	default:
		return "pacman", []string{"-S", "--needed", "--noconfirm", pkg}, true
	}
}

func (p *Provider) IsInstalled(ctx context.Context, tool provider.Tool) (bool, string, error) {
	stdout, _, err := p.exec.Run(ctx, "pacman", "-Q", tool.EffectivePackage())
	if err != nil {
		return false, "", nil // non-zero exit → not installed
	}
	_, version := parsePacmanQLine(strings.TrimSpace(stdout))
	return true, version, nil
}

func (p *Provider) ListInstalled(ctx context.Context) ([]provider.InstalledTool, error) {
	return listQueried(ctx, p.exec, p.Name(), "-Qqen", "-Qn")
}

// InstalledMap returns explicitly installed native pacman packages as lowercase-name→version; foreign packages belong to aur.
func (p *Provider) InstalledMap(ctx context.Context) (map[string]string, error) {
	return installedMapQueried(ctx, p.exec, "-Qqen", "-Qn")
}

func listQueried(ctx context.Context, exec executor.Executor, providerName, explicitFlag, listFlag string) ([]provider.InstalledTool, error) {
	m, err := queryExplicit(ctx, exec, explicitFlag, listFlag, func(name string) string { return name })
	if err != nil {
		return nil, err
	}
	tools := make([]provider.InstalledTool, 0, len(m))
	for _, e := range m {
		tools = append(tools, provider.InstalledTool{
			Tool:    provider.Tool{Name: e.name, Provider: providerName},
			Version: e.version,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func installedMapQueried(ctx context.Context, exec executor.Executor, explicitFlag, listFlag string) (map[string]string, error) {
	m, err := queryExplicit(ctx, exec, explicitFlag, listFlag, strings.ToLower)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(m))
	for key, e := range m {
		out[key] = e.version
	}
	return out, nil
}

type queriedPackage struct{ name, version string }

func queryExplicit(ctx context.Context, exec executor.Executor, explicitFlag, listFlag string, key func(string) string) (map[string]queriedPackage, error) {
	explicit, err := explicitPackages(ctx, exec, explicitFlag)
	if err != nil {
		return nil, err
	}
	stdout, err := runQuery(ctx, exec, listFlag)
	if err != nil {
		return nil, err
	}
	out := make(map[string]queriedPackage)
	for _, line := range strings.Split(stdout, "\n") {
		name, version := parsePacmanQLine(line)
		if name == "" || !explicit[strings.ToLower(name)] {
			continue
		}
		out[key(name)] = queriedPackage{name: name, version: version}
	}
	return out, nil
}

func explicitPackages(ctx context.Context, exec executor.Executor, flag string) (map[string]bool, error) {
	stdout, err := runQuery(ctx, exec, flag)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool)
	for _, line := range strings.Split(stdout, "\n") {
		name := strings.ToLower(strings.TrimSpace(line))
		if name != "" {
			m[name] = true
		}
	}
	return m, nil
}

// pacman exits 1 without output when a filtered query (-Qm, -Qe) matches nothing.
func runQuery(ctx context.Context, exec executor.Executor, flag string) (string, error) {
	stdout, stderr, err := exec.Run(ctx, "pacman", flag)
	if err != nil {
		if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) == "" {
			return "", nil
		}
		return "", executor.WrapError(err, "pacman "+flag, stdout, stderr)
	}
	return stdout, nil
}

// BulkDescribe fetches descriptions for multiple tools via a single `pacman -Si` call.
func (p *Provider) BulkDescribe(ctx context.Context, tools []provider.Tool) (map[string]string, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	args := make([]string, 0, len(tools)+1)
	args = append(args, "-Si")
	for _, t := range tools {
		args = append(args, t.EffectivePackage())
	}
	stdout, stderr, err := p.exec.Run(ctx, "pacman", args...)
	if err != nil {
		return nil, executor.WrapError(err, "pacman -Si", stdout, stderr)
	}
	return parsePacmanBulkDescriptions(stdout), nil
}

// parsePacmanBulkDescriptions extracts name→description from multi-package `pacman -Si` output.
func parsePacmanBulkDescriptions(output string) map[string]string {
	m := make(map[string]string)
	var currentName string
	for _, line := range strings.Split(output, "\n") {
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		switch key {
		case "Name":
			currentName = val
		case "Description":
			if currentName != "" {
				if _, exists := m[currentName]; !exists {
					m[currentName] = val
				}
			}
		}
	}
	return m
}

// Describe fetches a one-line description via `pacman -Si`.
func (p *Provider) Describe(ctx context.Context, tool provider.Tool) (string, error) {
	stdout, stderr, err := p.exec.Run(ctx, "pacman", "-Si", tool.EffectivePackage())
	if err != nil {
		return "", executor.WrapError(err, fmt.Sprintf("pacman -Si %s", tool.EffectivePackage()), stdout, stderr)
	}
	return parsePacmanDescription(stdout), nil
}

// parsePacmanDescription extracts the Description field from `pacman -Si` output.
func parsePacmanDescription(output string) string {
	for _, line := range strings.Split(output, "\n") {
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		if strings.TrimSpace(key) == "Description" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// Expected format: "ripgrep 15.1.0-2" → ("ripgrep", "15.1.0-2")
func parsePacmanQLine(line string) (name, version string) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", ""
	}
	return fields[0], fields[1]
}
