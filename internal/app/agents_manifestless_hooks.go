package app

import (
	"encoding/json"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// manifestlessHookDeclaration accepts the canonical wrapped or naked event map,
// not a whitelist of client event names. At least one executable handler must
// exist, and malformed typed fields anywhere invalidate the whole document.
func manifestlessHookDeclaration(data []byte) bool {
	_, ok := manifestlessHookEvents(data)
	return ok
}

func manifestlessHookEvents(data []byte) (map[string][]map[string]any, bool) {
	var document map[string]any
	if len(data) > maxBundleManifestBytes || !utf8.Valid(data) || json.Unmarshal(data, &document) != nil || document == nil {
		return nil, false
	}
	events := document
	if wrapped, exists := document["hooks"]; exists {
		var ok bool
		events, ok = wrapped.(map[string]any)
		if !ok {
			return nil, false
		}
	}
	result := make(map[string][]map[string]any, len(events))
	positive, count := false, 0
	for event, value := range events {
		entries, ok := value.([]any)
		if strings.TrimSpace(event) == "" || !ok {
			return nil, false
		}
		for _, value := range entries {
			entry, ok := value.(map[string]any)
			if !ok {
				return nil, false
			}
			count++
			active, valid := manifestlessHookHandler(entry)
			if !valid || count > maxBundleEntries {
				return nil, false
			}
			positive = positive || active
			if value, exists := entry["hooks"]; exists {
				nested, ok := value.([]any)
				if !ok {
					return nil, false
				}
				for _, value := range nested {
					handler, ok := value.(map[string]any)
					if !ok {
						return nil, false
					}
					count++
					active, valid := manifestlessHookHandler(handler)
					if _, deeper := handler["hooks"]; deeper || !valid || count > maxBundleEntries {
						return nil, false
					}
					positive = positive || active
					result[event] = append(result[event], handler)
				}
			} else {
				result[event] = append(result[event], entry)
			}
		}
	}
	return result, positive
}

func manifestlessHookHandler(handler map[string]any) (bool, bool) {
	positive := false
	for _, key := range []string{"command", "bash", "powershell", "windows", "linux", "osx", "type", "prompt", "matcher"} {
		if value, exists := handler[key]; exists {
			text, ok := value.(string)
			if !ok {
				return false, false
			}
			if key != "type" && key != "prompt" && key != "matcher" && strings.TrimSpace(text) != "" {
				positive = true
			}
		}
	}
	for _, key := range []string{"timeout", "timeoutSec"} {
		if value, exists := handler[key]; exists {
			if _, ok := value.(float64); !ok {
				return false, false
			}
		}
	}
	kind, _ := handler["type"].(string)
	if _, present := handler["type"]; present && kind != "command" && kind != "prompt" && kind != "agent" {
		return false, false
	}
	prompt, _ := handler["prompt"].(string)
	return positive || (kind == "prompt" || kind == "agent") && strings.TrimSpace(prompt) != "", true
}

// manifestlessForeignHookPath recognizes only user-scope package payload roots.
// Generated descriptors are matched separately against source-derived names.
func manifestlessForeignHookPath(deployed, root, module string) bool {
	if deployed == "" || strings.TrimSpace(deployed) != deployed || strings.Contains(deployed, "\\") || path.Clean(deployed) != deployed {
		return false
	}
	for _, segment := range strings.Split(deployed, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	name := filepath.Base(module)
	if !manifestlessSingleSegment(name) {
		return false
	}
	claude, ok := manifestlessClaudeHookRoot(filepath.Dir(root))
	if !ok {
		return false
	}
	for _, target := range []string{claude, ".cursor", ".codex", ".gemini", ".codeium/windsurf", ".kiro"} {
		prefix := path.Join(target, "hooks", name) + "/"
		if strings.HasPrefix(deployed, prefix) && len(deployed) > len(prefix) {
			return true
		}
	}
	prefix := path.Join(".copilot/hooks/scripts", name) + "/"
	return strings.HasPrefix(deployed, prefix) && len(deployed) > len(prefix)
}

func manifestlessClaudeHookRoot(workspace string) (string, bool) {
	value := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if value == "" {
		return ".claude", true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	if strings.HasPrefix(value, "~") {
		username, rest, _ := strings.Cut(strings.TrimPrefix(value, "~"), "/")
		expanded := home
		if username != "" {
			account, err := user.Lookup(username)
			if err != nil {
				return "", false
			}
			expanded = account.HomeDir
		}
		value = filepath.Join(expanded, rest)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(workspace, value)
	}
	resolved, ok := manifestlessResolvePath(value)
	if !ok {
		return "", false
	}
	home, ok = manifestlessResolvePath(home)
	if !ok {
		return "", false
	}
	if agentsPathWithinRoot(home, resolved) {
		rel, err := filepath.Rel(home, resolved)
		return filepath.ToSlash(rel), err == nil
	}
	return filepath.ToSlash(resolved), true
}

type manifestlessHookSource struct {
	name   string
	events map[string][]map[string]any
}

// The caller has already fingerprinted every shallow JSON descriptor in these
// directories with manifestlessHookEvidence. No additional source is consulted.
func manifestlessGeneratedHookPaths(root, module string) ([]string, bool) {
	var sources []manifestlessHookSource
	count, bytes := 0, 0
	for _, dir := range []string{".apm/hooks", "hooks"} {
		identity, exists, ok := inspectAPMModuleEvidencePath(module, dir)
		if !ok || exists && !identity.Directory {
			return nil, false
		}
		if !exists {
			continue
		}
		children, err := agentsModuleReadDir(identity.Path)
		count += len(children)
		if err != nil || count > maxBundleEntries {
			return nil, false
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if !strings.EqualFold(path.Ext(child.Name()), ".json") {
				continue
			}
			if path.Ext(child.Name()) != ".json" {
				return nil, false
			}
			_, data, exists, ok := manifestlessEvidenceFile(root, module, path.Join(dir, child.Name()))
			bytes += len(data)
			if !ok || !exists || bytes > maxAllManifestBytes {
				return nil, false
			}
			events, ok := manifestlessHookEvents(data)
			if !ok {
				return nil, false
			}
			sources = append(sources, manifestlessHookSource{name: child.Name(), events: events})
		}
	}
	name := filepath.Base(module)
	if !manifestlessSingleSegment(name) {
		return nil, false
	}
	paths := make(map[string]struct{})
	for _, target := range []string{"copilot", "kiro"} {
		specific := false
		for _, source := range sources {
			if routed, selected := manifestlessHookRouting(source.name, target); routed && selected {
				specific = true
			}
		}
		seen := make(map[string]struct{})
		for _, source := range sources {
			routed, selected := manifestlessHookRouting(source.name, target)
			if specific && (!routed || !selected) || !specific && routed {
				continue
			}
			key := strings.ToLower(source.name)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			stem := strings.TrimSuffix(source.name, ".json")
			if target == "copilot" {
				paths[path.Join(".copilot/hooks", name+"-"+stem+".json")] = struct{}{}
				continue
			}
			counts := make(map[string]int)
			for event, handlers := range source.events {
				event = manifestlessKiroEvent(event)
				for _, handler := range handlers {
					if manifestlessKiroAction(handler) {
						counts[event]++
					}
				}
			}
			for event, count := range counts {
				for index := 1; index <= count; index++ {
					filename := manifestlessHookSlug(name) + "-" + manifestlessHookSlug(stem) + "-" + manifestlessHookSlug(event) + "-" + strconv.Itoa(index) + ".json"
					paths[path.Join(".kiro/hooks", filename)] = struct{}{}
					if len(paths) > maxBundleEntries {
						return nil, false
					}
				}
			}
		}
	}
	result := make([]string, 0, len(paths))
	for generated := range paths {
		result = append(result, generated)
	}
	sort.Strings(result)
	return result, true
}

func manifestlessHookRouting(name, target string) (bool, bool) {
	stem := strings.ToLower(strings.TrimSuffix(name, ".json"))
	known := func(token string) bool {
		switch token {
		case "copilot", "vscode", "cursor", "claude", "codex", "gemini", "antigravity", "windsurf", "kiro", "omp":
			return true
		}
		return false
	}
	matches := func(token string) bool { return token == target || token == "vscode" && target == "copilot" }
	if token, prefixed := strings.CutPrefix(stem, "hooks-"); prefixed && known(token) {
		return true, matches(token)
	}
	if !strings.HasSuffix(stem, "-hooks") {
		return false, false
	}
	parts := strings.Split(strings.TrimSuffix(stem, "-hooks"), "-")
	routed, selected := false, false
	for index := len(parts) - 1; index >= 0 && known(parts[index]); index-- {
		routed = true
		selected = selected || matches(parts[index])
	}
	return routed, selected
}

var manifestlessHookSlugPattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func manifestlessHookSlug(value string) string {
	slug := strings.ToLower(strings.Trim(manifestlessHookSlugPattern.ReplaceAllString(strings.TrimSpace(value), "-"), ".-_"))
	if slug == "" {
		return "hook"
	}
	return slug
}

func manifestlessKiroEvent(event string) string {
	switch event {
	case "preToolUse":
		return "PreToolUse"
	case "postToolUse":
		return "PostToolUse"
	case "userPromptSubmit", "promptSubmit":
		return "UserPromptSubmit"
	case "stop", "AgentStop", "agentStop":
		return "Stop"
	case "sessionStart":
		return "SessionStart"
	case "PreTaskExecution", "preTaskExecution":
		return "PreTaskExec"
	case "PostTaskExecution", "postTaskExecution":
		return "PostTaskExec"
	}
	return event
}

func manifestlessKiroAction(handler map[string]any) bool {
	for _, key := range []string{"command", "bash", "powershell", "windows"} {
		if value, exists := handler[key]; exists {
			if value.(string) != "" {
				return true
			}
			break
		}
	}
	prompt, _ := handler["prompt"].(string)
	return strings.TrimSpace(prompt) != ""
}
