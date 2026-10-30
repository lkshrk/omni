package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"gopkg.in/yaml.v3"

	"github.com/lkshrk/omni/internal/apm"
	"github.com/lkshrk/omni/internal/config"
)

// AgentsManifestDriftError reports a hand edit of ~/.apm/apm.yml since omni last rendered it.
type AgentsManifestDriftError struct {
	Path string
	// Diff prints no values beyond entry identities; it stays out of Error(), which reaches the trace log.
	Diff string
}

func (e *AgentsManifestDriftError) Error() string {
	return fmt.Sprintf("%s was edited since omni last rendered it; run 'omni agents sync --adopt' to keep the new entries in the agents settings or 'omni agents sync --force' to overwrite", e.Path)
}

func agentsManifestDiff(live, rendered []byte) string {
	liveSummary, err := manifestSummary(live)
	if err != nil {
		return "(no diff: " + err.Error() + ")\n"
	}
	renderedSummary, err := manifestSummary(rendered)
	if err != nil {
		return "(no diff: " + err.Error() + ")\n"
	}
	diff := udiff.Unified("~/.apm/apm.yml (live)", "~/.apm/apm.yml (rendered from settings)", string(liveSummary), string(renderedSummary))
	if diff == "" {
		return "(only formatting differs)\n"
	}
	return diff
}

func decodeSingleYAMLDocument(raw []byte, dst any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return errors.New("the manifest is not valid YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("the manifest holds more than one YAML document")
	}
	return nil
}

// manifestSummary prints one line per entry: identity, other field names and a keyed hash of their values.
func manifestSummary(raw []byte) ([]byte, error) {
	var decoded any
	if err := decodeSingleYAMLDocument(raw, &decoded); err != nil {
		return nil, err
	}
	top := summaryMap(decoded)
	dependencies := summaryMap(top["dependencies"])
	var buf strings.Builder
	topRest := withoutKeys(top, "dependencies")
	fmt.Fprintf(&buf, "manifest targets=%s%s %s\n", summaryList(top["targets"]), summaryFields(top, "dependencies", "targets"), summaryHash(topRest))
	rest := withoutKeys(dependencies, "apm", "mcp", "lsp")
	for _, kind := range []string{"apm", "mcp", "lsp"} {
		if _, isList := dependencies[kind].([]any); !isList && dependencies[kind] != nil {
			rest[kind] = dependencies[kind]
		}
	}
	if len(rest) > 0 {
		fmt.Fprintf(&buf, "dependencies%s %s\n", summaryFields(rest), summaryHash(rest))
	}
	packages, _ := dependencies["apm"].([]any)
	for _, entry := range packages {
		fields := summaryMap(entry)
		if spec, ok := entry.(string); ok {
			if isAPMLocalSpec(spec) {
				fields = map[string]any{"path": spec}
			} else {
				parsed := parseAgentPackageSpec(spec, true)
				fields = map[string]any{"git": parsed.source}
				if parsed.ref != "" {
					fields["ref"] = parsed.ref
				}
			}
		}
		source, _ := fields["git"].(string)
		fmt.Fprintf(&buf, "package %s path=%s ref=%s targets=%s%s %s\n", summarySource(source),
			summaryScalar(fields["path"]), summaryScalar(fields["ref"]), summaryList(fields["targets"]), summaryFields(fields, "git", "name", "path", "ref", "targets"), summaryHash(withoutKeys(fields, "name")))
	}
	for _, kind := range []string{"mcp", "lsp"} {
		entries, _ := dependencies[kind].([]any)
		for _, entry := range entries {
			fields := summaryMap(entry)
			fmt.Fprintf(&buf, "%s %s targets=%s%s %s\n", kind, summaryScalar(fields["name"]), summaryList(fields["targets"]), summaryFields(fields, "name", "targets"), summaryHash(entry))
		}
	}
	return []byte(buf.String()), nil
}

