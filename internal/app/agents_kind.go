package app

import (
	"path"
	"strconv"
	"strings"
)

type AgentsPackageKind string

const (
	AgentsKindPlugin AgentsPackageKind = "plugin"
	AgentsKindSkill  AgentsPackageKind = "skill"
	AgentsKindBundle AgentsPackageKind = "bundle"
)

func (r AgentsPackageRow) Kind() AgentsPackageKind {
	switch r.PackageType {
	case "marketplace_plugin":
		return AgentsKindPlugin
	case "skill_bundle", "claude_skill":
		return AgentsKindSkill
	default:
		return AgentsKindBundle
	}
}

// Contents is never empty: a row whose kind or files are unrecognised reads as a plain package.
func (r AgentsPackageRow) Contents() string {
	switch r.Kind() {
	case AgentsKindPlugin:
		return "plugin"
	case AgentsKindSkill:
		if n := len(agentsDeployedContents(r.DeployedPaths).skills); n > 1 {
			return strconv.Itoa(n) + " skills"
		}
		return "skill"
	}
	if r.PackageType != "apm_package" {
		return "package"
	}
	found := agentsDeployedContents(r.DeployedPaths)
	var parts []string
	for _, count := range []struct {
		n                int
		singular, plural string
	}{
		{len(found.skills), "skill", "skills"},
		{len(found.commands), "command", "commands"},
		{len(found.agents), "agent", "agents"},
	} {
		if count.n == 1 {
			parts = append(parts, "1 "+count.singular)
		} else if count.n > 1 {
			parts = append(parts, strconv.Itoa(count.n)+" "+count.plural)
		}
	}
	// Hook files are scripts and configs rather than one file per hook, so only their presence is shown.
	if found.hooks {
		parts = append(parts, "hooks")
	}
	children := map[string]int{}
	for _, child := range r.Provides {
		children[child.Kind]++
	}
	for _, kind := range []string{"mcp", "lsp"} {
		if children[kind] > 0 {
			parts = append(parts, strconv.Itoa(children[kind])+" "+kind)
		}
	}
	if len(parts) == 0 {
		return "package"
	}
	return strings.Join(parts, " · ")
}

type agentsDeployedSummary struct {
	skills, commands, agents map[string]bool
	hooks                    bool
}

// Each client gets its own copy of a file, so items are counted by name rather than by path.
func agentsDeployedContents(files []string) agentsDeployedSummary {
	out := agentsDeployedSummary{skills: map[string]bool{}, commands: map[string]bool{}, agents: map[string]bool{}}
	for _, file := range files {
		parts := strings.Split(path.Clean(strings.ReplaceAll(file, "\\", "/")), "/")
		for i, part := range parts {
			next := ""
			if i+1 < len(parts) {
				next = parts[i+1]
			}
			matched := true
			switch {
			case part == "skills" && next != "":
				out.skills[next] = true
			case (part == "commands" || part == "command") && next != "":
				out.commands[strings.TrimSuffix(next, path.Ext(next))] = true
			case (part == "agents" || part == "agent") && next != "":
				out.agents[strings.TrimSuffix(next, path.Ext(next))] = true
			case part == "hooks" || strings.HasPrefix(part, "hooks."):
				out.hooks = true
			default:
				matched = false
			}
			if matched {
				break
			}
		}
	}
	return out
}
