package app

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAgentsParkedEmptyDeploymentAttestation(t *testing.T) {
	t.Setenv("GITHUB_HOST", "github.com")
	const parked = "dependencies:\n  - repo_url: Acme/Demo\n    exec_status: gated_pending_approval\n"
	const sibling = "  - repo_url: acme/other\n    deployed_files: [extensions/other/index.ts]\n"
	const rootRow = "deployments:\n  - kind: uri\n    target: mcp\n    value: mcp://standalone\n    scope: project\n    owners: ['.']\n    active_owner: '.'\n"
	const siblingRow = "deployments:\n  - kind: target-relative\n    target: omp\n    value: extensions/other/index.ts\n    scope: project\n    owners: [acme/other]\n    active_owner: acme/other\n"
	tests := []struct {
		name string
		lock string
		want bool
	}{
		{"gated omitted inventory", parked + "deployments: []\n", true},
		{"denied omitted inventory", strings.ReplaceAll(parked, "gated_pending_approval", "denied") + "deployments: []\n", true},
		{"explicit empty inventory", parked + "    deployed_files: []\n    deployed_file_hashes: {}\ndeployments: []\n", true},
		{"missing ledger", parked, false},
		{"null ledger", parked + "deployments: null\n", false},
		{"mapping ledger", parked + "deployments: {}\n", false},
		{"scalar ledger", parked + "deployments: unknown\n", false},
		{"unknown trust", strings.ReplaceAll(parked, "gated_pending_approval", "future_state") + "deployments: []\n", false},
		{"deployed trust", strings.ReplaceAll(parked, "gated_pending_approval", "deployed") + "deployments: []\n", false},
		{"absent trust", strings.ReplaceAll(parked, "gated_pending_approval", "absent") + "deployments: []\n", false},
		{"omitted trust", "dependencies: [{repo_url: acme/demo}]\ndeployments: []\n", false},
		{"file claim", parked + "    deployed_files: [extensions/demo/index.ts]\ndeployments: []\n", false},
		{"hash claim", parked + "    deployed_file_hashes: {extensions/demo/index.ts: 'sha256:abc'}\ndeployments: []\n", false},
		{"legacy skill claim", parked + "    deployed_skills: [demo]\ndeployments: []\n", false},
		{"null deployed files", parked + "    deployed_files: null\ndeployments: []\n", false},
		{"root standalone service", parked + rootRow, true},
		{"other deployed dependency", parked + sibling + siblingRow, true},
		{"local bundle", parked + strings.ReplaceAll(rootRow, "'.'", "local-bundle"), true},
		{"own active owner", parked + strings.ReplaceAll(rootRow, "'.'", "acme/demo"), false},
		{"own inactive owner", parked + strings.Replace(rootRow, "owners: ['.']", "owners: [acme/demo, '.']", 1), false},
		{"unknown inactive owner", parked + strings.Replace(rootRow, "owners: ['.']", "owners: [unknown/package, '.']", 1), false},
		{"unknown active owner", parked + strings.Replace(rootRow, "active_owner: '.'", "active_owner: unknown/package", 1), false},
		{"known active owner absent from owners", parked + sibling + strings.Replace(rootRow, "active_owner: '.'", "active_owner: acme/other", 1), false},
		{"empty owners", parked + strings.Replace(rootRow, "owners: ['.']", "owners: []", 1), false},
		{"null row", parked + "deployments: [null]\n", false},
		{"null row beside valid row", parked + strings.Replace(rootRow, "deployments:\n", "deployments:\n  - null\n", 1), false},
		{"aliased null row", parked + "empty: &empty null\ndeployments: [*empty]\n", false},
		{"aliased empty ledger", parked + "ledger: &ledger []\ndeployments: *ledger\n", true},
		{"missing row fields", parked + "deployments: [{}]\n", false},
		{"unknown kind", parked + strings.Replace(rootRow, "kind: uri", "kind: unknown", 1), false},
		{"missing target", parked + strings.Replace(rootRow, "    target: mcp\n", "", 1), false},
		{"empty value", parked + strings.Replace(rootRow, "value: mcp://standalone", "value: ''", 1), false},
		{"missing scope", parked + strings.Replace(rootRow, "    scope: project\n", "", 1), false},
		{"numeric target", parked + strings.Replace(rootRow, "target: mcp", "target: 42", 1), false},
		{"numeric scope", parked + strings.Replace(rootRow, "scope: project", "scope: 42", 1), false},
		{"numeric value", parked + strings.Replace(rootRow, "value: mcp://standalone", "value: 42", 1), false},
		{"numeric owner", parked + strings.Replace(rootRow, "owners: ['.']", "owners: [42, '.']", 1), false},
		{"malformed content hash", parked + rootRow + "    content_hash: 42\n", false},
		{"unknown source", parked + "    source: future\ndeployments: []\n", false},
		{"malformed sibling identity", parked + "  - repo_url: acme/other\n    source: future\n" + rootRow, false},
		{"duplicate owner identity", parked + "  - repo_url: acme/demo\ndeployments: []\n", false},
		{"numeric local identity", parked + "  - repo_url: _local/other\n    source: local\n    local_path: 42\ndeployments: []\n", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var lock apmLockfile
			err := yaml.Unmarshal([]byte(test.lock), &lock)
			if err != nil {
				if test.want {
					t.Fatal(err)
				}
				return
			}
			evidence := newAgentsPackageLockEvidence(newAgentsLockDeploymentEvidence(lock), lock.Dependencies[0])
			if evidence.EmptyDeploymentAttested != test.want {
				t.Fatalf("EmptyDeploymentAttested = %v, want %v", evidence.EmptyDeploymentAttested, test.want)
			}
			if evidence.ExecStatus != lock.Dependencies[0].ExecStatus {
				t.Fatal("evidence lost the explicit executable trust state")
			}
		})
	}
}

