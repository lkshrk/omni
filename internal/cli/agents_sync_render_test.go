package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/config"
	"github.com/lkshrk/omni/internal/executor"
)

func runRenderedAgentsSync(t *testing.T, responses []executor.MockCall, args ...string) (string, string, string, error) {
	t.Helper()
	return runRenderedAgentsSyncTerminal(t, false, responses, args...)
}

func runRenderedAgentsSyncTerminal(t *testing.T, terminal bool, responses []executor.MockCall, args ...string) (string, string, string, error) {
	t.Helper()
	t.Setenv("OMNI_HOSTNAME", "testhost")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	settings := filepath.Join(home, "settings.json")
	if err := os.WriteFile(settings, fmt.Appendf(nil, `{"version":%d,
  "agents":{"packages":[{"source":"owner/declared"}],"ignored":[]},
  "groups":[{"name":"testhost","special":"host","agents":["declared"]}],
  "hosts":{"testhost":[]}}`, config.CurrentVersion), 0o600); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(home, ".apm", "apm.yml")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("name: hand\ndependencies:\n  apm:\n    - git: owner/hand-added\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	origIsTerminal := stdinIsTerminal
	stdinIsTerminal = func() bool { return terminal }
	t.Cleanup(func() { stdinIsTerminal = origIsTerminal })
	a := app.New(settings)
	a.StateDir = t.TempDir()
	a.SetFallbackExecutor(&executor.MockExecutor{Responses: responses})
	cmd := newAgentsCmd(&rootState{app: a})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"sync"}, args...))
	err := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), live, err
}

func TestAgentsSyncNonInteractiveDriftShowsDiffAndFails(t *testing.T) {
	_, stderr, live, err := runRenderedAgentsSync(t, []executor.MockCall{{Stdout: "APM CLI version " + cliTestPinnedAPMVersion + "\n"}})
	if err == nil || !strings.Contains(err.Error(), "--adopt") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want the drift refused with --adopt/--force named", err)
	}
	if !strings.Contains(stderr, "-package owner/hand-added") || !strings.Contains(stderr, "+package owner/declared") {
		t.Fatalf("stderr = %s, want the diff between the live and rendered manifest", stderr)
	}
	if got, _ := os.ReadFile(live); !strings.Contains(string(got), "owner/hand-added") {
		t.Fatalf("live = %s, want the hand edit left in place", got)
	}
}

func TestAgentsSyncForceOverwritesDrift(t *testing.T) {
	_, _, live, err := runRenderedAgentsSync(t, []executor.MockCall{
		{Stdout: "APM CLI version " + cliTestPinnedAPMVersion + "\n"},
		{Stdout: "installed\n"},
	}, "--force")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(live)
	if strings.Contains(string(got), "hand-added") || !strings.Contains(string(got), "git: owner/declared") {
		t.Fatalf("live = %s, want the rendered manifest", got)
	}
}

func TestAgentsSyncAdoptAndForceAreExclusive(t *testing.T) {
	_, _, _, err := runRenderedAgentsSync(t, nil, "--adopt", "--force")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want --adopt and --force rejected together", err)
	}
}

func TestAgentsSyncInteractiveOverwrite(t *testing.T) {
	origChoice := readDriftChoice
	readDriftChoice = func() (string, bool) { return "o", true }
	t.Cleanup(func() { readDriftChoice = origChoice })
	stdout, _, live, err := runRenderedAgentsSyncTerminal(t, true, []executor.MockCall{
		{Stdout: "APM CLI version " + cliTestPinnedAPMVersion + "\n"},
		{Stdout: "APM CLI version " + cliTestPinnedAPMVersion + "\n"},
		{Stdout: "installed\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "[a]dopt") {
		t.Fatalf("stdout = %s, want the adopt/overwrite prompt", stdout)
	}
	if got, _ := os.ReadFile(live); strings.Contains(string(got), "hand-added") {
		t.Fatalf("live = %s, want the overwrite applied", got)
	}
}

func TestAgentsSyncInteractiveCancelKeepsHandEdit(t *testing.T) {
	origChoice := readDriftChoice
	readDriftChoice = func() (string, bool) { return "c", true }
	t.Cleanup(func() { readDriftChoice = origChoice })
	_, _, live, err := runRenderedAgentsSyncTerminal(t, true, []executor.MockCall{{Stdout: "APM CLI version " + cliTestPinnedAPMVersion + "\n"}})
	if err == nil {
		t.Fatal("want cancel to keep the drift error")
	}
	if got, _ := os.ReadFile(live); !strings.Contains(string(got), "hand-added") {
		t.Fatalf("live = %s, want the hand edit kept", got)
	}
}

func TestAgentsSyncInteractiveAdopt(t *testing.T) {
	origChoice := readDriftChoice
	readDriftChoice = func() (string, bool) { return "a", true }
	t.Cleanup(func() { readDriftChoice = origChoice })
	_, stderr, live, err := runRenderedAgentsSyncTerminal(t, true, []executor.MockCall{
		{Stdout: "APM CLI version " + cliTestPinnedAPMVersion + "\n"},
		{Stdout: "APM CLI version " + cliTestPinnedAPMVersion + "\n"},
		{Stdout: "installed\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "note: adopted into the agents settings: hand-added") {
		t.Fatalf("stderr = %s, want the adopted entry named", stderr)
	}
	fragment, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(live)), "settings.d", "agents.json"))
	if err != nil || !strings.Contains(string(fragment), "owner/hand-added") {
		t.Fatalf("agents.json = %s, %v; want the hand-added package declared", fragment, err)
	}
	if got, _ := os.ReadFile(live); !strings.Contains(string(got), "git: owner/hand-added") || !strings.Contains(string(got), "git: owner/declared") {
		t.Fatalf("live = %s, want both packages rendered", got)
	}
}