// summaryMap accepts the map[any]any yaml.v3 builds for non-string keys, so such entries still count.
func summaryMap(value any) map[string]any {
	switch value := value.(type) {
	case map[string]any:
		return value
	case map[any]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[fmt.Sprint(key)] = item
		}
		return out
	case nil:
		return nil
	default:
		return map[string]any{"<entry>": value}
	}
}

var summaryIdentifier = regexp.MustCompile(`^[A-Za-z0-9._~/@:+…-]*$`)

func summaryScalar(value any) string {
	if value == nil {
		return "-"
	}
	text := fmt.Sprint(value)
	if !summaryIdentifier.MatchString(text) || len(text) > 200 {
		return "<value>"
	}
	return text
}

func summaryList(value any) string {
	list, ok := value.([]any)
	if !ok {
		return summaryScalar(value)
	}
	parts := make([]string, 0, len(list))
	for _, item := range list {
		parts = append(parts, summaryScalar(item))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func summarySource(source string) string {
	return summaryScalar(parseAgentPackageSpec(source, true).display)
}

var summaryFieldNames = []string{
	"apm", "args", "author", "command", "compilation", "cwd", "dependencies", "description", "devDependencies", "env",
	"executables", "extensionToLanguage", "extra", "git", "headers", "includes", "initializationOptions", "license",
	"lsp", "marketplace", "maxRestarts", "mcp", "name", "package", "path", "ref", "registry", "restartOnCrash", "scripts",
	"shutdownTimeout", "startupTimeout", "targets", "tools", "transport", "type", "url", "version", "workspaceFolder",
}

func withoutKeys(fields map[string]any, drop ...string) map[string]any {
	rest := make(map[string]any, len(fields))
	for key, value := range fields {
		if !slices.Contains(drop, key) {
			rest[key] = value
		}
	}
	return rest
}

// summaryFields names the fields not printed as identity; unknown names print as <field>.
func summaryFields(fields map[string]any, shown ...string) string {
	keys := slices.Sorted(maps.Keys(withoutKeys(fields, shown...)))
	if len(keys) == 0 {
		return ""
	}
	for i, key := range keys {
		if !slices.Contains(summaryFieldNames, key) {
			keys[i] = "<field>"
		}
	}
	return " fields=" + strings.Join(keys, ",")
}

// summaryKey makes printed hashes useless for guessing a value offline; both sides are hashed in this process.
var summaryKey = func() []byte {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
}()

func summaryHash(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(fmt.Sprintf("%#v", value))
	}
	mac := hmac.New(sha256.New, summaryKey)
	_, _ = mac.Write(data)
	return "#" + hex.EncodeToString(mac.Sum(nil))[:8]
}

func errAdoptCredentials(source string) error {
	return fmt.Errorf("adopt: package %s carries credentials or a query in its URL; declare it by hand without them", summarySource(source))
}

// renderedManifestDrift returns a live manifest that matches neither rendered nor the snapshot; no snapshot is drift.
func renderedManifestDrift(workspaceDir, stateDir string, rendered []byte) ([]byte, error) {
	live, err := os.ReadFile(filepath.Join(workspaceDir, "apm.yml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect live manifest: %w", err)
	}
	if manifestHash(live) == manifestHash(rendered) {
		return nil, nil
	}
	state, err := os.ReadFile(filepath.Join(stateDir, renderStateName))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && strings.TrimSpace(string(state)) == manifestHash(live) {
		return nil, nil
	}
	return live, nil
}

// writeRenderedManifest writes through a symlinked apm.yml; 0600 because it can carry header and env values.
func writeRenderedManifest(workspaceDir string, rendered []byte) error {
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(workspaceDir, "apm.yml")
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil {
			return fmt.Errorf("%s is a symlink whose target is missing; restore or remove it before syncing", target)
		}
		target = resolved
	}
	if err := replaceFile(target, rendered, 0o600); err != nil {
		return fmt.Errorf("write rendered APM manifest: %w", err)
	}
	return nil
}

type agentsAdoption struct {
	names    []string
	warnings []string
}

