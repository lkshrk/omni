package app

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lkshrk/omni/internal/executor"
)

func newAutoApproveApp(t *testing.T, lockStatus string, responses ...executor.MockCall) (*App, *executor.MockExecutor) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(home, ".apm", "apm.yml"), "name: t\nversion: 1.0.0\n")
	lock := "dependencies:\n- repo_url: acme/ext\n  name: ext\n  version: 1.0.0\n"
	if lockStatus != "" {
		lock += "  exec_status: " + lockStatus + "\n"
	}
	writeFile(t, filepath.Join(home, ".apm", "apm.lock.yaml"), lock)
	mock := &executor.MockExecutor{Responses: append([]executor.MockCall{{Stdout: "APM CLI version " + apmVersionPin + "\n"}}, responses...)}
	a := New(filepath.Join(t.TempDir(), "settings.json"))
	a.SetFallbackExecutor(mock)
	return a, mock
}

func apmCommands(mock *executor.MockExecutor) [][]string {
	var commands [][]string
	for _, call := range mock.Calls[1:] { // call 0 is the version probe
		commands = append(commands, call.Args)
	}
	return commands
}

// Every entry point (agents add, sync, TUI row and global actions) reaches APM through RunAPM, so approving there
// covers all of them.
func TestRunAPMApprovesParkedPackagesAndInstallsAgain(t *testing.T) {
	for _, args := range [][]string{
		{"install", "-g", "acme/ext"},
		{"install", "--global", "acme/ext"},
		{"install", "-g", "--trust-transitive-mcp", "--frozen"},
		{"update", "-g", "--yes"},
	} {
		a, mock := newAutoApproveApp(t, "gated_pending_approval", executor.MockCall{Stdout: "first\n"}, executor.MockCall{Stdout: "approved\n"}, executor.MockCall{Stdout: "second\n"})
		result, err := a.RunAPM(t.Context(), args...)
		if err != nil || result.Stdout != "first\napproved\nsecond\n" {
			t.Fatalf("%v: result = %#v, err = %v", args, result, err)
		}
		want := [][]string{args, {"approve", "--all", "--user"}, args}
		got := apmCommands(mock)
		if len(got) != 3 || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) || !slices.Equal(got[2], want[2]) {
			t.Fatalf("%v: commands = %v, want %v", args, got, want)
		}
	}
}

func TestRunAPMLeavesInstallsAloneWhenNothingIsParked(t *testing.T) {
	for _, status := range []string{"", "deployed", "denied"} {
		a, mock := newAutoApproveApp(t, status, executor.MockCall{Stdout: "installed\n"})
		result, err := a.RunAPM(t.Context(), "install", "-g")
		if err != nil || result.Stdout != "installed\n" || len(apmCommands(mock)) != 1 {
			t.Fatalf("status %q: result = %#v, err = %v, commands = %v", status, result, err, apmCommands(mock))
		}
	}
}

func TestRunAPMApprovesOnlyAfterAGlobalNonDryInstallOrUpdate(t *testing.T) {
	for _, args := range [][]string{
		{"install", "-g", "--dry-run"},
		{"install"},
		{"install", "acme/ext"},
		{"outdated", "-g"},
		{"uninstall", "-g", "acme/ext"},
		{"audit", "--ci"},
		{"marketplace", "update", "team"},
	} {
		a, mock := newAutoApproveApp(t, "gated_pending_approval", executor.MockCall{Stdout: "ok\n"})
		if _, err := a.RunAPM(t.Context(), args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if got := apmCommands(mock); len(got) != 1 {
			t.Fatalf("%v ran %v, want one command and no approval", args, got)
		}
	}
}

func TestRunAPMDoesNotApproveAfterAFailedInstall(t *testing.T) {
	a, mock := newAutoApproveApp(t, "gated_pending_approval", executor.MockCall{Stderr: "boom\n", Err: errors.New("exit status 1")})
	if _, err := a.RunAPM(t.Context(), "install", "-g"); err == nil {
		t.Fatal("failed install reported success")
	}
	if got := apmCommands(mock); len(got) != 1 {
		t.Fatalf("approved after a failed install: %v", got)
	}
}
