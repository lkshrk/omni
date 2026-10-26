package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lkshrk/omni/internal/executor"
)

const (
	localManifestParentRepo = "_local/ai-plugins"
	localManifestParentPath = "~/Dev/dotfiles/apm/ai-plugins"
	localManifestChildRepo  = "_local/omp-skills"
	localManifestChanged    = "APM package evidence changed during ownership preflight"
	localManifestBlocked    = "cannot verify package-owned MCP/LSP declarations"
)

const localManifestStandalone = `  mcp:
  - name: standalone-mcp
    registry: false
    transport: stdio
    command: standalone-server
`

const localManifestLockServices = `mcp_servers:
- standalone-mcp
- omp-child
mcp_configs:
  standalone-mcp:
    name: standalone-mcp
    transport: stdio
    command: standalone-server
  omp-child:
    name: omp-child
    transport: stdio
    command: sh
`

type localLockDep struct {
	repo, name, source, localPath, anchored, resolvedBy, declaringParent string
}

func localManifestLock(deps ...localLockDep) string {
	var lock strings.Builder
	lock.WriteString("lockfile_version: '1'\ndependencies:\n")
	for _, dep := range deps {
		fmt.Fprintf(&lock, "- repo_url: %s\n  name: %s\n  version: 1.0.0\n  package_type: apm_package\n", dep.repo, dep.name)
		for _, field := range [][2]string{
			{"source", dep.source}, {"local_path", dep.localPath}, {"anchored_local_path", dep.anchored},
			{"resolved_by", dep.resolvedBy}, {"declaring_parent", dep.declaringParent},
		} {
			if field[1] != "" {
				fmt.Fprintf(&lock, "  %s: %q\n", field[0], field[1])
			}
		}
	}
	return lock.String()
}

func localManifestSetup(t *testing.T, manifest, lock string) (string, string) {
	t.Helper()
	_ = setupAgentsWorkspace(t, manifest, lock)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(home, ".apm")
}

func localManifestMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func localManifestSymlink(t *testing.T, target, link string) {
	t.Helper()
	localManifestMkdir(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func localManifestAppendLock(t *testing.T, workspace, text string) {
	t.Helper()
	path := filepath.Join(workspace, "apm.lock.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, text...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func localManifestRewriteLock(t *testing.T, workspace string, rewrite func(string) string) {
	t.Helper()
	path := filepath.Join(workspace, "apm.lock.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := rewrite(string(raw))
	if changed == string(raw) {
		t.Fatal("lock rewrite changed nothing")
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLocalManifest(t *testing.T, dir, manifest string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "apm.yml"), manifest)
}

func localServiceManifest(owner, mcp string) string {
	return "name: " + owner + "\ndescription: " + owner + " source\ndependencies:\n  mcp:\n  - name: " + mcp +
		"\n    registry: false\n    transport: stdio\n    command: sh\n"
}

func localManifestExpand(home, path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

// APM materializes a transitive local dependency under a digest of its anchored identity.
func localManifestNestedSlot(workspace, identity, localPath string) string {
	sum := sha256.Sum256([]byte(identity))
	return filepath.Join(workspace, "apm_modules", "_local", hex.EncodeToString(sum[:])[:12], pathpkg.Base(localPath))
}

// localScenario describes one package under test. The builders never create source or module.
type localScenario struct {
	home, workspace string
	source          string // lexical directory the lock resolves to
	module          string // the slot APM would materialize the package in
	name            string // package row that owns the evidence
}

func newRootLocalScenario(t *testing.T, localPath, source string) localScenario {
	t.Helper()
	name := pathpkg.Base(localPath)
	home, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, localPath), localManifestLock(
		localLockDep{repo: "_local/" + name, name: name, source: source, localPath: localPath},
	))
	localManifestMkdir(t, filepath.Join(workspace, "apm_modules", "_local"))
	return localScenario{
		home: home, workspace: workspace, name: name,
		source: filepath.Join(workspace, filepath.FromSlash(localPath)),
		module: filepath.Join(workspace, "apm_modules", "_local", name),
	}
}

func writeNestedLocalParent(t *testing.T, home, workspace string) {
	t.Helper()
	writeLocalManifest(t, filepath.Join(home, "Dev", "dotfiles", "apm", "ai-plugins"), "name: ai-plugins\n")
	writeLocalManifest(t, filepath.Join(workspace, "apm_modules", "_local", "ai-plugins"), "name: ai-plugins\n")
}

// The nested child's parent source and module copy exist; the child's own module slot does not.
func newNestedLocalScenario(t *testing.T, childPath, anchored, source string) localScenario {
	t.Helper()
	home, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, localManifestParentPath), localManifestLock(
		localLockDep{repo: localManifestParentRepo, name: "ai-plugins", source: "local", localPath: localManifestParentPath},
		localLockDep{
			repo: localManifestChildRepo, name: "omp-skills", source: source, localPath: childPath, anchored: anchored,
			resolvedBy: localManifestParentRepo, declaringParent: localManifestParentRepo,
		},
	))
	writeNestedLocalParent(t, home, workspace)
	identity := childPath
	if anchored != "" {
		identity = anchored
	}
	module := localManifestNestedSlot(workspace, identity, childPath)
	if source != "local" {
		module = filepath.Join(workspace, "apm_modules", "_local", "omp-skills")
	}
	return localScenario{
		home: home, workspace: workspace, name: "omp-skills", module: module,
		source: localManifestExpand(home, childPath),
	}
}

// The child's relative path anchors on the declared local parent's source directory.
func newTransitiveLocalScenario(t *testing.T, parentPath, childPath string) localScenario {
	t.Helper()
	home, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, parentPath), localManifestLock(
		localLockDep{repo: localManifestParentRepo, name: "ai-plugins", source: "local", localPath: parentPath},
		localLockDep{
			repo: localManifestChildRepo, name: "omp-skills", source: "local", localPath: childPath,
			resolvedBy: localManifestParentRepo, declaringParent: localManifestParentRepo,
		},
	))
	parentSource := filepath.Join(workspace, filepath.FromSlash(parentPath))
	writeLocalManifest(t, parentSource, "name: ai-plugins\n")
	writeLocalManifest(t, filepath.Join(workspace, "apm_modules", "_local", "ai-plugins"), "name: ai-plugins\n")
	return localScenario{
		home: home, workspace: workspace, name: "omp-skills",
		source: filepath.Join(parentSource, filepath.FromSlash(childPath)),
		module: localManifestNestedSlot(workspace, childPath, childPath),
	}
}

