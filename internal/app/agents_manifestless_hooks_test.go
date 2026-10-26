package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestManifestlessHookDeclarationShapes(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want bool
	}{
		{"wrapped nested", `{"hooks":{"CustomFutureEvent":[{"matcher":"*","hooks":[{"type":"command","command":"echo ok"}]}]}}`, true},
		{"naked flat", `{"BeforeAnything":[{"bash":"echo ok","powershell":""}]}`, true},
		{"platform command", `{"hooks":{"Future":[{"linux":"echo ok","osx":"echo mac"}]}}`, true},
		{"prompt", `{"hooks":{"Future":[{"type":"prompt","prompt":"Review this"}]}}`, true},
		{"agent", `{"Future":[{"type":"agent","prompt":"Review this"}]}`, true},
		{"empty unrelated event", `{"Empty":[],"Future":[{"command":"echo ok"}]}`, true},
		{"empty", `{}`, false},
		{"no intent", `{"Future":[{"type":"command","command":"  "}]}`, false},
		{"untyped prompt", `{"Future":[{"prompt":"Review this"}]}`, false},
		{"empty event name", `{" ":[{"command":"echo ok"}]}`, false},
		{"invalid optional command", `{"Future":[{"command":"echo ok","linux":null}]}`, false},
		{"invalid prompt", `{"Future":[{"command":"echo ok","prompt":12}]}`, false},
		{"invalid type", `{"Future":[{"command":"echo ok","type":[]}]}`, false},
		{"unsupported type", `{"Future":[{"command":"echo ok","type":"unknown"}]}`, false},
		{"empty explicit type", `{"Future":[{"command":"echo ok","type":""}]}`, false},
		{"invalid matcher", `{"Future":[{"matcher":false,"hooks":[{"command":"echo ok"}]}]}`, false},
		{"invalid nested", `{"Future":[{"command":"echo ok","hooks":null}]}`, false},
		{"invalid entry", `{"Future":[{"command":"echo ok"},false]}`, false},
		{"naked metadata", `{"Future":[{"command":"echo ok"}],"version":1}`, false},
		{"wrapped metadata", `{"hooks":{"Future":[{"command":"echo ok"}]},"version":1}`, true},
		{"invalid JSON", `{"hooks":`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := manifestlessHookDeclaration([]byte(test.data)); got != test.want {
				t.Fatalf("manifestlessHookDeclaration() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestManifestlessForeignHookPayloadRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	root := filepath.Join(home, ".apm", "apm_modules")
	module := filepath.Join(root, "owner", "pkg")
	for _, deployed := range []string{
		".claude/hooks/pkg/run.sh", ".cursor/hooks/pkg/lib/run.js",
		".codex/hooks/pkg/run.sh", ".gemini/hooks/pkg/run.sh",
		".codeium/windsurf/hooks/pkg/run.sh", ".kiro/hooks/pkg/run.sh",
		".copilot/hooks/scripts/pkg/run.sh",
	} {
		if !manifestlessForeignHookPath(deployed, root, module) {
			t.Errorf("rejected package payload %q", deployed)
		}
	}
	for _, deployed := range []string{
		".claude/hooks/other/run.sh", ".claude/hooks/pkg", ".claude/hooks/pkg/",
		".claude/hooks/pkg/../other/run.sh", ".claude/hooks/pkg//run.sh",
		".claude/hooks/pkg/./run.sh", ".claude/hooks/pkg/run.sh ",
		".copilot/hooks/pkg-arbitrary.json", ".kiro/hooks/pkg-arbitrary-1.json",
		".github/hooks/scripts/pkg/run.sh", ".windsurf/hooks/pkg/run.sh",
	} {
		if manifestlessForeignHookPath(deployed, root, module) {
			t.Errorf("accepted unowned payload %q", deployed)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", " ~/custom-claude ")
	if !manifestlessForeignHookPath("custom-claude/hooks/pkg/run.sh", root, module) || manifestlessForeignHookPath(".claude/hooks/pkg/run.sh", root, module) {
		t.Fatal("Claude home override did not replace the default root")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "relative-claude")
	if !manifestlessForeignHookPath(".apm/relative-claude/hooks/pkg/run.sh", root, module) {
		t.Fatal("Claude relative override was not resolved against the APM workspace")
	}
}

func TestManifestlessGeneratedHookPaths(t *testing.T) {
	for _, test := range []struct {
		name    string
		sources map[string]string
		want    []string
	}{
		{
			name: "mapped events and prompt counts",
			sources: map[string]string{
				"hooks/lifecycle.json": `{"hooks":{"Stop":[{"hooks":[{"command":"echo one"},{"type":"agent","prompt":"review"}]}],"agentStop":[{"bash":"echo two"}],"Future":[{"linux":"echo platform"}]}}`,
			},
			want: []string{".copilot/hooks/Pkg-lifecycle.json", ".kiro/hooks/pkg-lifecycle-stop-1.json", ".kiro/hooks/pkg-lifecycle-stop-2.json", ".kiro/hooks/pkg-lifecycle-stop-3.json"},
		},
		{
			name: "specific routing suppresses universal per target",
			sources: map[string]string{
				"hooks/universal.json":  `{"Stop":[{"command":"echo universal"}]}`,
				"hooks/hooks-kiro.json": `{"SessionStart":[{"command":"echo kiro"}]}`,
			},
			want: []string{".copilot/hooks/Pkg-universal.json", ".kiro/hooks/pkg-hooks-kiro-sessionstart-1.json"},
		},
		{
			name: "converted source takes precedence",
			sources: map[string]string{
				".apm/hooks/lifecycle.json": `{"Stop":[{"command":"echo converted"}]}`,
				"hooks/lifecycle.json":      `{"SessionStart":[{"command":"echo original"}]}`,
			},
			want: []string{".copilot/hooks/Pkg-lifecycle.json", ".kiro/hooks/pkg-lifecycle-stop-1.json"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			module := filepath.Join(root, "Pkg")
			for rel, data := range test.sources {
				filename := filepath.Join(module, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := manifestlessGeneratedHookPaths(root, module)
			if !ok || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("manifestlessGeneratedHookPaths() = %v, %v; want %v, true", got, ok, test.want)
			}
		})
	}
}
