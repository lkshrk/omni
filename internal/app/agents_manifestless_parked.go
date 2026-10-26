package app

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// agentsLockDependencyOwnerKey mirrors APM's build_dependency_unique_key, not
// the deliberately host-blind status join. Never trim or strip a lock repo URL:
// APM has already parsed it into a repository path before serializing the lock.
func agentsLockDependencyOwnerKey(dep apmLockDep) (string, bool) {
	if dep.malformed {
		return "", false
	}
	switch dep.Source {
	case "local":
		if !agentsLockIdentityText(dep.LocalPath) {
			return "", false
		}
		if dep.AnchoredLocalPath != "" {
			if !agentsLockIdentityText(dep.AnchoredLocalPath) {
				return "", false
			}
			return "local:" + dep.AnchoredLocalPath, true
		}
		if dep.LocalPath == "." || dep.LocalPath == "local-bundle" {
			return "", false
		}
		return dep.LocalPath, true
	case "", "registry":
	default:
		return "", false
	}
	if dep.LocalPath != "" || dep.AnchoredLocalPath != "" || !agentsLockRepositoryPath(dep.RepoURL) ||
		(dep.Source != "registry" && !strings.Contains(dep.RepoURL, "/")) ||
		(dep.RegistryPrefix != "" && !agentsLockRepositoryPath(dep.RegistryPrefix)) {
		return "", false
	}
	host := strings.ToLower(dep.Host)
	if host != "" && !agentsLockSourceHost(host) {
		return "", false
	}
	repo := dep.RepoURL
	if dep.Source == "registry" || dep.RegistryPrefix != "" {
		repo = strings.ToLower(repo)
	} else {
		effectiveHost := host
		if effectiveHost == "" {
			effectiveHost = "github.com"
			if configured, ok := os.LookupEnv("GITHUB_HOST"); ok {
				effectiveHost = strings.ToLower(configured)
			}
		}
		if agentsLockGitHubHost(effectiveHost) {
			repo = strings.ToLower(repo)
		}
	}
	if dep.IsVirtual && dep.VirtualPath != "" {
		if !agentsLockRepositoryPath(dep.VirtualPath) {
			return "", false
		}
		repo += "/" + dep.VirtualPath
	}
	if dep.RegistryPrefix == "" && host != "" && host != "github.com" {
		return host + "/" + repo, true
	}
	return repo, true
}

func agentsLockIdentityText(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n\t")
}

func agentsLockRepositoryPath(value string) bool {
	if !agentsLockIdentityText(value) || strings.ContainsAny(value, ":\\?#") {
		return false
	}
	for {
		segment, rest, more := strings.Cut(value, "/")
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		if !more {
			return true
		}
		value = rest
	}
}

