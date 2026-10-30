package app

import "fmt"

// AgentsTemplateHintLines explains that APM only touches the live workspace: a package is lost on the
// next template sync unless the host template declares it, and survives an uninstall until it drops it.
func AgentsTemplateHintLines(spec string, removal bool) []string {
	template, err := AgentsTemplatePath()
	if err != nil {
		return nil
	}
	if removal {
		return []string{
			"",
			fmt.Sprintf("hint: removed from the live workspace — also remove it from the host template (%s)", template),
			"  or the next 'omni agents sync' reinstalls it",
		}
	}
	repo, ref := APMPackageSpec(spec)
	lines := []string{
		"",
		"hint: declare it in the host template so 'omni agents sync' keeps it:",
		"  " + template,
		"    - git: " + repo,
	}
	if ref != "" {
		lines = append(lines, "      ref: "+ref)
	}
	return append(lines, "  add 'path:' for a subdirectory of the repo and 'targets:' to limit deploy targets")
}

// AgentsHintLines points at the agents settings when they render the manifest, else at the host template.
func (a *App) AgentsHintLines(spec string, removal bool) []string {
	cfg, err := a.loadConfig()
	if err != nil || !cfg.Agents.Declares() {
		return AgentsTemplateHintLines(spec, removal)
	}
	if removal {
		return []string{
			"",
			"hint: removed from the live workspace only; it stays declared in the agents settings (settings.d/agents.json)",
			"  and the next 'omni agents sync' reports the manifest as hand-edited until you remove it there",
		}
	}
	return []string{
		"",
		"hint: the agents settings render ~/.apm/apm.yml, so the next 'omni agents sync' reports this install as a hand edit;",
		"  run 'omni agents sync --adopt' to declare it in settings.d/agents.json and this host's group",
	}
}
