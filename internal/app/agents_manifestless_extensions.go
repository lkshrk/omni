package app

import (
	"encoding/json"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// manifestlessExtensionEvidence mirrors APM's one-whole-package extension
// discovery. Only explicit native declarations or designated extension sources
// opt in; a package.json or index file at the package root is not sufficient.
func manifestlessExtensionEvidence(root, module string, allowMissing bool) (identities []agentsModuleManifestIdentity, declared bool, ok bool) {
	evidence := manifestlessExtensionInspection{
		root: root, module: module, allowMissing: allowMissing,
		positions: make(map[string]int),
		entries:   make(map[string]struct{}),
	}
	entries, explicit, valid := evidence.declaration("package.json")
	if !valid {
		return nil, explicit, false
	}
	if explicit {
		for _, entry := range entries {
			if !evidence.entry("", entry) {
				return nil, true, false
			}
		}
		if !evidence.uniqueNativeNames() {
			return nil, true, false
		}
		return evidence.identities, true, true
	}

	for _, source := range []string{".omp/extensions", ".pi/extensions", ".apm/extensions"} {
		identity, exists, valid := evidence.inspect(source)
		if !valid {
			return nil, false, false
		}
		if !exists {
			continue
		}
		if !identity.Directory {
			if !identity.Info.Mode().IsRegular() {
				return nil, false, false
			}
			continue
		}
		children, err := agentsModuleReadDir(identity.Path)
		if err != nil || len(children) > maxBundleEntries-evidence.examined {
			return nil, false, false
		}
		evidence.examined += len(children)
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		names := make(map[string]struct{}, len(children))
		for _, child := range children {
			name := child.Name()
			folded := strings.ToLower(name)
			if _, duplicate := names[folded]; duplicate {
				return nil, false, false
			}
			names[folded] = struct{}{}
			rel := path.Join(source, name)
			childIdentity, exists, valid := evidence.inspect(rel)
			if !valid || !exists {
				return nil, false, false
			}
			if !childIdentity.Directory {
				if !childIdentity.Info.Mode().IsRegular() {
					return nil, false, false
				}
				if path.Ext(name) == ".ts" || path.Ext(name) == ".js" {
					if !evidence.entry("", rel) {
						return nil, false, false
					}
				}
				continue
			}

			// APM's receipt marks a deployed bundle, not another source bundle.
			receipt := path.Join(rel, ".apm-extension.json")
			receiptIdentity, receiptExists, valid := evidence.inspect(receipt)
			if !valid {
				return nil, false, false
			}
			if receiptExists && !receiptIdentity.Directory {
				if !receiptIdentity.Info.Mode().IsRegular() {
					return nil, false, false
				}
				continue
			}
			nested, explicit, valid := evidence.declaration(path.Join(rel, "package.json"))
			if !valid {
				return nil, explicit, false
			}
			if explicit {
				for _, entry := range nested {
					if !evidence.entry(rel, entry) {
						return nil, true, false
					}
				}
			} else if _, valid := evidence.index(rel); !valid {
				return nil, false, false
			}
		}
	}
	if !evidence.uniqueNativeNames() {
		return nil, true, false
	}
	return evidence.identities, len(evidence.entries) != 0 || evidence.unbuilt, true
}

type manifestlessExtensionInspection struct {
	root         string
	module       string
	identities   []agentsModuleManifestIdentity
	positions    map[string]int
	entries      map[string]struct{}
	examined     int
	bytes        int
	payloadBytes int64
	allowMissing bool
	unbuilt      bool
}

// Discovery deduplicates resolved entry paths before the installer checks the
// native names. Index entries take their containing directory's name.
func (e *manifestlessExtensionInspection) uniqueNativeNames() bool {
	names := make(map[string]struct{}, len(e.entries))
	for entry := range e.entries {
		name := path.Base(entry)
		if name == "index.ts" || name == "index.js" {
			parent := path.Dir(entry)
			name = path.Base(parent)
			if parent == "." {
				name = filepath.Base(e.module)
			}
		} else {
			name = strings.TrimSuffix(name, path.Ext(name))
		}
		if manifestlessReservedExtensionName(name) {
			return false
		}
		if _, duplicate := names[name]; duplicate {
			return false
		}
		names[name] = struct{}{}
	}
	return true
}

func (e *manifestlessExtensionInspection) remember(identity agentsModuleManifestIdentity) bool {
	if position, exists := e.positions[identity.RelativePath]; exists {
		e.identities[position] = identity
		return true
	}
	if len(e.identities) >= maxBundleEntries {
		return false
	}
	e.positions[identity.RelativePath] = len(e.identities)
	e.identities = append(e.identities, identity)
	return true
}

func (e *manifestlessExtensionInspection) inspect(rel string) (agentsModuleManifestIdentity, bool, bool) {
	if position, exists := e.positions[rel]; exists {
		identity := e.identities[position]
		return identity, !identity.Absent, true
	}
	identity, exists, ok := inspectAPMModuleEvidencePath(e.module, rel)
	if !ok || !e.remember(identity) {
		return identity, exists, false
	}
	return identity, exists, true
}

func (e *manifestlessExtensionInspection) file(rel string) ([]byte, bool, bool) {
	identity, data, exists, ok := manifestlessEvidenceFile(e.root, e.module, rel)
	if !ok || len(data) > maxAllManifestBytes-e.bytes || !e.remember(identity) {
		return nil, exists, false
	}
	e.bytes += len(data)
	return data, exists, true
}

func (e *manifestlessExtensionInspection) payload(rel string) (bool, bool) {
	identity, exists, ok := manifestlessEvidencePayload(e.root, e.module, rel)
	if !ok || !exists || identity.Info == nil || identity.Info.Size() > maxBundleRuntimeBytes-e.payloadBytes || !e.remember(identity) {
		return exists, false
	}
	e.payloadBytes += identity.Info.Size()
	return true, true
}

// Native discovery selects the first non-null namespace object (omp ?? pi),
// not the first extensions field. An empty omp object suppresses pi fallback.
func (e *manifestlessExtensionInspection) declaration(rel string) ([]string, bool, bool) {
	data, exists, ok := e.file(rel)
	if !ok || !exists {
		return nil, false, ok
	}
	var document any
	if !utf8.Valid(data) || json.Unmarshal(data, &document) != nil {
		return nil, false, false
	}
	object, objectOK := document.(map[string]any)
	if !objectOK {
		return nil, false, false
	}
	for _, namespace := range []string{"omp", "pi"} {
		value, present := object[namespace]
		if !present || value == nil {
			continue
		}
		section, sectionOK := value.(map[string]any)
		if !sectionOK {
			return nil, false, false
		}
		value, present = section["extensions"]
		if !present {
			return nil, false, true
		}
		values, arrayOK := value.([]any)
		if !arrayOK || len(values) == 0 || len(values) > maxBundleEntries-e.examined {
			return nil, true, false
		}
		e.examined += len(values)
		entries := make([]string, 0, len(values))
		for _, value := range values {
			entry, stringOK := value.(string)
			if !stringOK || entry == "" {
				return nil, true, false
			}
			entries = append(entries, entry)
		}
		return entries, true, true
	}
	return nil, false, true
}

func (e *manifestlessExtensionInspection) entry(base, value string) bool {
	if value == "" || strings.ContainsAny(value, "\\:") || path.IsAbs(value) {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return false
		}
	}
	rel := path.Join(base, value)
	// PurePosixPath permits '.' as an explicit directory entry. The root is
	// pinned by the index candidates' parent identities rather than a '.' walk.
	if rel == "." {
		found, ok := e.index("")
		if ok && !found && e.allowMissing {
			e.unbuilt = true
			return true
		}
		return found && ok
	}
	identity, exists, ok := e.inspect(rel)
	if !ok {
		return false
	}
	if !exists {
		// Excluded-target admission can park explicit declarations before build.
		// The absent identity pins existing parents and detects later activation.
		e.unbuilt = e.unbuilt || e.allowMissing
		return e.allowMissing
	}
	if identity.Directory {
		found, ok := e.index(rel)
		if ok && !found && e.allowMissing {
			e.unbuilt = true
			return true
		}
		return found && ok
	}
	switch path.Ext(rel) {
	case ".ts", ".js", ".mjs", ".cjs":
	default:
		return false
	}
	if !identity.Info.Mode().IsRegular() {
		return false
	}
	if identity.Hash == "" {
		if exists, ok := e.payload(rel); !ok || !exists {
			return false
		}
	}
	e.entries[rel] = struct{}{}
	return true
}

// index preserves native index.ts-before-index.js precedence and retains
// negative candidates, so adding a higher-priority index invalidates evidence.
func (e *manifestlessExtensionInspection) index(base string) (bool, bool) {
	for _, name := range []string{"index.ts", "index.js"} {
		rel := path.Join(base, name)
		identity, exists, ok := e.inspect(rel)
		if !ok {
			return false, false
		}
		if !exists || identity.Directory {
			continue
		}
		return true, e.entry("", rel)
	}
	return false, true
}
