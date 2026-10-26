package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

// Manifestless ownership is proved from source declarations, not from a package
// type or a package.json alone. Deployment integrity remains APM audit's job.
func manifestlessContentEvidence(root, module string, row AgentsPackageRow) (map[string]struct{}, []agentsModuleManifestIdentity, bool) {
	lock := row.lockEvidence
	allowMissing := lock != nil && lock.EmptyDeploymentAttested && len(lock.DeployedFiles) == 0
	identities, extensions, ok := manifestlessExtensionEvidence(root, module, allowMissing)
	if !ok {
		return nil, nil, false
	}
	hookIdentities, hooks, ok := manifestlessHookEvidence(root, module)
	if !ok {
		return nil, nil, false
	}
	identities = append(identities, hookIdentities...)
	nativeRoot, ok := manifestlessNativeDeployRoot(filepath.Dir(root))
	if !ok {
		return nil, nil, false
	}
	generatedHooks := make(map[string]struct{})
	if hooks {
		paths, safe := manifestlessGeneratedHookPaths(root, module)
		if !safe {
			return nil, nil, false
		}
		for _, path := range paths {
			generatedHooks[path] = struct{}{}
		}
	}
	known := make(map[string]agentsModuleManifestIdentity, len(identities))
	for _, identity := range identities {
		if prior, exists := known[identity.RelativePath]; !exists || prior.Hash == "" {
			known[identity.RelativePath] = identity
		}
	}
	if len(lock.DeployedFiles) == 0 && (!lock.EmptyDeploymentAttested || !extensions && !hooks) {
		return nil, nil, false
	}
	// The lock attests a single full-package bundle. Its name may be a hash of
	// a host-qualified or local dependency key that Omni does not otherwise use.
	// APM's receipt/audit, not service ownership proof, verifies that identity.
	name := ""
	skills := make(map[string]struct{})
	extensionFiles, hookFiles := 0, 0
	for _, deployed := range lock.DeployedFiles {
		if deployed == "" || strings.TrimSpace(deployed) != deployed || strings.Contains(deployed, "\\") ||
			pathpkg.Clean(deployed) != deployed {
			return nil, nil, false
		}
		if name, isSkill := manifestlessDeployedSkillName(deployed); isSkill {
			if name == "" || name == "." || name == ".." || strings.EqualFold(name, "SKILL.md") {
				return nil, nil, false
			}
			for prior := range skills {
				if strings.EqualFold(prior, name) && prior != name {
					return nil, nil, false
				}
			}
			skills[name] = struct{}{}
			continue
		}
		if hooks && (deployed == pathpkg.Join(nativeRoot, "extensions/apm-hooks-user/index.ts") || deployed == pathpkg.Join(nativeRoot, "apm-hooks-runtime.json")) {
			hookFiles++
			continue
		}
		_, generatedHook := generatedHooks[deployed]
		if hooks && (generatedHook || manifestlessForeignHookPath(deployed, root, module)) {
			hookFiles++
			continue
		}
		prefix := pathpkg.Join(nativeRoot, "extensions") + "/"
		if extensions && strings.HasPrefix(deployed, prefix) {
			bundle, rel, found := strings.Cut(strings.TrimPrefix(deployed, prefix), "/")
			if !found || !manifestlessExtensionName(bundle) || manifestlessReservedExtensionName(bundle) || (name != "" && name != bundle) {
				return nil, nil, false
			}
			name = bundle
			if rel == ".apm-extension.json" {
				extensionFiles++
				continue
			}
			identity, recorded := known[rel]
			if !recorded {
				var safe bool
				identity, _, safe = inspectAPMModuleEvidencePath(module, rel)
				if !safe {
					return nil, nil, false
				}
				known[rel] = identity
				identities = append(identities, identity)
			}
			// APM generates package.json even for designated-directory declarations.
			if (identity.Absent && rel != "package.json") ||
				(!identity.Absent && (identity.Info == nil || !identity.Info.Mode().IsRegular())) {
				return nil, nil, false
			}
			extensionFiles++
			continue
		}
		return nil, nil, false
	}
	switch lock.PackageType {
	case "claude_skill":
		if len(skills) > 1 || len(skills) == 0 && extensionFiles+hookFiles == 0 && !lock.EmptyDeploymentAttested {
			return nil, nil, false
		}
	case "skill_bundle":
		if len(skills) == 0 && extensionFiles+hookFiles == 0 && !lock.EmptyDeploymentAttested {
			return nil, nil, false
		}
	case "extension_package":
		if !extensions {
			return nil, nil, false
		}
	case "hook_package":
		if !hooks {
			return nil, nil, false
		}
	default:
		return nil, nil, false
	}
	return skills, identities, true
}

// Every directory the pinned APM target catalog can deploy a skill under: skills_root_prefixes() of its
// integration/targets.py. A claude_skill or skill bundle deploys to whichever of these the enabled targets use.
var manifestlessSkillRoots = []string{
	".agents/skills",
	".claude/skills",
	".config/opencode/skills",
	".gemini/antigravity-cli/skills",
	".grok/skills",
	".hermes/skills",
	".kiro/skills",
	".openclaw/skills",
	"agent-data/workflows",
}

// manifestlessDeployedSkillName reports the skill directory a deployed path belongs to, if it is under a skills root.
func manifestlessDeployedSkillName(deployed string) (string, bool) {
	for _, root := range manifestlessSkillRoots {
		if rest, found := strings.CutPrefix(deployed, root+"/"); found {
			name, _, _ := strings.Cut(rest, "/")
			return name, true
		}
	}
	return "", false
}

func manifestlessSingleSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\")
}

func manifestlessReservedExtensionName(name string) bool {
	return name == "apm-hooks-user" || name == "apm-hooks-project"
}

