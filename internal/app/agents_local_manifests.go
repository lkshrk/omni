package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
)

// Local materialization slots are not always repo_url: transitive local sources
// use a digest of their anchored identity to avoid same-basename collisions.
func (lock *agentsPackageLockEvidence) moduleSource() string {
	if lock.Source != "local" {
		return apmPackageSource(lock.RepoURL, lock.VirtualPath)
	}
	if lock.LocalSourceUnsupported || lock.LocalPath == "" || strings.Contains(lock.LocalPath, "\\") {
		return ""
	}
	name := pathpkg.Base(lock.LocalPath)
	if !manifestlessSingleSegment(name) {
		return ""
	}
	if lock.DeclaringParent == "" {
		return pathpkg.Join("_local", name)
	}
	identity := lock.AnchoredLocalPath
	if identity == "" {
		identity = lock.LocalPath
	}
	digest := sha256.Sum256([]byte(identity))
	return pathpkg.Join("_local", hex.EncodeToString(digest[:])[:12], name)
}

type agentsLocalSourceResolver struct {
	workspace string
	parents   map[string][]*agentsPackageLockEvidence
	resolved  map[*agentsPackageLockEvidence]agentsLocalSourceResolution
}

type agentsLocalSourceResolution struct {
	path       string
	identities []agentsModuleManifestIdentity
}

func newAgentsLocalSourceResolver(workspace string, rows []AgentsPackageRow) agentsLocalSourceResolver {
	resolver := agentsLocalSourceResolver{workspace: workspace}
	for _, row := range rows {
		if lock := row.lockEvidence; lock != nil && lock.Source == "local" {
			if resolver.parents == nil {
				resolver.parents = make(map[string][]*agentsPackageLockEvidence)
			}
			resolver.parents[lock.RepoURL] = append(resolver.parents[lock.RepoURL], lock)
		}
	}
	return resolver
}

// Fallback is allowed only for a safely absent materialization slot. An
// unreadable, ambiguous, non-directory, or symlinked slot is not a cache miss.
func (r *agentsLocalSourceResolver) fallback(row AgentsPackageRow) (agentsLocalSourceResolution, bool) {
	lock := row.lockEvidence
	if lock == nil || lock.Source != "local" || lock.LocalSourceUnsupported || lock.OwnerKey == "" {
		return agentsLocalSourceResolution{}, false
	}
	missing, exists, ok := inspectAPMModuleEvidencePath(r.workspace, "apm_modules/"+row.ModuleSource)
	if !ok || exists || !missing.Absent {
		return agentsLocalSourceResolution{}, false
	}
	resolved, ok := r.resolve(lock, make(map[*agentsPackageLockEvidence]bool))
	if !ok {
		return agentsLocalSourceResolution{}, false
	}
	identities := make([]agentsModuleManifestIdentity, 0, len(resolved.identities)+1)
	identities = append(identities, missing)
	identities = append(identities, resolved.identities...)
	return agentsLocalSourceResolution{path: resolved.path, identities: identities}, true
}

// APM anchors root local paths on the workspace and transitive paths on the
// unique local parent's source directory, identified by exact resolved_by repo
// URL. Legitimate ../sibling and absolute out-of-tree sources remain supported.
func (r *agentsLocalSourceResolver) resolve(lock *agentsPackageLockEvidence, visiting map[*agentsPackageLockEvidence]bool) (agentsLocalSourceResolution, bool) {
	if result, found := r.resolved[lock]; found {
		return result, true
	}
	if lock.Source != "local" || lock.LocalSourceUnsupported || lock.LocalPath == "" || lock.OwnerKey == "" || visiting[lock] || len(visiting) >= maxBundleDepth {
		return agentsLocalSourceResolution{}, false
	}
	visiting[lock] = true
	defer delete(visiting, lock)
	anchor := r.workspace
	var identities []agentsModuleManifestIdentity
	if lock.ResolvedBy != "" {
		parents := r.parents[lock.ResolvedBy]
		if len(parents) != 1 {
			return agentsLocalSourceResolution{}, false
		}
		parent, ok := r.resolve(parents[0], visiting)
		if !ok {
			return agentsLocalSourceResolution{}, false
		}
		anchor = parent.path
		identities = append(identities, parent.identities...)
	}
	identity, ok := agentsLocalSourceDirectory(anchor, lock.LocalPath)
	if !ok {
		return agentsLocalSourceResolution{}, false
	}
	lockCopy := *lock
	lockCopy.DeployedFiles = slices.Clone(lock.DeployedFiles)
	identity.Lock = &lockCopy
	identities = append(identities, identity)
	result := agentsLocalSourceResolution{path: identity.Path, identities: identities}
	if r.resolved == nil {
		r.resolved = make(map[*agentsPackageLockEvidence]agentsLocalSourceResolution)
	}
	r.resolved[lock] = result
	return result, true
}

// Walk before cleaning '..' so a symlink cannot disappear through lexical
// normalization. Every visited directory is pinned, but ancestor contents are
// not: unrelated changes elsewhere in HOME must not invalidate local evidence.
func agentsLocalSourceDirectory(anchor, source string) (agentsModuleManifestIdentity, bool) {
	if source == "" || strings.ContainsAny(source, "\\\x00\r\n") {
		return agentsModuleManifestIdentity{}, false
	}
	if source == "~" || strings.HasPrefix(source, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return agentsModuleManifestIdentity{}, false
		}
		source = home + strings.TrimPrefix(source, "~")
	} else if strings.HasPrefix(source, "~") {
		return agentsModuleManifestIdentity{}, false
	}
	if !filepath.IsAbs(source) {
		source = anchor + string(filepath.Separator) + source
	}
	if !filepath.IsAbs(source) {
		return agentsModuleManifestIdentity{}, false
	}
	volume := filepath.VolumeName(source)
	current := volume + string(filepath.Separator)
	var parents []agentsModulePathIdentity
	for _, part := range strings.Split(strings.TrimPrefix(source, volume), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			current = filepath.Dir(current)
			continue
		}
		current = filepath.Join(current, part)
		info, err := agentsModuleLstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || len(parents) >= maxBundleEntries {
			return agentsModuleManifestIdentity{}, false
		}
		parents = append(parents, agentsModulePathIdentity{Path: current, Info: info})
	}
	info, err := agentsModuleLstat(current)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return agentsModuleManifestIdentity{}, false
	}
	return agentsModuleManifestIdentity{
		Path: current, ModuleRoot: current, Info: info, Parents: parents,
		Directory: true, LocalSource: true,
	}, true
}
