package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// AgentMCPTransports are the MCP transports an agents.mcp entry may declare.
var AgentMCPTransports = []string{"stdio", "http", "sse", "streamable-http"}

func validateAgents(cfg *RootConfig) []ValidationError {
	var errs []ValidationError
	agents := cfg.Agents
	if agents == nil {
		return nil
	}
	errs = append(errs, validateAgentTargets("$.agents.targets", agents.Targets)...)
	owners := make(map[string]string)
	claim := func(path, name string) {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, ValidationError{Path: path, Message: "entry name is required"})
			return
		}
		if first, dup := owners[name]; dup {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("agent entry name %q is already used by %s; set a distinct name", name, first)})
			return
		}
		owners[name] = path
	}
	for i, pkg := range agents.Packages {
		path := fmt.Sprintf("$.agents.packages[%d]", i)
		claim(path, pkg.EntryName())
		if strings.TrimSpace(pkg.Source) == "" && strings.TrimSpace(pkg.Path) == "" {
			errs = append(errs, ValidationError{Path: path, Message: "source or path is required"})
		}
		if strings.TrimSpace(pkg.Source) == "" && pkg.Path != "" && !portableAgentPath(pkg.Path) {
			errs = append(errs, ValidationError{Path: path + ".path", Message: "local package paths should start with ~/ or ${env:…} to stay portable across hosts", Warn: true})
		}
		errs = append(errs, validateAgentTargets(path+".targets", pkg.Targets)...)
	}
	for i, server := range agents.MCP {
		path := fmt.Sprintf("$.agents.mcp[%d]", i)
		claim(path+".name", server.Name)
		errs = append(errs, validateAgentMCP(path, server)...)
	}
	for i, server := range agents.LSP {
		path := fmt.Sprintf("$.agents.lsp[%d]", i)
		claim(path+".name", server.Name)
		if strings.TrimSpace(server.Command) == "" {
			errs = append(errs, ValidationError{Path: path + ".command", Message: "command is required"})
		}
		if len(server.ExtensionToLanguage) == 0 {
			errs = append(errs, ValidationError{Path: path + ".extensionToLanguage", Message: "extensionToLanguage is required"})
		}
	}
	marketplaces := make(map[string]struct{}, len(agents.Marketplaces))
	for i, marketplace := range agents.Marketplaces {
		path := fmt.Sprintf("$.agents.marketplaces[%d]", i)
		if strings.TrimSpace(marketplace.Name) == "" {
			errs = append(errs, ValidationError{Path: path + ".name", Message: "marketplace name is required"})
		} else if _, dup := marketplaces[marketplace.Name]; dup {
			errs = append(errs, ValidationError{Path: path + ".name", Message: fmt.Sprintf("duplicate marketplace %q", marketplace.Name)})
		}
		marketplaces[marketplace.Name] = struct{}{}
		if strings.TrimSpace(marketplace.Source) == "" {
			errs = append(errs, ValidationError{Path: path + ".source", Message: "marketplace source is required"})
		}
		// Both reach apm marketplace add as arguments, where a leading dash would read as an option.
		for _, field := range [][2]string{{"name", marketplace.Name}, {"source", marketplace.Source}} {
			if strings.HasPrefix(strings.TrimSpace(field[1]), "-") {
				errs = append(errs, ValidationError{Path: path + "." + field[0], Message: "must not start with -"})
			}
		}
	}
	return errs
}

func validateGroupAgents(gi int, group *GroupConfig, declared map[string]struct{}) []ValidationError {
	var errs []ValidationError
	seen := make(map[string]struct{}, len(group.Agents))
	for ai, name := range group.Agents {
		path := fmt.Sprintf("$.groups[%d].agents[%d]", gi, ai)
		if _, ok := declared[name]; !ok {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("missing agent entry %q in agents.packages, agents.mcp or agents.lsp", name)})
		}
		if _, dup := seen[name]; dup {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("duplicate agent membership %q in group %q", name, group.BaseName())})
		}
		seen[name] = struct{}{}
	}
	return errs
}

func validateAgentMCP(path string, server AgentMCPServer) []ValidationError {
	var errs []ValidationError
	switch {
	case !slices.Contains(AgentMCPTransports, server.Transport):
		errs = append(errs, ValidationError{Path: path + ".transport", Message: fmt.Sprintf("transport must be one of %s", strings.Join(AgentMCPTransports, ", "))})
	case server.Transport == "stdio" && strings.TrimSpace(server.Command) == "":
		errs = append(errs, ValidationError{Path: path + ".command", Message: "stdio transport requires command"})
	case server.Transport != "stdio" && strings.TrimSpace(server.URL) == "":
		errs = append(errs, ValidationError{Path: path + ".url", Message: server.Transport + " transport requires url"})
	}
	if server.Targets != nil {
		errs = append(errs, validateAgentTargets(path+".targets", *server.Targets)...)
	}
	for _, client := range slices.Sorted(maps.Keys(server.Client)) {
		if server.Targets != nil && !slices.Contains(*server.Targets, client) {
			errs = append(errs, ValidationError{Path: fmt.Sprintf("%s.client.%q", path, client), Message: fmt.Sprintf("client %q is not in targets", client)})
		}
	}
	if _, err := server.RenderedExtra(); err != nil {
		errs = append(errs, ValidationError{Path: path + ".client", Message: err.Error()})
	}
	return errs
}

// RenderedExtra merges extra and every client's fields into the single extra map APM reads.
func (s AgentMCPServer) RenderedExtra() (map[string]any, error) {
	if len(s.Extra) == 0 && len(s.Client) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(s.Extra))
	source := make(map[string]string, len(s.Extra))
	for key, value := range s.Extra {
		out[key], source[key] = value, "extra"
	}
	for _, client := range slices.Sorted(maps.Keys(s.Client)) {
		for _, key := range slices.Sorted(maps.Keys(s.Client[client])) {
			value := s.Client[client][key]
			if existing, ok := out[key]; ok && !sameJSONValue(existing, value) {
				return nil, fmt.Errorf("field %q differs between %s and client %q; APM has one extra map per server", key, source[key], client)
			}
			out[key] = value
			if _, ok := source[key]; !ok {
				source[key] = fmt.Sprintf("client %q", client)
			}
		}
	}
	return out, nil
}

// sameJSONValue compares by encoded value, so 1 and 1.0 agree.
func sameJSONValue(a, b any) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(left, right)
}

func validateAgentTargets(path string, targets []string) []ValidationError {
	var errs []ValidationError
	for i, target := range targets {
		if strings.TrimSpace(target) == "" {
			errs = append(errs, ValidationError{Path: fmt.Sprintf("%s[%d]", path, i), Message: "target is required"})
		}
	}
	return errs
}

func portableAgentPath(path string) bool {
	return strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "${env:")
}