func agentsLockSourceHost(host string) bool {
	name, port, hasPort := strings.Cut(host, ":")
	if !agentsLockHostname(name) {
		return false
	}
	if hasPort {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return false
		}
		for _, c := range port {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func agentsLockHostname(host string) bool {
	for {
		label, rest, more := strings.Cut(host, ".")
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
		if !more {
			return true
		}
		host = rest
	}
}

// A nil index means unknown or invalid evidence, never an empty ledger.
// Otherwise every known owner maps to whether any ledger row claims it.
type agentsLockDeploymentEvidence map[string]bool

// Build once per status join, not once per dependency. Parked siblings and root
// services are independent, while every row and owner reference must be valid.
func newAgentsLockDeploymentEvidence(lock apmLockfile) agentsLockDeploymentEvidence {
	if lock.Deployments == nil || len(lock.Dependencies) > maxBundleEntries || len(lock.Deployments) > maxBundleEntries {
		return nil
	}
	owners := make(agentsLockDeploymentEvidence, len(lock.Dependencies)+2)
	owners["."] = false
	owners["local-bundle"] = false
	for _, dependency := range lock.Dependencies {
		key, ok := agentsLockDependencyOwnerKey(dependency)
		if !ok {
			return nil
		}
		if _, duplicate := owners[key]; duplicate {
			return nil
		}
		owners[key] = false
	}
	claims := 0
	for _, row := range lock.Deployments {
		claims += len(row.Owners)
		if row.malformed || row.Target == "" || row.Value == "" || row.Scope == "" || len(row.Owners) == 0 || claims > maxBundleEntries {
			return nil
		}
		switch row.Kind {
		case "project-relative", "target-relative", "uri":
		default:
			return nil
		}
		active := false
		for _, claim := range row.Owners {
			if _, known := owners[claim]; !known {
				return nil
			}
			owners[claim] = true
			active = active || claim == row.ActiveOwner
		}
		if !active {
			return nil
		}
	}
	return owners
}

func agentsLockEmptyDeploymentAttested(owners agentsLockDeploymentEvidence, dep apmLockDep, owner string) bool {
	if owner == "" || dep.malformed || (dep.ExecStatus != "gated_pending_approval" && dep.ExecStatus != "denied") ||
		len(dep.DeployedFiles) != 0 || len(dep.DeployedFileHashes) != 0 {
		return false
	}
	claimed, known := owners[owner]
	return known && !claimed
}

// Match APM's is_github_hostname, including its environment-configured GHES
// classification and Azure DevOps exclusions. Lock loading re-normalizes repos.
func agentsLockGitHubHost(host string) bool {
	if host == "github.com" || strings.HasSuffix(host, ".ghe.com") {
		return true
	}
	if host == "" || host == "gitlab.com" || host != strings.ToLower(os.Getenv("GITHUB_HOST")) ||
		!strings.Contains(host, ".") || !agentsLockHostname(host) {
		return false
	}
	if host == "dev.azure.com" || host == "ssh.dev.azure.com" || strings.HasSuffix(host, ".visualstudio.com") {
		return false
	}
	// Configured ADO hosts exclude IP literals, unlike GitHub's FQDN helper.
	if strings.Trim(host, "0123456789.") == "" {
		return true
	}
	if host == agentsLockConfiguredHost(os.Getenv("ADO_HOST")) {
		return false
	}
	for remaining := os.Getenv("APM_ADO_HOSTS"); remaining != ""; {
		value, rest, _ := strings.Cut(remaining, ",")
		if host == agentsLockConfiguredHost(value) {
			return false
		}
		remaining = rest
	}
	return true
}

func agentsLockConfiguredHost(value string) string {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "://") {
		value = "//" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Port() != "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// yaml.v3 coerces scalar numbers and booleans into Go strings. Keep rendering's
// existing decoding behavior, but never use such coercions as ownership proof.
func (d *apmLockDep) UnmarshalYAML(node *yaml.Node) error {
	type rawDep apmLockDep
	var raw rawDep
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*d = apmLockDep(raw)
	d.malformed = node.Kind != yaml.MappingNode
	for i := 0; i+1 < len(node.Content); i += 2 {
		value := node.Content[i+1]
		switch node.Content[i].Value {
		case "repo_url", "host", "source", "registry_prefix", "local_path", "anchored_local_path", "virtual_path", "exec_status", "resolved_by", "declaring_parent", "alias":
			d.malformed = d.malformed || (value.Tag != "!!null" && !agentsLockYAMLString(value))
		case "is_virtual":
			d.malformed = d.malformed || value.Tag != "!!bool"
		case "deployed_files":
			d.malformed = d.malformed || value.Kind != yaml.SequenceNode
		case "deployed_file_hashes":
			d.malformed = d.malformed || (value.Tag != "!!null" && value.Kind != yaml.MappingNode)
		case "deployed_skills":
			// APM migrates this legacy field into deployed_files on load.
			d.malformed = d.malformed || value.Kind != yaml.SequenceNode || len(value.Content) != 0
		case "<<":
			d.malformed = true
		}
	}
	return nil
}

// yaml.v3 drops null elements from slices of structs without invoking the
// element unmarshaler. Preserve the raw ledger cardinality so [null] cannot
// masquerade as an explicitly empty deployment attestation.
func (d *apmLockDeployments) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("APM deployments must be a sequence")
	}
	type rawDeployments []apmLockDeployment
	var raw rawDeployments
	if err := node.Decode(&raw); err != nil {
		return err
	}
	if len(raw) != len(node.Content) {
		return fmt.Errorf("APM deployments contain a null row")
	}
	*d = apmLockDeployments(raw)
	return nil
}

func (d *apmLockDeployment) UnmarshalYAML(node *yaml.Node) error {
	type rawDeployment apmLockDeployment
	var raw rawDeployment
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*d = apmLockDeployment(raw)
	d.malformed = node.Kind != yaml.MappingNode
	for i := 0; i+1 < len(node.Content); i += 2 {
		value := node.Content[i+1]
		switch node.Content[i].Value {
		case "kind", "target", "value", "scope", "active_owner":
			d.malformed = d.malformed || !agentsLockYAMLString(value)
		case "runtime", "content_hash":
			d.malformed = d.malformed || (value.Tag != "!!null" && !agentsLockYAMLString(value))
		case "owners":
			d.malformed = d.malformed || value.Kind != yaml.SequenceNode
			for _, claim := range value.Content {
				d.malformed = d.malformed || !agentsLockYAMLString(claim)
			}
		case "<<":
			d.malformed = true
		}
	}
	return nil
}

func agentsLockYAMLString(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Tag == "!!str"
}