func localManifestEvidence(t *testing.T, workspace string) (agentsOwnershipEvidence, []AgentsPackageRow) {
	t.Helper()
	manifest, lock, err := readAPMWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	rows := joinAPMPackages(manifest, lock)
	return readAPMModuleManifests(workspace, rows), rows
}

func localManifestPreflight(t *testing.T, workspace string) (apmManifest, func() error, error) {
	t.Helper()
	path := filepath.Join(workspace, "apm.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest, verify, _, err := checkAgentsOwnershipPreflight(workspace, t.TempDir(), path, raw)
	return manifest, verify, err
}

// localManifestBaseline proves the standalone root MCP is not blocked and returns the snapshot verifier.
func localManifestBaseline(t *testing.T, workspace string) func() error {
	t.Helper()
	manifest, verify, err := localManifestPreflight(t, workspace)
	if err != nil {
		t.Fatalf("preflight rejected valid local evidence: %v", err)
	}
	if len(manifest.Dependencies.MCP) != 1 || manifest.Dependencies.MCP[0].Name != "standalone-mcp" {
		t.Fatalf("standalone root MCP was not retained: %#v", manifest.Dependencies.MCP)
	}
	if err := verify(); err != nil {
		t.Fatalf("unchanged local evidence rejected: %v", err)
	}
	return verify
}

func requireLocalManifestPath(t *testing.T, got, want string) {
	t.Helper()
	resolvedGot, gotErr := filepath.EvalSymlinks(got)
	resolvedWant, wantErr := filepath.EvalSymlinks(want)
	if gotErr != nil || wantErr != nil || resolvedGot != resolvedWant {
		t.Fatalf("path = %q (%v), want %q (%v)", got, gotErr, want, wantErr)
	}
}

func findLocalManifestChild(t *testing.T, evidence agentsOwnershipEvidence, mcp string) agentsOwnedChild {
	t.Helper()
	for _, child := range evidence.Children {
		if child.Kind == agentsChildMCP && child.Name == mcp {
			return child
		}
	}
	t.Fatalf("no MCP child %q in %#v", mcp, evidence.Children)
	return agentsOwnedChild{}
}

func requireLocalManifestChildOwner(t *testing.T, child agentsOwnedChild, owner, root string) {
	t.Helper()
	if child.Owner != owner || child.MCP == nil || child.MCP.Command != "sh" {
		t.Fatalf("child = %#v", child)
	}
	requireLocalManifestPath(t, child.OwnerRoot, root)
	if child.Fingerprint != agentsMCPFingerprint(*child.MCP, child.OwnerRoot) {
		t.Fatalf("fingerprint does not normalize against owner root %q: %#v", child.OwnerRoot, child)
	}
}

// requireLocalManifestChild expects exactly one owned MCP child and full evidence availability.
func requireLocalManifestChild(t *testing.T, evidence agentsOwnershipEvidence, owner, mcp, root string) {
	t.Helper()
	if len(evidence.Unavailable) != 0 || len(evidence.Children) != 1 {
		t.Fatalf("evidence = %#v", evidence)
	}
	requireLocalManifestChildOwner(t, findLocalManifestChild(t, evidence, mcp), owner, root)
}

func requireLocalManifestBlocked(t *testing.T, workspace, name string) {
	t.Helper()
	_, _, err := localManifestPreflight(t, workspace)
	if err == nil || !strings.Contains(err.Error(), localManifestBlocked) || !strings.Contains(err.Error(), name) {
		t.Fatalf("unverifiable local package %q did not block standalone MCP: %v", name, err)
	}
}

// requireLocalManifestFailsClosed expects no ownership evidence, and with blocked also a refused sync preflight.
func requireLocalManifestFailsClosed(t *testing.T, workspace string, blocked bool, names ...string) {
	t.Helper()
	evidence, _ := localManifestEvidence(t, workspace)
	for _, name := range names {
		if !slices.Contains(evidence.Unavailable, name) {
			t.Fatalf("%q not reported unavailable: %#v", name, evidence)
		}
	}
	if len(evidence.Children) != 0 {
		t.Fatalf("unsafe local source produced children: %#v", evidence.Children)
	}
	if blocked {
		requireLocalManifestBlocked(t, workspace, names[0])
	}
}

func TestLocalManifestNestedSourceProvidesOwnedChild(t *testing.T) {
	for name, childPath := range map[string]func(base string) string{
		"absolute": func(base string) string { return filepath.Join(base, "omp-skills") },
		"home":     func(string) string { return "~/Dev/omp-skills" },
	} {
		t.Run(name, func(t *testing.T) {
			s := newNestedLocalScenario(t, childPath(t.TempDir()), "", "local")
			writeLocalManifest(t, s.source, localServiceManifest("omp-skills", "omp-child"))

			evidence, _ := localManifestEvidence(t, s.workspace)
			requireLocalManifestChild(t, evidence, "omp-skills", "omp-child", s.source)
			for _, slot := range []string{s.module, filepath.Join(s.workspace, "apm_modules", "_local", "omp-skills")} {
				if _, err := os.Lstat(slot); !os.IsNotExist(err) {
					t.Fatalf("local fallback materialized %s: %v", slot, err)
				}
			}
			localManifestBaseline(t, s.workspace)
		})
	}
}

func TestLocalManifestNestedSourceDoesNotBlockExistingLockSyncs(t *testing.T) {
	for name, lockPath := range map[string]func(home string) string{
		"absolute": func(home string) string { return filepath.Join(home, "src", "omp-skills") },
		"home":     func(string) string { return "~/src/omp-skills" },
	} {
		t.Run(name, func(t *testing.T) {
			a, mock, home := setupOwnedSync(t, ownedSyncManifest(localManifestStandalone, localManifestParentPath))
			workspace := filepath.Join(home, ".apm")
			childPath := lockPath(home)
			writeFile(t, filepath.Join(workspace, "apm.lock.yaml"), localManifestLock(
				localLockDep{repo: localManifestParentRepo, name: "ai-plugins", source: "local", localPath: localManifestParentPath},
				localLockDep{
					repo: localManifestChildRepo, name: "omp-skills", source: "local", localPath: childPath,
					resolvedBy: localManifestParentRepo, declaringParent: localManifestParentRepo,
				},
			)+localManifestLockServices)
			writeNestedLocalParent(t, home, workspace)
			writeLocalManifest(t, localManifestExpand(home, childPath), localServiceManifest("omp-skills", "omp-child"))
			mock.Responses = []executor.MockCall{
				{Stdout: "APM CLI version " + apmVersionPin + "\n"},
				{Stdout: "installed\n"},
				{Stdout: "installed\n"},
			}

			for sync := range 2 {
				result, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{})
				if err != nil || result.Output != "installed\n" || len(result.Notices) != 0 {
					t.Fatalf("sync %d result = %#v, err = %v", sync, result, err)
				}
			}
			if len(mock.Calls) != 3 || mock.Calls[2].Args[0] != "install" {
				t.Fatalf("calls = %#v", mock.Calls)
			}

			status, err := a.AgentsStatus()
			if err != nil {
				t.Fatal(err)
			}
			row := ownedPackageRow(t, status.Packages, "omp-skills")
			if row.Status == AgentsPackageUnavailable || strings.Contains(strings.Join(row.Issues, "\n"), "ownership evidence unavailable") {
				t.Fatalf("local child ownership evidence unavailable: %#v", row)
			}
			owned := false
			for _, provided := range row.Provides {
				owned = owned || provided.Kind == "mcp" && provided.Name == "omp-child"
			}
			if !owned {
				t.Fatalf("local child MCP not attributed to its package: %#v", row)
			}
			if !hasAgentsService(status.MCP, "standalone-mcp") || hasAgentsService(status.MCP, "omp-child") {
				t.Fatalf("service rows: standalone retained or owned child duplicated incorrectly: %#v", status.MCP)
			}
		})
	}
}

