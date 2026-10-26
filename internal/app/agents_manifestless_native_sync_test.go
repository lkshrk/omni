package app

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lkshrk/omni/internal/executor"
)

const manifestlessNativeServices = `  mcp:
  - name: standalone-mcp
    registry: false
    transport: http
    url: https://example.invalid/mcp
  lsp:
  - name: standalone-lsp
    command: sh
    extensionToLanguage:
      .go: go
`

const manifestlessNativeServiceLock = `mcp_servers:
- standalone-mcp
mcp_configs:
  standalone-mcp:
    name: standalone-mcp
    transport: http
    url: https://example.invalid/mcp
lsp_servers:
- standalone-lsp
lsp_configs:
  standalone-lsp:
    name: standalone-lsp
    command: sh
    extensionToLanguage:
      .go: go
`

const manifestlessNativePackageJSON = `{"name":"demo","omp":{"extensions":["index.ts"]}}`
const manifestlessNativeEntry = `export default function (api) { api.on("session_start", async () => { console.log("demo session started"); }); }
`
const manifestlessNativeHooks = `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"printf 'session started'"}]}]}}`

func setupManifestlessNativeSync(t *testing.T, packageType string) (*App, *executor.MockExecutor, string, string, string) {
	t.Helper()
	return setupManifestlessNativeSyncState(t, packageType, "")
}

func setupManifestlessNativeSyncState(t *testing.T, packageType, execStatus string) (*App, *executor.MockExecutor, string, string, string) {
	t.Helper()
	a, mock, home := setupOwnedSync(t, ownedSyncManifest(manifestlessNativeServices, "acme/demo"))
	dir := filepath.Join(home, ".apm")
	module := filepath.Join(dir, "apm_modules", "acme", "demo")
	deployed := []string{}
	if packageType == "hook_package" {
		writeFile(t, filepath.Join(module, "hooks", "hooks.json"), manifestlessNativeHooks)
		// Shared merged hooks.json/apm-hooks.json are not package-owned inventory.
		deployed = append(deployed, ".omp/agent/extensions/apm-hooks-user/index.ts", ".omp/agent/apm-hooks-runtime.json")
	} else {
		writeFile(t, filepath.Join(module, "package.json"), manifestlessNativePackageJSON)
		writeFile(t, filepath.Join(module, "index.ts"), manifestlessNativeEntry)
		bundle := fmt.Sprintf(".omp/agent/extensions/package-%x", sha256.Sum256([]byte("acme/demo")))
		deployed = append(deployed, bundle+"/index.ts", bundle+"/package.json", bundle+"/.apm-extension.json")
	}
	switch packageType {
	case "skill_bundle":
		writeSkillManifest(t, module, "skills/demo/SKILL.md")
		deployed = append(deployed, ".agents/skills/demo/SKILL.md")
	case "claude_skill":
		writeSkillManifest(t, module, "SKILL.md")
		deployed = append(deployed, ".agents/skills/demo/SKILL.md")
	}
	// Ownership preflight consumes the installed source and lock receipt. Deployed
	// artifact content integrity is checked by APM, not by this sync boundary.
	lock := "dependencies:\n- repo_url: acme/demo\n  name: demo\n  version: unknown\n  package_type: " + packageType + "\n  resolved_commit: deadbeef\n"
	if execStatus != "" {
		lock += "  exec_status: " + execStatus + "\ndeployments: []\n"
	} else {
		lock += "  deployed_files:\n"
		for _, path := range deployed {
			lock += "  - " + path + "\n"
		}
	}
	lock += manifestlessNativeServiceLock
	writeFile(t, filepath.Join(dir, "apm.lock.yaml"), lock)
	return a, mock, dir, module, lock
}