// adoptAgentsManifest declares unknown live entries in settings and lists every adopted name in the host group.
func (a *App) adoptAgentsManifest(live []byte, hostname string) (agentsAdoption, error) {
	entries, err := parseAdoptableManifest(live)
	if err != nil {
		return agentsAdoption{}, err
	}
	// A validated dry pass first, so a refused adoption leaves every settings file untouched.
	preview, err := a.loadConfig()
	if err != nil {
		return agentsAdoption{}, fmt.Errorf("loading config: %w", err)
	}
	adoption, err := planAgentsAdoption(preview, entries, hostname)
	if err != nil || len(adoption.names) == 0 {
		return adoption, err
	}
	pv := config.ProviderValidation{}
	if a.registry != nil {
		pv = a.providerValidation()
	}
	if errs := fatalValidationErrors(config.ValidateRoot(preview, pv)); len(errs) > 0 {
		return agentsAdoption{}, fmt.Errorf("adopt: the adopted entries would not validate; declare them by hand: %w", config.ValidationErrors(errs))
	}
	a.configMu.Lock()
	err = config.EnsureAgentsFragment(a.ConfigPath)
	a.configMu.Unlock()
	if err != nil {
		return agentsAdoption{}, err
	}
	err = a.withConfig(func(cfg *config.RootConfig) error {
		var planErr error
		adoption, planErr = planAgentsAdoption(cfg, entries, hostname)
		if planErr == nil && len(adoption.names) == 0 {
			return errSkipSave
		}
		return planErr
	})
	return adoption, err
}

type adoptableManifest struct {
	packages []config.AgentPackage
	mcp      []config.AgentMCPServer
	lsp      []config.AgentLSPServer
}

var (
	// !!timestamp keeps its text so the next render writes what the manifest had; other tags are refused.
	adoptableTypedTags = []string{"!!bool", "!!float", "!!int", "!!null", "!!str"}
	// APM reads these as strings, so their source text is kept rather than YAML's typed value.
	adoptableTextKeys = []string{"args", "env", "headers", "targets", "tools"}
)

func parseAdoptableManifest(live []byte) (adoptableManifest, error) {
	var out adoptableManifest
	var doc struct {
		Dependencies struct {
			APM []yaml.Node `yaml:"apm"`
			MCP []yaml.Node `yaml:"mcp"`
			LSP []yaml.Node `yaml:"lsp"`
		} `yaml:"dependencies"`
	}
	if err := decodeSingleYAMLDocument(live, &doc); err != nil {
		return out, fmt.Errorf("adopt: %w", err)
	}
	for _, node := range doc.Dependencies.APM {
		pkg, err := adoptablePackage(&node)
		if err != nil {
			return out, err
		}
		out.packages = append(out.packages, pkg)
	}
	for _, node := range doc.Dependencies.MCP {
		mapping, name, err := adoptableMapping(&node, "MCP server")
		if err != nil {
			return out, err
		}
		if registry, ok := mapping["registry"].(bool); !ok || registry {
			return out, fmt.Errorf("adopt: registry MCP server %q cannot be declared in the agents settings; remove it from ~/.apm/apm.yml or re-run with --force", name)
		}
		delete(mapping, "registry")
		if _, ok := mapping["client"]; ok {
			return out, fmt.Errorf("adopt: MCP server %q has field \"client\", which APM does not read; remove it from ~/.apm/apm.yml or re-run with --force", name)
		}
		var server config.AgentMCPServer
		if err := decodeAdoptedMapping(mapping, &server); err != nil {
			return out, fmt.Errorf("adopt: MCP server %q: %w", name, err)
		}
		server.NormalizeNumbers()
		out.mcp = append(out.mcp, server)
	}
	for _, node := range doc.Dependencies.LSP {
		mapping, name, err := adoptableMapping(&node, "LSP server")
		if err != nil {
			return out, err
		}
		var server config.AgentLSPServer
		if err := decodeAdoptedMapping(mapping, &server); err != nil {
			return out, fmt.Errorf("adopt: LSP server %q: %w", name, err)
		}
		server.NormalizeNumbers()
		out.lsp = append(out.lsp, server)
	}
	return out, nil
}