func TestLocalManifestInstalledModuleCopyStillWins(t *testing.T) {
	for name, build := range map[string]func(t *testing.T) localScenario{
		"root": func(t *testing.T) localScenario { return newRootLocalScenario(t, "./rootpkg", "local") },
		"nested": func(t *testing.T) localScenario {
			return newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := build(t)
			writeLocalManifest(t, s.source, localServiceManifest(s.name, "source-mcp"))
			writeLocalManifest(t, s.module, localServiceManifest(s.name, "module-mcp"))

			evidence, _ := localManifestEvidence(t, s.workspace)
			requireLocalManifestChild(t, evidence, s.name, "module-mcp", s.module)

			verify := localManifestBaseline(t, s.workspace)
			writeLocalManifest(t, s.source, localServiceManifest(s.name, "edited-mcp"))
			if err := verify(); err != nil {
				t.Fatalf("unused local source invalidated installed-module evidence: %v", err)
			}
			if err := os.RemoveAll(s.source); err != nil {
				t.Fatal(err)
			}
			if err := verify(); err != nil {
				t.Fatalf("missing unused local source invalidated installed-module evidence: %v", err)
			}
		})
	}
}

func TestLocalManifestNestedModuleSlotIsDigestOfAnchoredIdentity(t *testing.T) {
	t.Run("unrelated basename slot is not the nested copy", func(t *testing.T) {
		s := newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
		writeLocalManifest(t, filepath.Join(s.workspace, "apm_modules", "_local", "omp-skills"), localServiceManifest("omp-skills", "unrelated-mcp"))
		writeLocalManifest(t, s.source, localServiceManifest("omp-skills", "source-mcp"))

		evidence, _ := localManifestEvidence(t, s.workspace)
		requireLocalManifestChild(t, evidence, "omp-skills", "source-mcp", s.source)
		localManifestBaseline(t, s.workspace)
	})

	t.Run("digest slot wins over unrelated slot and source", func(t *testing.T) {
		s := newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
		writeLocalManifest(t, filepath.Join(s.workspace, "apm_modules", "_local", "omp-skills"), localServiceManifest("omp-skills", "unrelated-mcp"))
		writeLocalManifest(t, s.module, localServiceManifest("omp-skills", "module-mcp"))
		writeLocalManifest(t, s.source, localServiceManifest("omp-skills", "source-mcp"))

		evidence, _ := localManifestEvidence(t, s.workspace)
		requireLocalManifestChild(t, evidence, "omp-skills", "module-mcp", s.module)
		localManifestBaseline(t, s.workspace)
	})

	t.Run("anchored identity selects the slot", func(t *testing.T) {
		childPath := filepath.Join(t.TempDir(), "omp-skills")
		s := newNestedLocalScenario(t, childPath, "anchored/omp-skills", "local")
		plain := localManifestNestedSlot(s.workspace, childPath, childPath)
		if plain == s.module {
			t.Fatal("anchored and plain slots coincide")
		}
		writeLocalManifest(t, plain, localServiceManifest("omp-skills", "plain-digest-mcp"))
		writeLocalManifest(t, s.module, localServiceManifest("omp-skills", "module-mcp"))
		writeLocalManifest(t, s.source, localServiceManifest("omp-skills", "source-mcp"))

		evidence, _ := localManifestEvidence(t, s.workspace)
		requireLocalManifestChild(t, evidence, "omp-skills", "module-mcp", s.module)
	})

	t.Run("local path digest is not read when anchored identity exists", func(t *testing.T) {
		childPath := filepath.Join(t.TempDir(), "omp-skills")
		s := newNestedLocalScenario(t, childPath, "anchored/omp-skills", "local")
		writeLocalManifest(t, localManifestNestedSlot(s.workspace, childPath, childPath), localServiceManifest("omp-skills", "plain-digest-mcp"))
		writeLocalManifest(t, s.source, localServiceManifest("omp-skills", "source-mcp"))

		evidence, _ := localManifestEvidence(t, s.workspace)
		requireLocalManifestChild(t, evidence, "omp-skills", "source-mcp", s.source)
	})

	t.Run("root local dependency ignores digest slots", func(t *testing.T) {
		s := newRootLocalScenario(t, "./rootpkg", "local")
		writeLocalManifest(t, localManifestNestedSlot(s.workspace, "./rootpkg", "./rootpkg"), localServiceManifest("rootpkg", "digest-mcp"))
		writeLocalManifest(t, s.source, localServiceManifest("rootpkg", "source-mcp"))

		evidence, _ := localManifestEvidence(t, s.workspace)
		requireLocalManifestChild(t, evidence, "rootpkg", "source-mcp", s.source)
	})

	t.Run("same basename siblings keep separate slots", func(t *testing.T) {
		home, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, localManifestParentPath), localManifestLock(
			localLockDep{repo: localManifestParentRepo, name: "ai-plugins", source: "local", localPath: localManifestParentPath},
			localLockDep{
				repo: "_local/skills-a", name: "skills-a", source: "local", localPath: "~/Dev/a/omp-skills",
				resolvedBy: localManifestParentRepo, declaringParent: localManifestParentRepo,
			},
			localLockDep{
				repo: "_local/skills-b", name: "skills-b", source: "local", localPath: "~/Dev/b/omp-skills",
				resolvedBy: localManifestParentRepo, declaringParent: localManifestParentRepo,
			},
		))
		writeNestedLocalParent(t, home, workspace)
		sourceA, sourceB := filepath.Join(home, "Dev", "a", "omp-skills"), filepath.Join(home, "Dev", "b", "omp-skills")
		writeLocalManifest(t, sourceA, localServiceManifest("skills-a", "a-source-mcp"))
		writeLocalManifest(t, sourceB, localServiceManifest("skills-b", "b-source-mcp"))
		slotA := localManifestNestedSlot(workspace, "~/Dev/a/omp-skills", "~/Dev/a/omp-skills")
		writeLocalManifest(t, slotA, localServiceManifest("skills-a", "a-module-mcp"))

		evidence, _ := localManifestEvidence(t, workspace)
		if len(evidence.Unavailable) != 0 || len(evidence.Children) != 2 {
			t.Fatalf("evidence = %#v", evidence)
		}
		requireLocalManifestChildOwner(t, findLocalManifestChild(t, evidence, "a-module-mcp"), "skills-a", slotA)
		requireLocalManifestChildOwner(t, findLocalManifestChild(t, evidence, "b-source-mcp"), "skills-b", sourceB)
	})
}