func TestAgentsSyncExistingLockAllowsManifestlessNativePackagesWithStandaloneMCPAndLSP(t *testing.T) {
	for _, packageType := range []string{"extension_package", "hook_package", "skill_bundle", "claude_skill"} {
		t.Run(packageType, func(t *testing.T) {
			a, mock, dir, _, _ := setupManifestlessNativeSync(t, packageType)
			mock.Responses = []executor.MockCall{
				{Stdout: "APM CLI version " + apmVersionPin + "\n"},
				{Stdout: "installed\n"},
			}
			result, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{})
			if err != nil || result.Output != "installed\n" {
				t.Fatalf("existing-lock sync result = %#v, err = %v", result, err)
			}
			if len(result.Notices) != 0 {
				t.Fatalf("existing package reported unavailable: %q", result.Notices)
			}
			if len(mock.Calls) != 2 || len(mock.Calls[1].Args) == 0 || mock.Calls[1].Args[0] != "install" {
				t.Fatalf("sync calls = %#v", mock.Calls)
			}
			if _, err := os.Stat(filepath.Join(dir, "apm.yml")); err != nil {
				t.Fatalf("live manifest was not materialized: %v", err)
			}
			status, err := a.AgentsStatus()
			if err != nil {
				t.Fatal(err)
			}
			row := ownedPackageRow(t, status.Packages, "demo")
			if row.Status != AgentsPackageInstalled || len(row.Provides) != 0 || strings.Contains(strings.Join(row.Issues, "\n"), "ownership evidence unavailable") {
				t.Fatalf("native package status = %#v", row)
			}
			if !hasAgentsService(status.MCP, "standalone-mcp") || !hasAgentsService(status.LSP, "standalone-lsp") {
				t.Fatalf("independent services disappeared: MCP=%#v LSP=%#v", status.MCP, status.LSP)
			}
		})
	}
}

func TestAgentsSyncRevalidatesManifestlessNativeEvidenceBeforeMutation(t *testing.T) {
	for _, packageType := range []string{"extension_package", "hook_package", "skill_bundle", "claude_skill"} {
		t.Run(packageType, func(t *testing.T) {
			mutations := map[string]func(*testing.T, string, string, string){
				"added MCP carrier": func(t *testing.T, _, module, _ string) {
					writeFile(t, filepath.Join(module, "mcp.json"), `{"mcpServers":{"new":{"command":"sh"}}}`)
				},
				"added LSP carrier": func(t *testing.T, _, module, _ string) {
					writeFile(t, filepath.Join(module, "lsp.json"), `{"lspServers":{"new":{"command":"sh"}}}`)
				},
				"lock package type": func(t *testing.T, dir, _, lock string) {
					writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, "package_type: "+packageType, "package_type: apm_package", 1))
				},
				"lock commit": func(t *testing.T, dir, _, lock string) {
					writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, "resolved_commit: deadbeef", "resolved_commit: cafebabe", 1))
				},
				"lock deployed paths": func(t *testing.T, dir, _, lock string) {
					writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, ".omp/agent/extensions/", ".omp/agent/extensions/other/", 1))
				},
			}
			if packageType == "hook_package" {
				mutations["hook declaration"] = func(t *testing.T, _, module, _ string) {
					writeFile(t, filepath.Join(module, "hooks", "hooks.json"), strings.Replace(manifestlessNativeHooks, "session started", "session changed", 1))
				}
			} else {
				mutations["package.json"] = func(t *testing.T, _, module, _ string) {
					writeFile(t, filepath.Join(module, "package.json"), strings.Replace(manifestlessNativePackageJSON, `"name":"demo"`, `"name":"changed"`, 1))
				}
				mutations["index.ts"] = func(t *testing.T, _, module, _ string) {
					writeFile(t, filepath.Join(module, "index.ts"), strings.Replace(manifestlessNativeEntry, "session started", "session changed", 1))
				}
			}
			for name, mutate := range mutations {
				t.Run(name, func(t *testing.T) {
					a, mock, dir, module, lock := setupManifestlessNativeSync(t, packageType)
					candidatePath, candidate, err := agentsSyncCandidate(dir)
					if err != nil {
						t.Fatal(err)
					}
					_, verify, _, err := checkAgentsOwnershipPreflight(dir, a.StateDir, candidatePath, candidate)
					if err != nil {
						t.Fatal(err)
					}
					if err := verify(); err != nil {
						t.Fatalf("unchanged evidence rejected: %v", err)
					}
					mutate(t, dir, module, lock)
					if err := verify(); err == nil || err.Error() != "APM package evidence changed during ownership preflight" {
						t.Fatalf("revalidation error = %v", err)
					}
					if _, err := os.Stat(filepath.Join(dir, "apm.yml")); !os.IsNotExist(err) {
						t.Fatalf("live manifest was materialized: %v", err)
					}
					if len(mock.Calls) != 0 {
						t.Fatalf("executor was invoked: %#v", mock.Calls)
					}
				})
			}
		})
	}
}

