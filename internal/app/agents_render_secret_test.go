package app

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHasLiteralAgentSecret(t *testing.T) {
	cases := map[string]bool{
		"${env:TOKEN}":         false,
		"Bearer ${env:TOKEN}":  false,
		"hunter2":              true,
		"Bearer abc${env:X}":   true,
		"sk-${partial":         true,
		"${env:A}-${env:B}":    true,
		"Token ${env:API_KEY}": false,
	}
	for value, want := range cases {
		if got := hasLiteralAgentSecret(map[string]string{"k": value}); got != want {
			t.Errorf("hasLiteralAgentSecret(%q) = %v, want %v", value, got, want)
		}
	}
	if !hasLiteralAgentSecret(map[string]any{"nested": map[string]any{"k": "plain"}}) {
		t.Error("want a nested literal reported")
	}
}

func TestAdoptedSourceHasCredentials(t *testing.T) {
	cases := map[string]bool{
		"owner/repo":                     false,
		"git@github.com:owner/repo.git":  false,
		"https://github.com/owner/repo":  false,
		"https://u:SEC/RET@h/x":          true,
		"https://u:S%zz@h":               true,
		"oauth2:SECRET@gitlab.com/o/r":   true,
		"token@github.com/o/r":           true,
		"ssh://git@github.com/owner/r":   false,
		"acme/scalar@v3":                 false,
		"u:SEC/RET@h/x":                  true,
		"https://x-access-token:t@h/o/r": true,
	}
	for source, want := range cases {
		if got := parseAgentPackageSpec(source, true).credentials; got != want {
			t.Errorf("credentials(%q) = %v, want %v", source, got, want)
		}
	}
}

func TestArgsAndURLMayCarrySecret(t *testing.T) {
	if !argsMayCarrySecret([]string{"--api-key", "abc"}) || !argsMayCarrySecret([]string{"--token=abc"}) {
		t.Error("want literal credential flags flagged")
	}
	if argsMayCarrySecret([]string{"--api-key", "${env:KEY}"}) || argsMayCarrySecret([]string{"--port", "8080"}) {
		t.Error("want references and unrelated flags left alone")
	}
	if !urlMayCarrySecret("https://h/mcp?api_key=x") || urlMayCarrySecret("https://h/mcp/") || urlMayCarrySecret("https://h/${env:PATH_KEY}") {
		t.Error("urlMayCarrySecret misclassified a URL")
	}
}

func TestLiteralAgentSecretInListsAndEmptyValues(t *testing.T) {
	if !hasLiteralAgentSecret(map[string]any{"auth": []any{"sk-literal"}}) {
		t.Error("want a literal inside a list reported")
	}
	if hasLiteralAgentSecret(map[string]string{"EMPTY": ""}) {
		t.Error("want an empty value left alone")
	}
}