func TestLocalManifestRootRelativePathResolvesFromWorkspace(t *testing.T) {
	for name, localPath := range map[string]string{"dot slash": "./rootpkg", "bare": "rootpkg", "nested": "plugins/rootpkg"} {
		t.Run(name, func(t *testing.T) {
			s := newRootLocalScenario(t, localPath, "local")
			writeLocalManifest(t, s.source, localServiceManifest("rootpkg", "workspace-mcp"))
			cwd := filepath.Join(t.TempDir(), "cwd")
			for _, decoy := range []string{filepath.Join(cwd, localPath), filepath.Join(s.home, localPath)} {
				writeLocalManifest(t, decoy, localServiceManifest("rootpkg", "decoy-mcp"))
			}
			t.Chdir(cwd)

			evidence, _ := localManifestEvidence(t, s.workspace)
			requireLocalManifestChild(t, evidence, "rootpkg", "workspace-mcp", s.source)
			localManifestBaseline(t, s.workspace)
		})
	}
}

func TestLocalManifestTransitiveRelativePathAnchorsOnLocalParentSource(t *testing.T) {
	for name, childPath := range map[string]string{"inside parent": "./omp-skills", "sibling of parent": "../omp-skills"} {
		t.Run(name, func(t *testing.T) {
			s := newTransitiveLocalScenario(t, "plugins/ai-plugins", childPath)
			writeLocalManifest(t, s.source, localServiceManifest("omp-skills", "omp-child"))
			cwd := filepath.Join(t.TempDir(), "nested", "cwd")
			localManifestMkdir(t, cwd)
			for _, anchor := range []string{s.workspace, cwd} {
				if decoy := filepath.Join(anchor, childPath); decoy != s.source {
					writeLocalManifest(t, decoy, localServiceManifest("omp-skills", "decoy-mcp"))
				}
			}
			t.Chdir(cwd)

			evidence, _ := localManifestEvidence(t, s.workspace)
			requireLocalManifestChild(t, evidence, "omp-skills", "omp-child", s.source)
			localManifestBaseline(t, s.workspace)
		})
	}
}