func TestAgentsParkedLockOwnerIdentity(t *testing.T) {
	t.Setenv("GITHUB_HOST", "github.com")
	tests := []struct {
		name string
		dep  apmLockDep
		want string
	}{
		{"implicit GitHub", apmLockDep{RepoURL: "Acme/Demo"}, "acme/demo"},
		{"explicit GitHub", apmLockDep{RepoURL: "Acme/Demo", Host: "GITHUB.COM"}, "acme/demo"},
		{"enterprise cloud", apmLockDep{RepoURL: "Acme/Demo", Host: "acme.ghe.com"}, "acme.ghe.com/acme/demo"},
		{"non GitHub casing", apmLockDep{RepoURL: "Acme/Demo", Host: "GitLab.com"}, "gitlab.com/Acme/Demo"},
		{"single label host", apmLockDep{RepoURL: "Acme/Demo", Host: "gitserver"}, "gitserver/Acme/Demo"},
		{"explicit host port", apmLockDep{RepoURL: "Acme/Demo", Host: "git.example.com:8443"}, "git.example.com:8443/Acme/Demo"},
		{"lock repo suffix preserved", apmLockDep{RepoURL: "Acme/Demo.git"}, "acme/demo.git"},
		{"registry", apmLockDep{RepoURL: "Acme/Demo", Source: "registry"}, "acme/demo"},
		{"registry mirror", apmLockDep{RepoURL: "Acme/Demo", Host: "mirror.example.com", RegistryPrefix: "artifactory/github"}, "acme/demo"},
		{"virtual path casing", apmLockDep{RepoURL: "Acme/Demo", IsVirtual: true, VirtualPath: "Packages/Hook"}, "acme/demo/Packages/Hook"},
		{"local", apmLockDep{RepoURL: "_local/demo", Source: "local", LocalPath: "./Packages/Demo"}, "./Packages/Demo"},
		{"anchored local", apmLockDep{RepoURL: "_local/demo", Source: "local", LocalPath: "../Demo", AnchoredLocalPath: "Packages/Demo"}, "local:Packages/Demo"},
		{"unknown source", apmLockDep{RepoURL: "Acme/Demo", Source: "future"}, ""},
		{"missing repo", apmLockDep{}, ""},
		{"URL instead of lock repo", apmLockDep{RepoURL: "https://github.com/acme/demo"}, ""},
		{"malformed host", apmLockDep{RepoURL: "acme/demo", Host: "https://github.com"}, ""},
		{"unmarked local path", apmLockDep{RepoURL: "acme/demo", LocalPath: "./demo"}, ""},
		{"unmarked virtual path ignored", apmLockDep{RepoURL: "acme/demo", VirtualPath: "hook"}, "acme/demo"},
		{"empty virtual path ignored", apmLockDep{RepoURL: "acme/demo", IsVirtual: true}, "acme/demo"},
		{"missing local path", apmLockDep{RepoURL: "_local/demo", Source: "local"}, ""},
		{"root owner collision", apmLockDep{RepoURL: "_local/demo", Source: "local", LocalPath: "."}, ""},
		{"invalid segment", apmLockDep{RepoURL: "acme/../demo"}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := agentsLockDependencyOwnerKey(test.dep)
			if got != test.want || ok != (test.want != "") {
				t.Fatalf("owner = %q, valid = %v; want %q", got, ok, test.want)
			}
		})
	}
}