func TestAgentsSyncExistingLockAllowsParkedNativePackagesWithStandaloneMCPAndLSP(t *testing.T) {
	for _, packageType := range []string{"extension_package", "hook_package"} {
		for _, execStatus := range []string{"gated_pending_approval", "denied"} {
			t.Run(packageType+"/"+execStatus, func(t *testing.T) {
				a, mock, _, _, _ := setupManifestlessNativeSyncState(t, packageType, execStatus)
				mock.Responses = []executor.MockCall{
					{Stdout: "APM CLI version " + apmVersionPin + "\n"},
					{Stdout: "installed\n"},
				}
				wantOutput, wantCalls := "installed\n", 2
				if execStatus == "gated_pending_approval" {
					// APM parks native packages until approved; sync grants that and installs again to deploy them.
					mock.Responses = append(mock.Responses, executor.MockCall{Stdout: "approved\n"}, executor.MockCall{Stdout: "reinstalled\n"})
					wantOutput, wantCalls = "installed\napproved\nreinstalled\n", 4
				}
				result, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{})
				if err != nil || result.Output != wantOutput || len(result.Notices) != 0 {
					t.Fatalf("parked sync result = %#v, err = %v", result, err)
				}
				if len(mock.Calls) != wantCalls || len(mock.Calls[1].Args) == 0 || mock.Calls[1].Args[0] != "install" {
					t.Fatalf("sync calls = %#v", mock.Calls)
				}
				if wantCalls == 4 && (!slices.Equal(mock.Calls[2].Args, []string{"approve", "--all", "--user"}) || !slices.Equal(mock.Calls[3].Args, mock.Calls[1].Args)) {
					t.Fatalf("approval calls = %#v", mock.Calls[2:])
				}
				status, err := a.AgentsStatus()
				if err != nil {
					t.Fatal(err)
				}
				row := ownedPackageRow(t, status.Packages, "demo")
				if row.Status != AgentsPackageInstalled || row.DeployedFiles != 0 || len(row.Provides) != 0 || strings.Contains(strings.Join(row.Issues, "\n"), "ownership evidence unavailable") {
					t.Fatalf("parked package must not claim native deployment: %#v", row)
				}
				if !hasAgentsService(status.MCP, "standalone-mcp") || !hasAgentsService(status.LSP, "standalone-lsp") {
					t.Fatalf("independent services disappeared: MCP=%#v LSP=%#v", status.MCP, status.LSP)
				}
			})
		}
	}
}

func TestAgentsSyncRevalidatesParkedNativeEvidenceBeforeMutation(t *testing.T) {
	for _, packageType := range []string{"extension_package", "hook_package"} {
		for _, execStatus := range []string{"gated_pending_approval", "denied"} {
			t.Run(packageType+"/"+execStatus, func(t *testing.T) {
				mutations := map[string]func(*testing.T, string, string, string){
					"added MCP carrier": func(t *testing.T, _, module, _ string) {
						writeFile(t, filepath.Join(module, "mcp.json"), `{"mcpServers":{"new":{"command":"sh"}}}`)
					},
					"added LSP carrier": func(t *testing.T, _, module, _ string) {
						writeFile(t, filepath.Join(module, "lsp.json"), `{"lspServers":{"new":{"command":"sh"}}}`)
					},
					"execution status": func(t *testing.T, dir, _, lock string) {
						otherStatus := "denied"
						if execStatus == otherStatus {
							otherStatus = "gated_pending_approval"
						}
						writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, "exec_status: "+execStatus, "exec_status: "+otherStatus, 1))
					},
					"removed deployment ledger": func(t *testing.T, dir, _, lock string) {
						writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, "deployments: []\n", "", 1))
					},
					"null deployment ledger": func(t *testing.T, dir, _, lock string) {
						writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, "deployments: []", "deployments: null", 1))
					},
					"active deployment owner": func(t *testing.T, dir, _, lock string) {
						writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, "deployments: []\n", parkedNativeOwnedLedger("acme/demo"), 1))
					},
					"inactive deployment owner": func(t *testing.T, dir, _, lock string) {
						writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, "deployments: []\n", parkedNativeOwnedLedger("'.'"), 1))
					},
				}
				if packageType == "hook_package" {
					mutations["hook declaration"] = func(t *testing.T, _, module, _ string) {
						writeFile(t, filepath.Join(module, "hooks", "hooks.json"), strings.Replace(manifestlessNativeHooks, "session started", "session changed", 1))
					}
				} else {
					mutations["package.json"] = func(t *testing.T, _, module, _ string) {
						writeFile(t, filepath.Join(module, "package.json"), strings.Replace(manifestlessNativePackageJSON, `"name":"demo"`, `"name":"changed"`, 1))
					}
					mutations["index.ts"] = func(t *testing.T, _, module, _ string) {
						writeFile(t, filepath.Join(module, "index.ts"), strings.Replace(manifestlessNativeEntry, "session started", "session changed", 1))
					}
				}
				for name, mutate := range mutations {
					t.Run(name, func(t *testing.T) {
						a, mock, dir, module, lock := setupManifestlessNativeSyncState(t, packageType, execStatus)
						candidatePath, candidate, err := agentsSyncCandidate(dir)
						if err != nil {
							t.Fatal(err)
						}
						_, verify, _, err := checkAgentsOwnershipPreflight(dir, a.StateDir, candidatePath, candidate)
						if err != nil {
							t.Fatal(err)
						}
						if err := verify(); err != nil {
							t.Fatalf("unchanged parked evidence rejected: %v", err)
						}
						mutate(t, dir, module, lock)
						if err := verify(); err == nil || err.Error() != "APM package evidence changed during ownership preflight" {
							t.Fatalf("parked revalidation error = %v", err)
						}
						if _, err := os.Stat(filepath.Join(dir, "apm.yml")); !os.IsNotExist(err) {
							t.Fatalf("live manifest was materialized: %v", err)
						}
						if len(mock.Calls) != 0 {
							t.Fatalf("executor was invoked: %#v", mock.Calls)
						}
					})
				}
			})
		}
	}
}