func TestLocalManifestTransitiveChainAnchorsEachParentRecursively(t *testing.T) {
	_, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, "plugins/ai-plugins"), localManifestLock(
		localLockDep{repo: localManifestParentRepo, name: "ai-plugins", source: "local", localPath: "plugins/ai-plugins"},
		localLockDep{
			repo: "_local/mid-pack", name: "mid-pack", source: "local", localPath: "./mid-pack",
			resolvedBy: localManifestParentRepo, declaringParent: localManifestParentRepo,
		},
		localLockDep{
			repo: localManifestChildRepo, name: "omp-skills", source: "local", localPath: "../omp-skills",
			resolvedBy: "_local/mid-pack", declaringParent: "_local/mid-pack",
		},
	))
	root := filepath.Join(workspace, "plugins", "ai-plugins")
	mid, leaf := filepath.Join(root, "mid-pack"), filepath.Join(root, "omp-skills")
	writeLocalManifest(t, root, "name: ai-plugins\n")
	writeLocalManifest(t, filepath.Join(workspace, "apm_modules", "_local", "ai-plugins"), "name: ai-plugins\n")
	writeLocalManifest(t, mid, localServiceManifest("mid-pack", "mid-mcp"))
	writeLocalManifest(t, leaf, localServiceManifest("omp-skills", "leaf-mcp"))
	// Anchoring the leaf on the workspace or on the root instead of its direct parent lands here.
	for _, decoy := range []string{filepath.Join(workspace, "omp-skills"), filepath.Join(workspace, "plugins", "omp-skills")} {
		writeLocalManifest(t, decoy, localServiceManifest("omp-skills", "decoy-mcp"))
	}

	evidence, _ := localManifestEvidence(t, workspace)
	if len(evidence.Unavailable) != 0 || len(evidence.Children) != 2 {
		t.Fatalf("evidence = %#v", evidence)
	}
	requireLocalManifestChildOwner(t, findLocalManifestChild(t, evidence, "mid-mcp"), "mid-pack", mid)
	requireLocalManifestChildOwner(t, findLocalManifestChild(t, evidence, "leaf-mcp"), "omp-skills", leaf)
	localManifestBaseline(t, workspace)
}

