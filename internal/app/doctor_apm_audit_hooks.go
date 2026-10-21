package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// apm writes user-scope hook commands with the absolute deployment root, and its
// global drift replay runs in a temp root that its normalization never maps back,
// so every hook merge config and its sidecar always diff as "modified". APM itself
// compares only its own slice of those files, recorded in the apm-hooks.json
// sidecar; when every owned sidecar entry still sits verbatim in the live config,
// the drift is that replay artifact rather than an edit.
const apmHooksSidecar = "apm-hooks.json"

var apmHookMergeConfigs = []string{"settings.json", "hooks.json"}

func apmHookDriftIsReplayArtifact(home, detail string) bool {
	kind, rel, found := strings.Cut(detail, ": ")
	if !found || kind != "modified" {
		return false
	}
	dir, name := path.Split(rel)
	if name != apmHooksSidecar && !slices.Contains(apmHookMergeConfigs, name) {
		return false
	}
	owned, ok := apmOwnedSidecarHooks(filepath.Join(home, filepath.FromSlash(dir), apmHooksSidecar))
	if !ok {
		return false
	}
	configs := apmHookMergeConfigs
	if name != apmHooksSidecar {
		configs = []string{name}
	}
	for _, config := range configs {
		if apmOwnedHooksPresent(filepath.Join(home, filepath.FromSlash(dir), config), owned) {
			return true
		}
	}
	return false
}

// apmOwnedSidecarHooks returns, per event, the canonical form of every entry the
// sidecar marks as APM-owned; ok is false when there is nothing owned to compare.
func apmOwnedSidecarHooks(sidecarPath string) (map[string][]string, bool) {
	var sidecar map[string][]map[string]any
	if !readJSON(sidecarPath, &sidecar) {
		return nil, false
	}
	owned := make(map[string][]string)
	for event, entries := range sidecar {
		for _, entry := range entries {
			if _, tagged := entry["_apm_source"]; !tagged {
				continue
			}
			if canonical, ok := canonicalHookEntry(entry); ok {
				owned[event] = append(owned[event], canonical)
			}
		}
	}
	return owned, len(owned) > 0
}

func apmOwnedHooksPresent(configPath string, owned map[string][]string) bool {
	var config struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if !readJSON(configPath, &config) {
		return false
	}
	for event, want := range owned {
		live := make(map[string]int)
		for _, entry := range config.Hooks[event] {
			if canonical, ok := canonicalHookEntry(entry); ok {
				live[canonical]++
			}
		}
		for _, canonical := range want {
			if live[canonical] == 0 {
				return false
			}
			live[canonical]--
		}
	}
	return true
}

// Mirrors apm's own matching: the entry without its ownership marker, keys sorted.
func canonicalHookEntry(entry map[string]any) (string, bool) {
	comparable := make(map[string]any, len(entry))
	for key, value := range entry {
		if key != "_apm_source" {
			comparable[key] = value
		}
	}
	raw, err := json.Marshal(comparable)
	return string(raw), err == nil
}

func readJSON(path string, into any) bool {
	raw, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(raw, into) == nil
}

// apm only tracks entries it marked, so an unmarked copy of one of its hooks makes the hook run twice.
func doctorDuplicateHooks(result *DoctorResult, home string) {
	var config struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	path := filepath.Join(home, ".claude", "settings.json")
	if !readJSON(path, &config) {
		return
	}
	var dupes []string
	for event, entries := range config.Hooks {
		seen := make(map[string]bool, len(entries))
		for _, entry := range entries {
			canonical, ok := canonicalHookEntry(entry)
			if ok && seen[canonical] {
				dupes = append(dupes, event+": "+canonical)
			}
			seen[canonical] = true
		}
	}
	if len(dupes) == 0 {
		result.addCheck("claude-hooks", "Claude hooks", DoctorStatusOK, "no duplicate hooks")
		return
	}
	slices.Sort(dupes)
	result.addCheck("claude-hooks", "Claude hooks", DoctorStatusWarn,
		fmt.Sprintf("%d duplicate hook entr(ies) in %s", len(dupes), path), dupes...)
}