func parkedNativeOwnedLedger(activeOwner string) string {
	return "deployments:\n- kind: target-relative\n  target: omp\n  value: extensions/demo/index.ts\n  scope: project\n  owners: [acme/demo, '.']\n  active_owner: " + activeOwner + "\n"
}

func TestAgentsSyncRejectsUnprovenParkedNativePackagesBeforeMutation(t *testing.T) {
	for _, packageType := range []string{"extension_package", "hook_package"} {
		t.Run(packageType, func(t *testing.T) {
			for name, change := range map[string]func(string) string{
				"missing execution status": func(lock string) string {
					return strings.Replace(lock, "  exec_status: gated_pending_approval\n", "", 1)
				},
				"deployed execution status": func(lock string) string {
					return strings.Replace(lock, "exec_status: gated_pending_approval", "exec_status: deployed", 1)
				},
				"unknown execution status": func(lock string) string {
					return strings.Replace(lock, "exec_status: gated_pending_approval", "exec_status: future_state", 1)
				},
				"missing deployment ledger": func(lock string) string {
					return strings.Replace(lock, "deployments: []\n", "", 1)
				},
				"null deployment ledger": func(lock string) string {
					return strings.Replace(lock, "deployments: []", "deployments: null", 1)
				},
				"hash-only evidence": func(lock string) string {
					return strings.Replace(lock, "deployments: []", "  deployed_file_hashes:\n    .omp/agent/extensions/demo/index.ts: sha256:deadbeef\ndeployments: []", 1)
				},
				"active deployment owner": func(lock string) string {
					return strings.Replace(lock, "deployments: []\n", parkedNativeOwnedLedger("acme/demo"), 1)
				},
				"inactive deployment owner": func(lock string) string {
					return strings.Replace(lock, "deployments: []\n", parkedNativeOwnedLedger("'.'"), 1)
				},
			} {
				t.Run(name, func(t *testing.T) {
					a, mock, dir, _, lock := setupManifestlessNativeSyncState(t, packageType, "gated_pending_approval")
					writeFile(t, filepath.Join(dir, "apm.lock.yaml"), change(lock))
					_, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{})
					if err == nil || !strings.Contains(err.Error(), "cannot verify package-owned MCP/LSP declarations") {
						t.Fatalf("unproven parked receipt error = %v", err)
					}
					if _, err := os.Stat(filepath.Join(dir, "apm.yml")); !os.IsNotExist(err) {
						t.Fatalf("live manifest was materialized: %v", err)
					}
					if len(mock.Calls) != 0 {
						t.Fatalf("executor was invoked: %#v", mock.Calls)
					}
				})
			}
		})
	}
}

func TestAgentsSyncParkedMissingNativeEntry(t *testing.T) {
	for _, execStatus := range []string{"gated_pending_approval", "denied"} {
		t.Run(execStatus, func(t *testing.T) {
			a, mock, _, module, _ := setupManifestlessNativeSyncState(t, "extension_package", execStatus)
			writeFile(t, filepath.Join(module, "package.json"), `{"omp":{"extensions":["dist/index.js"]}}`)
			// A parked package is approved and installed again; a denied one is left alone.
			installs := 1
			if execStatus == "gated_pending_approval" {
				installs = 3
			}
			mock.Responses = []executor.MockCall{{Stdout: "APM CLI version " + apmVersionPin + "\n"}}
			for range 2 * installs {
				mock.Responses = append(mock.Responses, executor.MockCall{Stdout: "installed\n"})
			}
			for sync := range 2 {
				result, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{})
				if err != nil || result.Output != strings.Repeat("installed\n", installs) || len(result.Notices) != 0 {
					t.Fatalf("sync %d result = %#v, err = %v", sync, result, err)
				}
				status, err := a.AgentsStatus()
				if err != nil {
					t.Fatal(err)
				}
				row := ownedPackageRow(t, status.Packages, "demo")
				if row.Status != AgentsPackageInstalled || row.DeployedFiles != 0 || len(row.Provides) != 0 || strings.Contains(strings.Join(row.Issues, "\n"), "ownership evidence unavailable") {
					t.Fatalf("parked missing entry status = %#v", row)
				}
				if !hasAgentsService(status.MCP, "standalone-mcp") || !hasAgentsService(status.LSP, "standalone-lsp") {
					t.Fatalf("independent services disappeared: MCP=%#v LSP=%#v", status.MCP, status.LSP)
				}
			}
		})
	}
}