func adoptablePackage(node *yaml.Node) (config.AgentPackage, error) {
	if node.Kind == yaml.ScalarNode {
		if isAPMLocalSpec(node.Value) {
			return config.AgentPackage{Path: node.Value}, nil
		}
		parsed := parseAgentPackageSpec(node.Value, true)
		if parsed.credentials {
			return config.AgentPackage{}, errAdoptCredentials(node.Value)
		}
		return config.AgentPackage{Source: parsed.source, Ref: parsed.ref}, nil
	}
	mapping, _, err := adoptableMapping(node, "package")
	if err != nil {
		return config.AgentPackage{}, err
	}
	if _, ok := mapping["marketplace"]; ok {
		return config.AgentPackage{}, fmt.Errorf("adopt: marketplace package %v cannot be declared in the agents settings yet; remove it from ~/.apm/apm.yml or re-run with --force", summaryScalar(mapping["name"]))
	}
	var pkg struct {
		Git     string   `json:"git"`
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Ref     string   `json:"ref"`
		Targets []string `json:"targets"`
	}
	if err := decodeAdoptedMapping(mapping, &pkg); err != nil {
		return config.AgentPackage{}, fmt.Errorf("adopt: package %s: %w", summarySource(fmt.Sprint(mapping["git"])), err)
	}
	if parseAgentPackageSpec(pkg.Git, false).credentials {
		return config.AgentPackage{}, errAdoptCredentials(pkg.Git)
	}
	return config.AgentPackage{Name: pkg.Name, Source: pkg.Git, Path: pkg.Path, Ref: pkg.Ref, Targets: pkg.Targets}, nil
}

// adoptableMapping decodes one manifest entry, keeping the source text of its adoptableTextKeys values.
func adoptableMapping(node *yaml.Node, kind string) (map[string]any, string, error) {
	if node.Kind != yaml.MappingNode {
		return nil, "", fmt.Errorf("adopt: invalid %s entry", kind)
	}
	mapping := make(map[string]any, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if node.Content[i].Kind != yaml.ScalarNode || key == "<<" {
			return nil, "", fmt.Errorf("adopt: %s entries using YAML merge or alias keys cannot be adopted; expand them in ~/.apm/apm.yml or re-run with --force", kind)
		}
		value, err := adoptableNodeValue(node.Content[i+1], slices.Contains(adoptableTextKeys, key))
		if err != nil {
			return nil, "", fmt.Errorf("adopt: %s entry cannot be adopted (%v); expand it in ~/.apm/apm.yml or re-run with --force", kind, err)
		}
		mapping[key] = value
	}
	name, _ := mapping["name"].(string)
	return mapping, name, nil
}

func adoptableNodeValue(node *yaml.Node, keepText bool) (any, error) {
	switch node.Kind {
	case yaml.AliasNode:
		return nil, fmt.Errorf("aliases are not supported")
	case yaml.ScalarNode:
		tag := node.ShortTag()
		if keepText && (tag == "!!null" || !slices.Contains(adoptableTypedTags, tag) && tag != "!!timestamp") {
			return nil, fmt.Errorf("value tagged %s cannot be kept as written", tag)
		}
		if keepText || tag == "!!timestamp" {
			return node.Value, nil
		}
		if !slices.Contains(adoptableTypedTags, tag) {
			return nil, fmt.Errorf("value tagged %s is not supported", tag)
		}
		var value any
		err := node.Decode(&value)
		return value, err
	case yaml.SequenceNode:
		list := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := adoptableNodeValue(child, keepText)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		return list, nil
	case yaml.MappingNode:
		mapping := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Kind != yaml.ScalarNode || node.Content[i].Value == "<<" {
				return nil, fmt.Errorf("merge and alias keys are not supported")
			}
			value, err := adoptableNodeValue(node.Content[i+1], keepText)
			if err != nil {
				return nil, err
			}
			mapping[node.Content[i].Value] = value
		}
		return mapping, nil
	default:
		return nil, fmt.Errorf("unsupported YAML node")
	}
}