func TestLocalManifestFailsClosedForAmbiguousOrUnanchoredParents(t *testing.T) {
	decoy := func(t *testing.T, dirs ...string) {
		t.Helper()
		for _, dir := range dirs {
			writeLocalManifest(t, dir, localServiceManifest("decoy", "decoy-mcp"))
		}
	}
	child := func(path, parent string) localLockDep {
		return localLockDep{
			repo: localManifestChildRepo, name: "omp-skills", source: "local", localPath: path,
			resolvedBy: parent, declaringParent: parent,
		}
	}
	nonLocalParent := func(source string) func(t *testing.T) (string, []string, bool) {
		return func(t *testing.T) (string, []string, bool) {
			_, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, "acme/ai-plugins"), localManifestLock(
				localLockDep{repo: "acme/ai-plugins", name: "ai-plugins", source: source},
				child("./omp-skills", "acme/ai-plugins"),
			))
			module := filepath.Join(workspace, "apm_modules", "acme", "ai-plugins")
			writeLocalManifest(t, module, "name: ai-plugins\n")
			decoy(t, filepath.Join(workspace, "omp-skills"), filepath.Join(module, "omp-skills"))
			return workspace, []string{"omp-skills"}, true
		}
	}

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) (workspace string, unavailable []string, blocked bool)
	}{
		{"multiple local parents", func(t *testing.T) (string, []string, bool) {
			home, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, "a/ai-plugins", "b/ai-plugins"), localManifestLock(
				localLockDep{repo: localManifestParentRepo, name: "ai-plugins-a", source: "local", localPath: "a/ai-plugins"},
				localLockDep{repo: localManifestParentRepo, name: "ai-plugins-b", source: "local", localPath: "b/ai-plugins"},
				child("../omp-skills", localManifestParentRepo),
			))
			writeLocalManifest(t, filepath.Join(workspace, "a", "ai-plugins"), "name: ai-plugins-a\n")
			writeLocalManifest(t, filepath.Join(workspace, "b", "ai-plugins"), "name: ai-plugins-b\n")
			decoy(t, filepath.Join(workspace, "a", "omp-skills"), filepath.Join(workspace, "b", "omp-skills"), filepath.Join(home, "omp-skills"))
			return workspace, []string{"omp-skills"}, true
		}},
		{"cycle", func(t *testing.T) (string, []string, bool) {
			_, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone), localManifestLock(
				localLockDep{
					repo: "_local/cycle-a", name: "cycle-a", source: "local", localPath: "./cycle-a",
					resolvedBy: "_local/cycle-b", declaringParent: "_local/cycle-b",
				},
				localLockDep{
					repo: "_local/cycle-b", name: "cycle-b", source: "local", localPath: "./cycle-b",
					resolvedBy: "_local/cycle-a", declaringParent: "_local/cycle-a",
				},
			))
			decoy(t,
				filepath.Join(workspace, "cycle-a"), filepath.Join(workspace, "cycle-b"),
				filepath.Join(workspace, "cycle-a", "cycle-b"), filepath.Join(workspace, "cycle-b", "cycle-a"),
			)
			return workspace, []string{"cycle-a", "cycle-b"}, false
		}},
		{"self parent", func(t *testing.T) (string, []string, bool) {
			_, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone), localManifestLock(
				localLockDep{
					repo: "_local/self-ref", name: "self-ref", source: "local", localPath: "./self-ref",
					resolvedBy: "_local/self-ref", declaringParent: "_local/self-ref",
				},
			))
			decoy(t, filepath.Join(workspace, "self-ref"), filepath.Join(workspace, "self-ref", "self-ref"))
			return workspace, []string{"self-ref"}, false
		}},
		{"missing parent", func(t *testing.T) (string, []string, bool) {
			_, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone), localManifestLock(
				child("./omp-skills", "_local/ghost"),
			))
			decoy(t, filepath.Join(workspace, "omp-skills"))
			return workspace, []string{"omp-skills"}, false
		}},
		{"inexact parent repo URL", func(t *testing.T) (string, []string, bool) {
			_, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, "plugins/ai-plugins"), localManifestLock(
				localLockDep{repo: localManifestParentRepo, name: "ai-plugins", source: "local", localPath: "plugins/ai-plugins"},
				child("./omp-skills", "_local/AI-plugins"),
			))
			writeLocalManifest(t, filepath.Join(workspace, "plugins", "ai-plugins"), "name: ai-plugins\n")
			decoy(t, filepath.Join(workspace, "omp-skills"), filepath.Join(workspace, "plugins", "ai-plugins", "omp-skills"))
			return workspace, []string{"omp-skills"}, false
		}},
		{"non-local parent without source", nonLocalParent("")},
		{"non-local parent with git source", nonLocalParent("git")},
		{"parent source missing", func(t *testing.T) (string, []string, bool) {
			_, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, "plugins/ai-plugins"), localManifestLock(
				localLockDep{repo: localManifestParentRepo, name: "ai-plugins", source: "local", localPath: "plugins/ai-plugins"},
				child("../omp-skills", localManifestParentRepo),
			))
			writeLocalManifest(t, filepath.Join(workspace, "apm_modules", "_local", "ai-plugins"), "name: ai-plugins\n")
			decoy(t, filepath.Join(workspace, "plugins", "omp-skills"), filepath.Join(workspace, "..", "omp-skills"))
			return workspace, []string{"omp-skills"}, true
		}},
		{"parent source is a symlink", func(t *testing.T) (string, []string, bool) {
			_, workspace := localManifestSetup(t, ownedSyncManifest(localManifestStandalone, "plugins/ai-plugins"), localManifestLock(
				localLockDep{repo: localManifestParentRepo, name: "ai-plugins", source: "local", localPath: "plugins/ai-plugins"},
				child("./omp-skills", localManifestParentRepo),
			))
			writeLocalManifest(t, filepath.Join(workspace, "apm_modules", "_local", "ai-plugins"), "name: ai-plugins\n")
			outside := t.TempDir()
			writeLocalManifest(t, outside, "name: ai-plugins\n")
			decoy(t, filepath.Join(outside, "omp-skills"), filepath.Join(workspace, "omp-skills"))
			localManifestSymlink(t, outside, filepath.Join(workspace, "plugins", "ai-plugins"))
			return workspace, []string{"omp-skills"}, true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace, unavailable, blocked := tc.setup(t)
			requireLocalManifestFailsClosed(t, workspace, blocked, unavailable...)
		})
	}
}