func TestAgentsParkedLockOwnerConfiguredHost(t *testing.T) {
	tests := []struct {
		name       string
		githubHost string
		adoHost    string
		adoHosts   string
		host       string
		want       string
	}{
		{"custom GHES", "git.example.com", "", "", "git.example.com", "git.example.com/acme/demo"},
		{"implicit custom GHES", "git.example.com", "", "", "", "acme/demo"},
		{"GitLab default preserves case", "gitlab.com", "", "", "", "Acme/Demo"},
		{"Azure cloud preserves case", "dev.azure.com", "", "", "dev.azure.com", "dev.azure.com/Acme/Demo"},
		{"configured Azure preserves case", "git.example.com", "https://git.example.com/", "", "git.example.com", "git.example.com/Acme/Demo"},
		{"configured Azure list preserves case", "git.example.com", "", "other.example.com, https://git.example.com/", "git.example.com", "git.example.com/Acme/Demo"},
		{"Azure URL with port is not host config", "git.example.com", "https://git.example.com:443/", "", "git.example.com", "git.example.com/acme/demo"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GITHUB_HOST", test.githubHost)
			t.Setenv("ADO_HOST", test.adoHost)
			t.Setenv("APM_ADO_HOSTS", test.adoHosts)
			got, ok := agentsLockDependencyOwnerKey(apmLockDep{RepoURL: "Acme/Demo", Host: test.host})
			if !ok || got != test.want {
				t.Fatalf("owner = %q, valid = %v; want %q", got, ok, test.want)
			}
		})
	}
}

func TestAgentsParkedDeploymentEvidenceBounds(t *testing.T) {
	t.Setenv("GITHUB_HOST", "github.com")
	dep := apmLockDep{RepoURL: "acme/demo", ExecStatus: "denied"}
	for _, test := range []struct {
		name string
		lock apmLockfile
	}{
		{"dependencies", apmLockfile{Dependencies: make([]apmLockDep, maxBundleEntries+1), Deployments: []apmLockDeployment{}}},
		{"rows", apmLockfile{Dependencies: []apmLockDep{dep}, Deployments: make([]apmLockDeployment, maxBundleEntries+1)}},
		{"claims", apmLockfile{Dependencies: []apmLockDep{dep}, Deployments: []apmLockDeployment{{Kind: "uri", Target: "mcp", Value: "mcp://service", Scope: "project", Owners: make([]string, maxBundleEntries+1), ActiveOwner: "."}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := newAgentsLockDeploymentEvidence(test.lock); got != nil {
				t.Fatal("oversized ledger produced ownership evidence")
			}
		})
	}
}