func TestAgentsSyncRejectsMissingNativeEntryWithoutParkedAttestation(t *testing.T) {
	for _, state := range []string{"installed", "deployed", "unattested"} {
		t.Run(state, func(t *testing.T) {
			execStatus := "gated_pending_approval"
			switch state {
			case "installed":
				execStatus = ""
			case "deployed":
				execStatus = "deployed"
			}
			a, mock, dir, module, lock := setupManifestlessNativeSyncState(t, "extension_package", execStatus)
			writeFile(t, filepath.Join(module, "package.json"), `{"omp":{"extensions":["dist/index.js"]}}`)
			if state == "unattested" {
				writeFile(t, filepath.Join(dir, "apm.lock.yaml"), strings.Replace(lock, "deployments: []\n", "", 1))
			}
			if _, err := a.AgentsSyncAll(t.Context(), AgentsSyncAllOptions{}); err == nil || !strings.Contains(err.Error(), "cannot verify package-owned MCP/LSP declarations") {
				t.Fatalf("missing entry rejection = %v", err)
			}
			if len(mock.Calls) != 0 {
				t.Fatalf("executor was invoked: %#v", mock.Calls)
			}
			if _, err := os.Stat(filepath.Join(dir, "apm.yml")); !os.IsNotExist(err) {
				t.Fatalf("live manifest was materialized: %v", err)
			}
		})
	}
}

func TestAgentsSyncRevalidatesParkedMissingNativeEntry(t *testing.T) {
	for _, execStatus := range []string{"gated_pending_approval", "denied"} {
		for _, mutation := range []string{"absent leaf created", "missing ancestor created", "missing ancestor replaced by symlink", "declaration changed", "service carrier added"} {
			t.Run(execStatus+"/"+mutation, func(t *testing.T) {
				a, mock, dir, module, _ := setupManifestlessNativeSyncState(t, "extension_package", execStatus)
				writeFile(t, filepath.Join(module, "package.json"), `{"omp":{"extensions":["dist/index.js"]}}`)
				if mutation == "absent leaf created" {
					if err := os.MkdirAll(filepath.Join(module, "dist"), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				candidatePath, candidate, err := agentsSyncCandidate(dir)
				if err != nil {
					t.Fatal(err)
				}
				_, verify, _, err := checkAgentsOwnershipPreflight(dir, a.StateDir, candidatePath, candidate)
				if err != nil {
					t.Fatal(err)
				}
				if err := verify(); err != nil {
					t.Fatalf("unchanged absent entry rejected: %v", err)
				}
				switch mutation {
				case "absent leaf created":
					writeFile(t, filepath.Join(module, "dist", "index.js"), manifestlessNativeEntry)
				case "missing ancestor created":
					if err := os.Mkdir(filepath.Join(module, "dist"), 0o700); err != nil {
						t.Fatal(err)
					}
				case "missing ancestor replaced by symlink":
					if err := os.Symlink(t.TempDir(), filepath.Join(module, "dist")); err != nil {
						t.Fatal(err)
					}
				case "declaration changed":
					writeFile(t, filepath.Join(module, "package.json"), `{"omp":{"extensions":["other/index.js"]}}`)
				case "service carrier added":
					writeFile(t, filepath.Join(module, "mcp.json"), `{"mcpServers":{"new":{"command":"sh"}}}`)
				}
				if err := verify(); err == nil || err.Error() != "APM package evidence changed during ownership preflight" {
					t.Fatalf("absent entry revalidation error = %v", err)
				}
				if len(mock.Calls) != 0 {
					t.Fatalf("executor was invoked: %#v", mock.Calls)
				}
				if _, err := os.Stat(filepath.Join(dir, "apm.yml")); !os.IsNotExist(err) {
					t.Fatalf("live manifest was materialized: %v", err)
				}
			})
		}
	}
}