func TestLocalManifestRejectsEvidenceChangedAfterPreflight(t *testing.T) {
	scenarios := map[string]func(t *testing.T) localScenario{
		"root relative": func(t *testing.T) localScenario { return newRootLocalScenario(t, "./rootpkg", "local") },
		"nested absolute": func(t *testing.T) localScenario {
			return newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
		},
		"transitive sibling": func(t *testing.T) localScenario {
			return newTransitiveLocalScenario(t, "plugins/ai-plugins", "../omp-skills")
		},
	}
	mutations := map[string]func(t *testing.T, s localScenario){
		"edited manifest": func(t *testing.T, s localScenario) {
			writeLocalManifest(t, s.source, localServiceManifest(s.name, "edited-mcp"))
		},
		"removed manifest": func(t *testing.T, s localScenario) {
			if err := os.Remove(filepath.Join(s.source, "apm.yml")); err != nil {
				t.Fatal(err)
			}
		},
		"manifest replaced by symlink": func(t *testing.T, s localScenario) {
			manifest := filepath.Join(s.source, "apm.yml")
			if err := os.Rename(manifest, manifest+".real"); err != nil {
				t.Fatal(err)
			}
			localManifestSymlink(t, manifest+".real", manifest)
		},
		"source directory swapped": func(t *testing.T, s localScenario) {
			if err := os.Rename(s.source, s.source+".old"); err != nil {
				t.Fatal(err)
			}
			writeLocalManifest(t, s.source, localServiceManifest(s.name, "source-mcp"))
		},
		"source directory replaced by symlink": func(t *testing.T, s localScenario) {
			if err := os.Rename(s.source, s.source+".old"); err != nil {
				t.Fatal(err)
			}
			localManifestSymlink(t, s.source+".old", s.source)
		},
		"source directory removed": func(t *testing.T, s localScenario) {
			if err := os.RemoveAll(s.source); err != nil {
				t.Fatal(err)
			}
		},
		"child module materialized with manifest": func(t *testing.T, s localScenario) {
			writeLocalManifest(t, s.module, localServiceManifest(s.name, "module-mcp"))
		},
		"child module materialized empty": func(t *testing.T, s localScenario) {
			localManifestMkdir(t, s.module)
		},
	}
	for scenarioName, build := range scenarios {
		for mutationName, mutate := range mutations {
			t.Run(scenarioName+" "+mutationName, func(t *testing.T) {
				s := build(t)
				writeLocalManifest(t, s.source, localServiceManifest(s.name, "source-mcp"))
				verify := localManifestBaseline(t, s.workspace)

				mutate(t, s)
				if err := verify(); err == nil || !strings.Contains(err.Error(), localManifestChanged) {
					t.Fatalf("verify after %s = %v", mutationName, err)
				}
			})
		}
	}
}

func TestLocalManifestRejectsLockMetadataChangedAfterPreflight(t *testing.T) {
	for name, rewrite := range map[string]func(lock string) string{
		"resolved_by": func(lock string) string {
			return strings.Replace(lock, `resolved_by: "_local/ai-plugins"`, `resolved_by: "_local/other"`, 1)
		},
		"declaring_parent": func(lock string) string {
			return strings.Replace(lock, `declaring_parent: "_local/ai-plugins"`, `declaring_parent: "_local/other"`, 1)
		},
		"anchored_local_path": func(lock string) string {
			return lock + "  anchored_local_path: \"anchored/omp-skills\"\n"
		},
		"source": func(lock string) string {
			return strings.ReplaceAll(lock, `source: "local"`, `source: "git"`)
		},
		"local_path": func(lock string) string {
			return strings.Replace(lock, `omp-skills"`, `omp-skills-moved"`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
			writeLocalManifest(t, s.source, localServiceManifest("omp-skills", "omp-child"))
			verify := localManifestBaseline(t, s.workspace)

			localManifestRewriteLock(t, s.workspace, rewrite)
			if err := verify(); err == nil || !strings.Contains(err.Error(), localManifestChanged) {
				t.Fatalf("verify after %s change = %v", name, err)
			}
		})
	}
}

func TestLocalManifestRejectsEscapingSourceSymlinks(t *testing.T) {
	layouts := map[string]func(t *testing.T, ancestor bool) localScenario{
		"root relative": func(t *testing.T, ancestor bool) localScenario {
			if ancestor {
				return newRootLocalScenario(t, "linked/rootpkg", "local")
			}
			return newRootLocalScenario(t, "rootpkg", "local")
		},
		"nested absolute": func(t *testing.T, ancestor bool) localScenario {
			if ancestor {
				return newNestedLocalScenario(t, filepath.Join(t.TempDir(), "linked", "omp-skills"), "", "local")
			}
			return newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
		},
	}
	for layoutName, build := range layouts {
		for _, escape := range []string{"source root", "ancestor", "manifest"} {
			t.Run(layoutName+" "+escape, func(t *testing.T) {
				s := build(t, escape == "ancestor")
				outside := t.TempDir()
				target := filepath.Join(outside, filepath.Base(s.source))
				writeLocalManifest(t, target, localServiceManifest(s.name, "escaped-mcp"))
				switch escape {
				case "source root":
					localManifestSymlink(t, target, s.source)
				case "ancestor":
					localManifestSymlink(t, outside, filepath.Dir(s.source))
				case "manifest":
					localManifestMkdir(t, s.source)
					localManifestSymlink(t, filepath.Join(target, "apm.yml"), filepath.Join(s.source, "apm.yml"))
				}
				requireLocalManifestFailsClosed(t, s.workspace, true, s.name)
			})
		}
	}
}