func planAgentsAdoption(cfg *config.RootConfig, entries adoptableManifest, hostname string) (agentsAdoption, error) {
	if cfg.Agents == nil {
		cfg.Agents = &config.AgentsConfig{}
	}
	agents := cfg.Agents
	selected := selectedAgentNames(cfg, hostname)
	var adoption agentsAdoption
	var names []string
	for _, pkg := range entries.packages {
		key := agentPackageKey(pkg.Source, pkg.Path)
		index := slices.IndexFunc(agents.Packages, func(existing config.AgentPackage) bool {
			return agentPackageKey(existing.Source, existing.Path) == key
		})
		if index >= 0 {
			names = append(names, agents.Packages[index].EntryName())
			continue
		}
		if slices.Contains(agents.EntryNames(), pkg.EntryName()) {
			return adoption, fmt.Errorf("adopt: package %s would be named %q, which another agent entry already uses; declare it by hand with a distinct name", summarySource(pkg.Source+pkg.Path), pkg.EntryName())
		}
		agents.Packages = append(agents.Packages, pkg)
		names = append(names, pkg.EntryName())
	}
	for _, server := range entries.mcp {
		if !slices.ContainsFunc(agents.MCP, func(existing config.AgentMCPServer) bool { return existing.Name == server.Name }) {
			agents.MCP = append(agents.MCP, server)
			if hasLiteralAgentSecret(server.Headers) || hasLiteralAgentSecret(server.Env) || hasLiteralAgentSecret(server.Extra) ||
				urlMayCarrySecret(server.URL) || argsMayCarrySecret(server.Args) {
				adoption.warnings = append(adoption.warnings, fmt.Sprintf("MCP server %q was adopted with literal values that may be secrets (headers, env, extra, url or args); replace them with ${env:NAME} before committing the agents settings", server.Name))
			}
		}
		names = append(names, server.Name)
	}
	for _, server := range entries.lsp {
		if !slices.ContainsFunc(agents.LSP, func(existing config.AgentLSPServer) bool { return existing.Name == server.Name }) {
			agents.LSP = append(agents.LSP, server)
			if hasLiteralAgentSecret(server.Env) || argsMayCarrySecret(server.Args) || initializationMayCarrySecret(server.InitializationOptions) {
				adoption.warnings = append(adoption.warnings, fmt.Sprintf("LSP server %q was adopted with literal env, args or initializationOptions values that may be secrets; replace them with ${env:NAME} before committing the agents settings", server.Name))
			}
		}
		names = append(names, server.Name)
	}
	names = slices.DeleteFunc(names, func(name string) bool { return selected[name] })
	if len(names) == 0 {
		return adoption, nil
	}
	group, err := ensureDestinationGroupInConfig(cfg, hostname)
	if err != nil {
		return adoption, err
	}
	for _, name := range names {
		if !slices.Contains(group.Agents, name) {
			group.Agents = append(group.Agents, name)
			adoption.names = append(adoption.names, name)
		}
	}
	return adoption, nil
}

var (
	agentEnvReference = regexp.MustCompile(`\$\{env:[A-Za-z_][A-Za-z0-9_]*\}`)
	agentAuthScheme   = regexp.MustCompile(`(?i)^\s*((bearer|basic|token)\s*)?$`)
)

// hasLiteralAgentSecret reports a string leaf that is not ${env:NAME} references plus at most an auth scheme word.
func hasLiteralAgentSecret[V any](values map[string]V) bool {
	for _, value := range values {
		if literalAgentSecretIn(value) {
			return true
		}
	}
	return false
}

func literalAgentSecretIn(value any) bool {
	switch value := value.(type) {
	case string:
		return value != "" && (!agentEnvReference.MatchString(value) || !agentAuthScheme.MatchString(agentEnvReference.ReplaceAllString(value, "")))
	case map[string]any:
		return hasLiteralAgentSecret(value)
	case []any:
		return slices.ContainsFunc(value, literalAgentSecretIn)
	}
	return false
}