func TestParseAdoptableManifestEdgeCases(t *testing.T) {
	entries, err := parseAdoptableManifest([]byte("dependencies:\n  mcp:\n    - name: s\n      registry: false\n      transport: stdio\n      command: x\n      env: {B: 010}\n      extra: {env: {X: 1}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	server := entries.mcp[0]
	if server.Env["B"] != "010" {
		t.Fatalf("env = %#v, want 010 kept as written", server.Env)
	}
	for _, value := range []string{"~", "!!binary aGk="} {
		if _, err := parseAdoptableManifest([]byte("dependencies:\n  mcp:\n    - name: s\n      registry: false\n      transport: stdio\n      command: x\n      env: {A: " + value + "}\n")); err == nil {
			t.Errorf("env value %s: want adoption refused rather than changed", value)
		}
	}
	if got := agentsManifestDiff([]byte("dependencies:\n  lsp: oops\n"), []byte("dependencies: {}\n")); strings.Contains(got, "only formatting") {
		t.Errorf("non-list dependencies diff = %q, want the change shown", got)
	}
	if nested, _ := server.Extra["env"].(map[string]any); nested["X"] != int64(1) {
		t.Fatalf("extra = %#v, want nested values keep their YAML type", server.Extra)
	}
	for name, manifest := range map[string]string{
		"client key":    "dependencies:\n  mcp:\n    - name: s\n      registry: false\n      transport: stdio\n      command: x\n      client: {codex: {t: 1}}\n",
		"merge key":     "base: &b {registry: false, transport: stdio, command: x}\ndependencies:\n  mcp:\n    - name: s\n      <<: *b\n",
		"two documents": "dependencies:\n  apm: [a/b]\n---\ndependencies:\n  apm: [c/d]\n",
	} {
		if _, err := parseAdoptableManifest([]byte(manifest)); err == nil {
			t.Errorf("%s: want adoption refused", name)
		}
	}
}

func TestParseAgentPackageSpec(t *testing.T) {
	cases := []struct {
		spec, source, ref string
		credentials       bool
	}{
		{"owner/repo", "owner/repo", "", false},
		{"owner/repo@v1", "owner/repo", "v1", false},
		{"owner/repo@release/1.0", "owner/repo", "release/1.0", false},
		{"https://github.com/o/r@v1", "https://github.com/o/r", "v1", false},
		{"ssh://git@github.com/o/r@v1", "ssh://git@github.com/o/r", "v1", false},
		{"git@github.com:o/r.git", "git@github.com:o/r.git", "", false},
		{"git@github.com:o/r@v2", "git@github.com:o/r", "v2", false},
		{"https://x-access-token:t@github.com/o/r", "", "", true},
		{"oauth2:glpat-SECRET@gitlab.com:repo", "", "", true},
		{"ghp_SECRET@github.com", "", "", true},
		{"https://gitlab.com/org/repo?private_token=glpat", "", "", true},
		{"user:pass@host/o/r?r=https://x", "", "", true},
		{"TOKEN@host/https://x", "", "", true},
		{"https//u:pass@h/o/r", "", "", true},
		{"https://TOK/EN@github.com/o/r", "", "", true},
		{"https://u:1234/5678@h.example/x", "", "", true},
		{"TOK/EN@github.com/o/r", "", "", true},
		{"https://github.com/o/r@v1.2", "https://github.com/o/r", "v1.2", false},
	}
	for _, tc := range cases {
		got := parseAgentPackageSpec(tc.spec, true)
		if got.credentials != tc.credentials || (!tc.credentials && (got.source != tc.source || got.ref != tc.ref)) {
			t.Errorf("parseAgentPackageSpec(%q) = %+v, want source %q ref %q credentials %v", tc.spec, got, tc.source, tc.ref, tc.credentials)
		}
		for _, secret := range []string{"SECRET", "glpat", "x-access-token", "pass", "TOK", "1234"} {
			if strings.Contains(got.display, secret) {
				t.Errorf("display of %q leaks %q: %q", tc.spec, secret, got.display)
			}
		}
	}
}

func TestAdoptionRefusesAliasesKeepsTimestampsAndFlagsSecrets(t *testing.T) {
	if _, err := parseAdoptableManifest([]byte("dependencies:\n  mcp:\n    - &x\n      name: s\n      registry: false\n      transport: stdio\n      command: x\n      extra: {self: *x}\n")); err == nil {
		t.Error("want a recursive alias refused")
	}
	entries, err := parseAdoptableManifest([]byte("dependencies:\n  mcp:\n    - name: s\n      registry: false\n      transport: stdio\n      command: x\n      extra: {since: 2024-01-01}\n"))
	if err != nil || entries.mcp[0].Extra["since"] != "2024-01-01" {
		t.Fatalf("entries = %#v, %v; want the timestamp kept as written", entries, err)
	}
	if _, err := parseAdoptableManifest([]byte("dependencies:\n  mcp:\n    - name: s\n      registry: false\n      transport: stdio\n      Command: x\n")); err == nil {
		t.Error("want a differently cased key refused")
	}
	if !argsMayCarrySecret([]string{"-H", "Authorization: Bearer abc"}) || !argsMayCarrySecret([]string{"run", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN=ghp_x"}) {
		t.Error("want header and NAME=value secrets flagged")
	}
	if !urlMayCarrySecret("https://${env:HOST}/mcp?key=literal") {
		t.Error("want a literal query flagged next to a reference")
	}
	a, _ := manifestSummary([]byte("dependencies:\n  apm:\n    - path: './My Skills/a'\n"))
	b, _ := manifestSummary([]byte("dependencies:\n  apm:\n    - path: './My Skills/b'\n"))
	if string(a) == string(b) {
		t.Error("want entries that differ only in a collapsed identity to summarise differently")
	}
}

func TestInitializationMayCarrySecret(t *testing.T) {
	if !initializationMayCarrySecret(map[string]any{"settings": map[string]any{"apiKey": "sk-1"}}) {
		t.Error("want a nested literal api key flagged")
	}
	if initializationMayCarrySecret(map[string]any{"apiKey": "${env:KEY}", "limit": int64(5)}) {
		t.Error("want references and unrelated keys left alone")
	}
}

func TestYAMLSafeNumbersRender(t *testing.T) {
	out, err := yaml.Marshal(yamlSafeNumbers(map[string]any{"a": int64(1000000), "b": float64(1e-7), "c": float64(1e20), "d": 2.5}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a: 1000000\n", "b: 0.0000001\n", "c: 100000000000000000000.0\n", "d: 2.5\n"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rendered %q, want %q", out, want)
		}
	}
	if parseAgentPackageSpec("https://github.com/o/r@main?private_token=SEC", true).credentials != true {
		t.Error("want a query after the ref refused")
	}
	if parseAgentPackageSpec("http://[::1]:8080/mcp", false).credentials {
		t.Error("want an IPv6 literal host with a port accepted")
	}
}