func manifestlessExtensionName(value string) bool {
	if !manifestlessSingleSegment(value) || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.') {
			return false
		}
	}
	return true
}

// APM runs global commands in its workspace. PI_CODING_AGENT_DIR is cwd-relative,
// is not whitespace-trimmed or tilde-expanded, and is resolved before lock paths
// are serialized relative to HOME (absolute only when outside HOME).
func manifestlessNativeDeployRoot(workspace string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	home, ok := manifestlessResolvePath(home)
	if !ok {
		return "", false
	}
	value := os.Getenv("PI_CODING_AGENT_DIR")
	if value == "" {
		return ".omp/agent", true
	}
	if !filepath.IsAbs(value) {
		value = workspace + string(filepath.Separator) + value
	}
	resolved, ok := manifestlessResolvePath(value)
	if !ok {
		return "", false
	}
	if agentsPathWithinRoot(home, resolved) {
		rel, err := filepath.Rel(home, resolved)
		return filepath.ToSlash(rel), err == nil
	}
	return filepath.ToSlash(resolved), true
}

// Resolve existing symlinks before dot segments, including dangling targets,
// like pathlib.Path.resolve(strict=False). filepath.Abs would clean link/..
// too early and authorize a different deployment root than APM actually uses.
func manifestlessResolvePath(value string) (string, bool) {
	if !filepath.IsAbs(value) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		value = cwd + string(filepath.Separator) + value
	}
	volume := filepath.VolumeName(value)
	resolved := volume + string(filepath.Separator)
	pending := strings.Split(strings.TrimPrefix(value, volume), string(filepath.Separator))
	links := 0
	for len(pending) != 0 {
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			resolved = filepath.Dir(resolved)
			continue
		}
		candidate := filepath.Join(resolved, part)
		info, err := os.Lstat(candidate)
		if err != nil && !os.IsNotExist(err) {
			return "", false
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 40 {
				return "", false
			}
			target, err := os.Readlink(candidate)
			if err != nil {
				return "", false
			}
			if filepath.IsAbs(target) {
				volume = filepath.VolumeName(target)
				resolved = volume + string(filepath.Separator)
				target = strings.TrimPrefix(target, volume)
			}
			pending = append(strings.Split(target, string(filepath.Separator)), pending...)
			continue
		}
		resolved = candidate
	}
	return resolved, true
}

func manifestlessEvidenceFile(root, module, rel string) (agentsModuleManifestIdentity, []byte, bool, bool) {
	identity, exists, ok := inspectAPMModuleEvidencePath(module, rel)
	if !ok || !exists {
		return identity, nil, exists, ok
	}
	if identity.Info == nil || !identity.Info.Mode().IsRegular() {
		return identity, nil, true, false
	}
	data, info, err := readAPMModuleRegularFile(root, module, identity.Path)
	if err != nil || !os.SameFile(identity.Info, info) {
		return identity, nil, true, false
	}
	sum := sha256.Sum256(data)
	identity.Hash = hex.EncodeToString(sum[:])
	identity.Info = info
	identity.PathOnly = false
	return identity, data, true, true
}

func manifestlessEvidencePayload(root, module, rel string) (agentsModuleManifestIdentity, bool, bool) {
	identity, exists, ok := inspectAPMModuleEvidencePath(module, rel)
	if !ok || !exists {
		return identity, exists, ok
	}
	if identity.Info == nil || !identity.Info.Mode().IsRegular() {
		return identity, true, false
	}
	hash, info, err := hashAPMModuleRegularFile(root, module, identity.Path)
	if err != nil || !os.SameFile(identity.Info, info) {
		return identity, true, false
	}
	identity.Hash, identity.Info = hash, info
	identity.PathOnly = false
	return identity, true, true
}

func hashAPMModuleRegularFile(root, module, filename string) (hash string, identity os.FileInfo, retErr error) {
	file, info, err := openAPMModuleRegularFile(root, module, filename)
	if err != nil {
		return "", nil, err
	}
	defer func() { retErr = errors.Join(retErr, file.Close()) }()
	if info.Size() > maxBundleFileBytes {
		return "", nil, fmt.Errorf("package evidence exceeds runtime file size limit")
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, maxBundleFileBytes+1))
	if err != nil {
		return "", nil, err
	}
	if n > maxBundleFileBytes {
		return "", nil, fmt.Errorf("package evidence exceeds runtime file size limit")
	}
	return hex.EncodeToString(digest.Sum(nil)), info, nil
}

func manifestlessHookEvidence(root, module string) ([]agentsModuleManifestIdentity, bool, bool) {
	var identities []agentsModuleManifestIdentity
	declared := false
	count := 0
	bytes := 0
	for _, dir := range []string{".apm/hooks", "hooks"} {
		identity, exists, ok := inspectAPMModuleEvidencePath(module, dir)
		if !ok || exists && !identity.Directory {
			return nil, false, false
		}
		identities = append(identities, identity)
		if !exists {
			continue
		}
		entries, err := agentsModuleReadDir(identity.Path)
		count += len(entries)
		if err != nil || count > maxBundleEntries {
			return nil, false, false
		}
		for _, entry := range entries {
			if !strings.EqualFold(pathpkg.Ext(entry.Name()), ".json") {
				continue
			}
			if pathpkg.Ext(entry.Name()) != ".json" {
				return nil, false, false
			}
			file, data, exists, ok := manifestlessEvidenceFile(root, module, dir+"/"+entry.Name())
			bytes += len(data)
			if !ok || !exists || bytes > maxAllManifestBytes || !manifestlessHookDeclaration(data) {
				return nil, false, false
			}
			identities = append(identities, file)
			declared = true
		}
	}
	return identities, declared, true
}
