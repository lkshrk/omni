package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// AgentIgnoreEntry pins one host-local agent artifact that omni must leave alone.
type AgentIgnoreEntry struct {
	Host   string `json:"host"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// AgentPackage is one APM package; Path alone is a local package, Source plus Path a subdirectory of a repository.
type AgentPackage struct {
	Name    string   `json:"name,omitempty"`
	Source  string   `json:"source,omitempty"`
	Path    string   `json:"path,omitempty"`
	Ref     string   `json:"ref,omitempty"`
	Targets []string `json:"targets,omitempty"`
}

func (p AgentPackage) EntryName() string {
	if name := strings.TrimSpace(p.Name); name != "" {
		return name
	}
	base := strings.TrimSpace(p.Path)
	if base == "" {
		base = strings.TrimSpace(p.Source)
	}
	base = strings.TrimSuffix(strings.TrimRight(base, "/"), ".git")
	if idx := strings.LastIndexAny(base, "/:"); idx >= 0 {
		base = base[idx+1:]
	}
	return base
}

// AgentMCPServer is a self-defined MCP server; Targets nil means every active client and an empty list none.
type AgentMCPServer struct {
	Name      string                    `json:"name"`
	Transport string                    `json:"transport"`
	URL       string                    `json:"url,omitempty"`
	Command   string                    `json:"command,omitempty"`
	Args      []string                  `json:"args,omitempty"`
	Cwd       string                    `json:"cwd,omitempty"`
	Env       map[string]string         `json:"env,omitempty"`
	Headers   map[string]string         `json:"headers,omitempty"`
	Tools     []string                  `json:"tools,omitempty"`
	Targets   *[]string                 `json:"targets,omitempty"`
	Extra     map[string]any            `json:"extra,omitempty"`
	Client    map[string]map[string]any `json:"client,omitempty"`
}

type AgentLSPServer struct {
	Name                  string            `json:"name"`
	Transport             string            `json:"transport,omitempty"`
	Command               string            `json:"command,omitempty"`
	Args                  []string          `json:"args,omitempty"`
	Env                   map[string]string `json:"env,omitempty"`
	Cwd                   string            `json:"cwd,omitempty"`
	ExtensionToLanguage   map[string]string `json:"extensionToLanguage,omitempty"`
	InitializationOptions any               `json:"initializationOptions,omitempty"`
	WorkspaceFolder       string            `json:"workspaceFolder,omitempty"`
	StartupTimeout        int               `json:"startupTimeout,omitempty"`
	ShutdownTimeout       int               `json:"shutdownTimeout,omitempty"`
	RestartOnCrash        bool              `json:"restartOnCrash,omitempty"`
	MaxRestarts           int               `json:"maxRestarts,omitempty"`
}

type AgentMarketplace struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type AgentsConfig struct {
	Targets      []string           `json:"targets,omitempty"`
	Packages     []AgentPackage     `json:"packages,omitempty"`
	MCP          []AgentMCPServer   `json:"mcp,omitempty"`
	LSP          []AgentLSPServer   `json:"lsp,omitempty"`
	Marketplaces []AgentMarketplace `json:"marketplaces,omitempty"`
	Ignored      []AgentIgnoreEntry `json:"ignored"`
}

func (a AgentsConfig) MarshalJSON() ([]byte, error) {
	type plain AgentsConfig
	out := plain(a)
	if out.Ignored == nil {
		out.Ignored = []AgentIgnoreEntry{}
	}
	return json.Marshal(out)
}

// Declares reports whether the block declares agent state, which makes omni render ~/.apm/apm.yml from it.
func (a *AgentsConfig) Declares() bool {
	return a != nil && (len(a.Targets) > 0 || len(a.Packages) > 0 || len(a.MCP) > 0 || len(a.LSP) > 0 || len(a.Marketplaces) > 0)
}

// EntryNames lists every declared package, MCP and LSP name, the namespace group "agents" lists select from.
func (a *AgentsConfig) EntryNames() []string {
	if a == nil {
		return nil
	}
	names := make([]string, 0, len(a.Packages)+len(a.MCP)+len(a.LSP))
	for _, pkg := range a.Packages {
		names = append(names, pkg.EntryName())
	}
	for _, server := range a.MCP {
		names = append(names, server.Name)
	}
	for _, server := range a.LSP {
		names = append(names, server.Name)
	}
	return names
}

type agentsBlockKind int

const (
	agentsBlockAbsent agentsBlockKind = iota
	agentsBlockCurrent
	agentsBlockRetired
)

type agentsBlockClass struct {
	kind  agentsBlockKind
	field string
	alias string
}

var (
	agentIgnoreTargets = []string{"claude", "codex"}
	agentIgnoreKinds   = []string{"plugin", "mcp", "marketplace"}
	agentsBlockFields  = []string{"ignored", "lsp", "marketplaces", "mcp", "packages", "targets"}
	// Fields only v23 entries carried; they also catch a legacy block in a fragment, which has no version.
	retiredAgentEntryFields = map[string][]string{
		"marketplaces": {"agents", "marketplaceSource", "owner", "repo", "url"},
		"packages":     {"agents", "skills"},
	}
)

const agentsDeclarationsVersion = 27

// classifyAgentsBlock is the single classifier for the top-level "agents" key and its v24 tombstone.
func classifyAgentsBlock(raw map[string]json.RawMessage) agentsBlockClass {
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		if key != "agents" && strings.EqualFold(key, "agents") {
			return agentsBlockClass{kind: agentsBlockRetired, alias: key}
		}
	}
	block, ok := raw["agents"]
	if !ok {
		return agentsBlockClass{kind: agentsBlockAbsent}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(block, &fields); err != nil || len(fields) == 0 {
		return agentsBlockClass{kind: agentsBlockRetired}
	}
	allowed := agentsBlockFields
	var version int
	if value, ok := raw["version"]; ok && json.Unmarshal(value, &version) == nil && version < agentsDeclarationsVersion {
		allowed = []string{"ignored"}
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(allowed, name) || isJSONNull(fields[name]) {
			return agentsBlockClass{kind: agentsBlockRetired, field: name}
		}
	}
	if value, ok := fields["ignored"]; ok {
		if _, err := parseAgentIgnoreEntries(value); err != nil {
			return agentsBlockClass{kind: agentsBlockRetired, field: "ignored"}
		}
	}
	for _, field := range slices.Sorted(maps.Keys(retiredAgentEntryFields)) {
		var entries []map[string]json.RawMessage
		if json.Unmarshal(fields[field], &entries) != nil {
			continue
		}
		for _, entry := range entries {
			for _, key := range retiredAgentEntryFields[field] {
				if _, ok := entry[key]; ok {
					return agentsBlockClass{kind: agentsBlockRetired, field: field}
				}
			}
		}
	}
	return agentsBlockClass{kind: agentsBlockCurrent}
}

func (c agentsBlockClass) path() string {
	if c.field == "" {
		return "$.agents"
	}
	return "$.agents." + c.field
}

func decodeAgentsBlock(block json.RawMessage) (*AgentsConfig, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(block, &fields); err != nil {
		return nil, err
	}
	out := &AgentsConfig{}
	decode := func(name string, dst any) error {
		value, ok := fields[name]
		if !ok {
			return nil
		}
		if err := DecodeStrictJSON(value, dst); err != nil {
			return fmt.Errorf("agents.%s: %w", name, err)
		}
		return nil
	}
	if value, ok := fields["ignored"]; ok {
		entries, err := parseAgentIgnoreEntries(value)
		if err != nil {
			return nil, err
		}
		out.Ignored = entries
	}
	for _, field := range []struct {
		name string
		dst  any
	}{
		{"targets", &out.Targets},
		{"packages", &out.Packages},
		{"mcp", &out.MCP},
		{"lsp", &out.LSP},
		{"marketplaces", &out.Marketplaces},
	} {
		if err := decode(field.name, field.dst); err != nil {
			return nil, err
		}
	}
	for i := range out.MCP {
		out.MCP[i].NormalizeNumbers()
	}
	for i := range out.LSP {
		out.LSP[i].NormalizeNumbers()
	}
	return out, nil
}

// NormalizeNumbers converts the json.Number values DecodeStrictJSON leaves in untyped fields.
func (s *AgentMCPServer) NormalizeNumbers() {
	if s.Extra != nil {
		s.Extra = normalizeJSONNumbers(s.Extra).(map[string]any)
	}
	for client, fields := range s.Client {
		s.Client[client] = normalizeJSONNumbers(fields).(map[string]any)
	}
}

// NormalizeNumbers converts the json.Number values DecodeStrictJSON leaves in untyped fields.
func (s *AgentLSPServer) NormalizeNumbers() {
	s.InitializationOptions = normalizeJSONNumbers(s.InitializationOptions)
}

// normalizeJSONNumbers turns json.Number into int64 when integral, else float64; overflowing values stay json.Number.
func normalizeJSONNumbers(value any) any {
	switch value := value.(type) {
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return n
		}
		if n, err := strconv.ParseUint(value.String(), 10, 64); err == nil {
			return n
		}
		f, err := value.Float64()
		if err != nil {
			return value
		}
		if f == math.Trunc(f) && math.Abs(f) < 1<<63 {
			return int64(f)
		}
		return f
	case map[string]any:
		for key, item := range value {
			value[key] = normalizeJSONNumbers(item)
		}
		return value
	case []any:
		for i, item := range value {
			value[i] = normalizeJSONNumbers(item)
		}
		return value
	default:
		return value
	}
}

// DecodeStrictJSON rejects undeclared fields and keeps untyped numbers as json.Number.
func DecodeStrictJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	return decoder.Decode(dst)
}

func parseAgentIgnoreEntries(value json.RawMessage) ([]AgentIgnoreEntry, error) {
	var rawEntries []json.RawMessage
	if err := json.Unmarshal(value, &rawEntries); err != nil {
		return nil, err
	}
	if rawEntries == nil {
		return nil, fmt.Errorf("agents.ignored must be an array")
	}
	entries := make([]AgentIgnoreEntry, 0, len(rawEntries))
	for i, rawEntry := range rawEntries {
		entry, err := parseAgentIgnoreEntry(rawEntry)
		if err != nil {
			return nil, fmt.Errorf("agents.ignored[%d]: %w", i, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func parseAgentIgnoreEntry(data json.RawMessage) (AgentIgnoreEntry, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return AgentIgnoreEntry{}, err
	}
	if fields == nil {
		return AgentIgnoreEntry{}, fmt.Errorf("entry must be an object")
	}
	var entry AgentIgnoreEntry
	if err := DecodeStrictJSON(data, &entry); err != nil {
		return AgentIgnoreEntry{}, err
	}
	if strings.TrimSpace(entry.Host) == "" {
		return AgentIgnoreEntry{}, fmt.Errorf("host must be a non-empty string")
	}
	if !slices.Contains(agentIgnoreTargets, entry.Target) {
		return AgentIgnoreEntry{}, fmt.Errorf("target must be one of %s", strings.Join(agentIgnoreTargets, ", "))
	}
	if !slices.Contains(agentIgnoreKinds, entry.Kind) {
		return AgentIgnoreEntry{}, fmt.Errorf("kind must be one of %s", strings.Join(agentIgnoreKinds, ", "))
	}
	if strings.TrimSpace(entry.ID) == "" {
		return AgentIgnoreEntry{}, fmt.Errorf("id must be a non-empty string")
	}
	if _, ok := fields["reason"]; ok && strings.TrimSpace(entry.Reason) == "" {
		return AgentIgnoreEntry{}, fmt.Errorf("reason must be a non-empty string")
	}
	return entry, nil
}