func TestLocalManifestUnavailableSources(t *testing.T) {
	const secret = "TOP_SECRET_LOCAL_MANIFEST"
	layouts := map[string]func(t *testing.T) localScenario{
		"root relative": func(t *testing.T) localScenario { return newRootLocalScenario(t, "./rootpkg", "local") },
		"nested absolute": func(t *testing.T) localScenario {
			return newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
		},
	}
	sources := map[string]func(t *testing.T, s localScenario){
		"missing path": func(*testing.T, localScenario) {},
		"no apm.yml": func(t *testing.T, s localScenario) {
			localManifestMkdir(t, s.source)
		},
		"malformed manifest": func(t *testing.T, s localScenario) {
			writeLocalManifest(t, s.source, "name: broken\ndependencies: [\n  "+secret+"\n")
		},
		"source is a file": func(t *testing.T, s localScenario) {
			writeFile(t, s.source, "not a directory\n")
		},
		"manifest is a directory": func(t *testing.T, s localScenario) {
			localManifestMkdir(t, filepath.Join(s.source, "apm.yml"))
		},
	}
	for layoutName, build := range layouts {
		for sourceName, prepare := range sources {
			t.Run(layoutName+" "+sourceName, func(t *testing.T) {
				s := build(t)
				prepare(t, s)
				requireLocalManifestFailsClosed(t, s.workspace, true, s.name)
				_, rows := localManifestEvidence(t, s.workspace)
				for _, row := range rows {
					if strings.Contains(strings.Join(row.Issues, "\n"), secret) {
						t.Fatalf("manifest literal leaked into issues: %#v", row.Issues)
					}
				}
			})
		}
	}
}

func TestLocalManifestExistingBadModuleSlotDoesNotFallBackToLocalSource(t *testing.T) {
	layouts := map[string]func(t *testing.T) localScenario{
		"root relative": func(t *testing.T) localScenario { return newRootLocalScenario(t, "./rootpkg", "local") },
		"nested absolute": func(t *testing.T) localScenario {
			return newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
		},
	}
	slots := map[string]func(t *testing.T, s localScenario){
		"empty module directory": func(t *testing.T, s localScenario) {
			localManifestMkdir(t, s.module)
		},
		"malformed module manifest": func(t *testing.T, s localScenario) {
			writeLocalManifest(t, s.module, "name: broken\ndependencies: [\n")
		},
		"symlinked module manifest": func(t *testing.T, s localScenario) {
			localManifestMkdir(t, s.module)
			localManifestSymlink(t, filepath.Join(s.source, "apm.yml"), filepath.Join(s.module, "apm.yml"))
		},
		"symlinked module directory": func(t *testing.T, s localScenario) {
			localManifestSymlink(t, s.source, s.module)
		},
		"dangling module symlink": func(t *testing.T, s localScenario) {
			localManifestSymlink(t, filepath.Join(s.workspace, "missing"), s.module)
		},
		"module path is a file": func(t *testing.T, s localScenario) {
			writeFile(t, s.module, "not a module\n")
		},
		"symlinked module parent": func(t *testing.T, s localScenario) {
			parent := filepath.Dir(s.module)
			_ = os.Remove(parent)
			localManifestSymlink(t, t.TempDir(), parent)
		},
	}
	for layoutName, build := range layouts {
		for slotName, prepare := range slots {
			t.Run(layoutName+" "+slotName, func(t *testing.T) {
				s := build(t)
				writeLocalManifest(t, s.source, localServiceManifest(s.name, "source-mcp"))
				prepare(t, s)
				requireLocalManifestFailsClosed(t, s.workspace, true, s.name)
			})
		}
	}
}

func TestLocalManifestNonLocalSourceWithLocalPathCannotFallBack(t *testing.T) {
	for _, source := range []string{"", "registry", "git", "future"} {
		t.Run("root relative source "+source, func(t *testing.T) {
			s := newRootLocalScenario(t, "./rootpkg", source)
			writeLocalManifest(t, s.source, localServiceManifest(s.name, "source-mcp"))
			requireLocalManifestFailsClosed(t, s.workspace, true, s.name)
		})
		t.Run("nested absolute source "+source, func(t *testing.T) {
			s := newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", source)
			writeLocalManifest(t, s.source, localServiceManifest(s.name, "source-mcp"))
			requireLocalManifestFailsClosed(t, s.workspace, true, s.name)
		})
	}
}

func TestLocalManifestUnsupportedLockMetadataFailsClosed(t *testing.T) {
	layouts := map[string]func(t *testing.T) localScenario{
		"root relative": func(t *testing.T) localScenario { return newRootLocalScenario(t, "./rootpkg", "local") },
		"nested absolute": func(t *testing.T) localScenario {
			return newNestedLocalScenario(t, filepath.Join(t.TempDir(), "omp-skills"), "", "local")
		},
	}
	for layoutName, build := range layouts {
		for name, suffix := range map[string]string{
			"alias":                    "  alias: other\n",
			"replacement":              "  replacement:\n    path: ./elsewhere\n",
			"non-string anchored path": "  anchored_local_path: 42\n",
			"lock replacements":        "replacements:\n  _local/other: ./elsewhere\n",
		} {
			t.Run(layoutName+" "+name, func(t *testing.T) {
				s := build(t)
				writeLocalManifest(t, s.source, localServiceManifest(s.name, "source-mcp"))
				localManifestAppendLock(t, s.workspace, suffix)
				requireLocalManifestFailsClosed(t, s.workspace, true, s.name)
			})
		}
	}
}