var (
	agentSecretFlag   = regexp.MustCompile(`(?i)(key|token|secret|password|auth|credential)`)
	agentSecretInline = regexp.MustCompile(`(?i)(\b(bearer|basic)\s+\S|authorization\s*:)`)
)

// initializationMayCarrySecret flags a literal string under a credential-named key anywhere in the options.
func initializationMayCarrySecret(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			if agentSecretFlag.MatchString(key) && literalAgentSecretIn(item) || initializationMayCarrySecret(item) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(value, initializationMayCarrySecret)
	}
	return false
}

func urlMayCarrySecret(raw string) bool {
	literal := agentEnvReference.ReplaceAllString(raw, "")
	return literal != "" && (parseAgentPackageSpec(literal, false).credentials || strings.ContainsAny(literal, "?#"))
}

// argsMayCarrySecret flags auth header text and literal values for credential-named flags or NAME=value pairs.
func argsMayCarrySecret(args []string) bool {
	for i, arg := range args {
		if agentSecretInline.MatchString(agentEnvReference.ReplaceAllString(arg, "")) && literalAgentSecretIn(arg) {
			return true
		}
		name, value, inline := strings.Cut(arg, "=")
		if !agentSecretFlag.MatchString(name) || (!inline && !strings.HasPrefix(name, "-")) {
			continue
		}
		if !inline && i+1 < len(args) {
			value = args[i+1]
		}
		if value != "" && !agentEnvReference.MatchString(value) {
			return true
		}
	}
	return false
}

func agentPackageKey(source, path string) string {
	if strings.TrimSpace(source) == "" {
		return "path\x00" + strings.TrimRight(strings.TrimSpace(path), "/")
	}
	return "git\x00" + apmPackageKey(source, strings.TrimSpace(path))
}

// decodeAdoptedMapping fails on any manifest field the agents settings cannot express, so adopting never drops one.
func decodeAdoptedMapping(mapping map[string]any, dst any) error {
	fields := jsonFieldNames(dst)
	for key := range mapping {
		if !slices.Contains(fields, key) {
			return fmt.Errorf("the agents settings cannot express field %q; declare it by hand or re-run with --force", key)
		}
	}
	data, err := json.Marshal(mapping)
	if err == nil {
		err = config.DecodeStrictJSON(data, dst)
	}
	if err != nil {
		return fmt.Errorf("the agents settings cannot express it (%v); declare it by hand or re-run with --force", err)
	}
	return nil
}

