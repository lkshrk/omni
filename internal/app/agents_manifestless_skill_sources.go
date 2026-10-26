package app

import (
	pathpkg "path"
	"sort"
	"strings"
)

// manifestlessSkillSourceEvidence proves the deployed skill inventory against
// the one source layout APM promotes from: root skills/ when it holds a valid
// skill, otherwise .apm/skills. A root SKILL.md package uses only that file.
func manifestlessSkillSourceEvidence(root, module, packageType string, deployedSkills map[string]struct{}) ([]agentsModuleManifestIdentity, bool) {
	switch packageType {
	case "claude_skill":
		if len(deployedSkills) > 1 {
			return nil, false
		}
		identity, _, exists, ok := manifestlessEvidenceFile(root, module, "SKILL.md")
		if !ok || !exists {
			return nil, false
		}
		return []agentsModuleManifestIdentity{identity}, true
	case "skill_bundle":
	default:
		if len(deployedSkills) == 0 {
			return nil, true
		}
	}

	var identities []agentsModuleManifestIdentity
	examined, bytes := 0, 0
	for _, layout := range []string{"skills", ".apm/skills"} {
		layoutIdentities, names, ok := manifestlessSkillLayout(root, module, layout, &examined, &bytes)
		if !ok {
			return nil, false
		}
		identities = append(identities, layoutIdentities...)
		if len(identities) > maxBundleEntries {
			return nil, false
		}
		if len(names) == 0 {
			if packageType == "skill_bundle" {
				return nil, false
			}
			continue
		}
		for name := range deployedSkills {
			if _, found := names[name]; !found {
				return nil, false
			}
		}
		return identities, true
	}
	// Neither layout can explain the nonempty deployed inventory.
	return nil, false
}

func manifestlessSkillLayout(root, module, layout string, examined, bytes *int) ([]agentsModuleManifestIdentity, map[string]struct{}, bool) {
	identity, exists, ok := inspectAPMModuleEvidencePath(module, layout)
	if !ok {
		return nil, nil, false
	}
	identities := []agentsModuleManifestIdentity{identity}
	if !exists {
		return identities, nil, true
	}
	if !identity.Directory {
		return nil, nil, false
	}
	children, err := agentsModuleReadDir(identity.Path)
	*examined += len(children)
	if err != nil || *examined > maxBundleEntries {
		return nil, nil, false
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	folded := make(map[string]struct{}, len(children))
	names := make(map[string]struct{})
	for _, child := range children {
		name := child.Name()
		key := strings.ToLower(name)
		if _, duplicate := folded[key]; duplicate {
			return nil, nil, false
		}
		folded[key] = struct{}{}
		rel := pathpkg.Join(layout, name)
		childIdentity, childExists, ok := inspectAPMModuleEvidencePath(module, rel)
		if !ok || !childExists {
			return nil, nil, false
		}
		if !childIdentity.Directory {
			continue
		}
		skill, data, skillExists, ok := manifestlessEvidenceFile(root, module, pathpkg.Join(rel, "SKILL.md"))
		*bytes += len(data)
		if !ok || *bytes > maxAllManifestBytes {
			return nil, nil, false
		}
		if len(identities) >= maxBundleEntries {
			return nil, nil, false
		}
		identities = append(identities, skill)
		if skillExists {
			names[name] = struct{}{}
		}
	}
	return identities, names, true
}