// jsonFieldNames lists dst's exact JSON names, since encoding/json alone would also accept URL for url.
func jsonFieldNames(dst any) []string {
	var names []string
	for _, field := range reflect.VisibleFields(reflect.TypeOf(dst).Elem()) {
		if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

// sameManifest ignores entry order, since adopted entries are appended wherever the hand edit put them.
func sameManifest(left, right []byte) bool {
	a, errA := manifestSummary(left)
	b, errB := manifestSummary(right)
	if errA != nil || errB != nil {
		return false
	}
	linesA, linesB := strings.Split(string(a), "\n"), strings.Split(string(b), "\n")
	slices.Sort(linesA)
	slices.Sort(linesB)
	return slices.Equal(linesA, linesB)
}

type agentsPreAdoption struct {
	cfg      *config.RootConfig
	liveHash string
	notices  []string
}

// adoptBeforeSync declares a hand edit's new entries in settings and records which live manifest it adopted.
func (a *App) adoptBeforeSync(cfg *config.RootConfig) (agentsPreAdoption, error) {
	dir, err := apm.GlobalWorkspaceDir()
	if err != nil {
		return agentsPreAdoption{}, err
	}
	host := currentHostname()
	rendered, err := renderAgentsManifest(cfg, host)
	if err != nil {
		return agentsPreAdoption{}, err
	}
	live, err := renderedManifestDrift(dir, a.StateDir, rendered)
	if err != nil || live == nil {
		return agentsPreAdoption{}, err
	}
	adoption, err := a.adoptAgentsManifest(live, host)
	if err != nil {
		return agentsPreAdoption{}, err
	}
	pre := agentsPreAdoption{liveHash: manifestHash(live), notices: adoption.warnings}
	if len(adoption.names) > 0 {
		pre.notices = append([]string{"adopted into the agents settings: " + strings.Join(adoption.names, ", ")}, pre.notices...)
	}
	if pre.cfg, err = a.loadConfig(); err != nil {
		return pre, fmt.Errorf("loading config: %w", err)
	}
	if rendered, err = renderAgentsManifest(pre.cfg, host); err != nil {
		return pre, err
	}
	if !sameManifest(live, rendered) {
		pre.notices = append(pre.notices, "other hand edits of ~/.apm/apm.yml are replaced by the manifest rendered from the agents settings")
	}
	return pre, nil
}

func (a *App) agentsSyncRenderedLocked(ctx context.Context, opts AgentsSyncAllOptions, cfg *config.RootConfig, adoptedHash string) (AgentsSyncAllResult, error) {
	res := AgentsSyncAllResult{Rendered: true}
	dir, err := apm.GlobalWorkspaceDir()
	if err != nil {
		return res, err
	}
	host := currentHostname()
	rendered, err := renderAgentsManifest(cfg, host)
	if err != nil {
		return res, err
	}
	var notices []string
	if tmpl, err := AgentsTemplatePath(); err == nil {
		if _, statErr := os.Lstat(tmpl); statErr == nil {
			notices = append(notices, fmt.Sprintf("host template %s is ignored because the agents settings declare agent state", tmpl))
		}
	}
	if err := a.resolveRenderedDrift(opts, &res, dir, rendered, adoptedHash); err != nil {
		return res, err
	}
	manifest, preflight, err := preflightAgentsManifest(opts, dir, a.StateDir, "", rendered)
	if err != nil {
		return res, err
	}
	res.Notices = preflight
	plan := agentsInstallPlan{manifest: manifest, candidate: rendered, marketplaces: settingsMarketplaces(cfg.Agents), stateName: renderStateName}
	// A dry run must not rewrite the live manifest; it plans against the existing one.
	if !opts.DryRun {
		if err := writeRenderedManifest(dir, rendered); err != nil {
			return res, err
		}
		// Snapshot before install, so a failed install does not make omni's own render look like a hand edit.
		if err := snapshotLiveManifestAs(dir, a.StateDir, renderStateName); err != nil {
			return res, err
		}
		plan.synced = true
	}
	res, err = a.agentsInstallLocked(ctx, dir, opts, res, plan)
	res.Notices = append(notices, res.Notices...)
	return res, err
}

func (a *App) resolveRenderedDrift(opts AgentsSyncAllOptions, res *AgentsSyncAllResult, dir string, rendered []byte, adoptedHash string) error {
	live, err := renderedManifestDrift(dir, a.StateDir, rendered)
	if err != nil || live == nil || (adoptedHash != "" && manifestHash(live) == adoptedHash) {
		return err
	}
	livePath := filepath.Join(dir, "apm.yml")
	switch {
	case opts.DryRun:
		res.Warning = fmt.Sprintf("%s was edited since omni last rendered it; a real sync needs --adopt or --force", livePath)
		if opts.Adopt {
			res.Warning = fmt.Sprintf("%s was edited since omni last rendered it; a dry run does not adopt, a real --adopt run declares the new entries shown", livePath)
		}
		res.DriftDiff = agentsManifestDiff(live, rendered)
	case opts.Adopt:
		return fmt.Errorf("%s changed while its entries were adopted; run 'omni agents sync --adopt' again", livePath)
	case !opts.Force:
		return &AgentsManifestDriftError{Path: livePath, Diff: agentsManifestDiff(live, rendered)}
	}
	return nil
}
