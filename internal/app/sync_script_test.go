package app

import (
	"context"
	"testing"

	"github.com/lkshrk/omni/internal/executor"
	"github.com/lkshrk/omni/internal/provider"
	isync "github.com/lkshrk/omni/internal/sync"
)

func TestLastNonEmptyLine(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/usr/bin\n/new/bin\n": "/new/bin",
		"/only/bin":            "/only/bin",
		"  /trimmed/bin  \n\n": "/trimmed/bin",
		"":                     "",
		"\n\n":                 "",
	}
	for in, want := range cases {
		if got := lastNonEmptyLine(in); got != want {
			t.Errorf("lastNonEmptyLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPass1InstalledScriptTool(t *testing.T) {
	t.Parallel()
	none := &isync.SyncResult{Ops: []isync.SyncOp{
		{Kind: isync.OpInstall, Tool: provider.Tool{Provider: "brew"}},
	}}
	if pass1InstalledScriptTool(none) {
		t.Error("pass1InstalledScriptTool = true; want false (no script op)")
	}
	yes := &isync.SyncResult{Ops: []isync.SyncOp{
		{Kind: isync.OpInstall, Tool: provider.Tool{Provider: "script"}},
	}}
	if !pass1InstalledScriptTool(yes) {
		t.Error("pass1InstalledScriptTool = false; want true (script install op)")
	}
	if pass1InstalledScriptTool(nil) {
		t.Error("pass1InstalledScriptTool(nil) = true; want false")
	}
}

func TestRefreshPathAfterScriptInstalls_AppendsNewHomeDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PATH", "/caller/bin:/usr/bin")
	mock := executor.NewMatchMock(executor.MatchRule{
		Pattern:  "/bin/sh -lic",
		Response: executor.MockCall{Stdout: home + "/.bun/bin:/usr/local/sbin:/usr/bin"},
	}).WithFallback(executor.MockCall{})
	pass1 := &isync.SyncResult{Ops: []isync.SyncOp{
		{Kind: isync.OpInstall, Tool: provider.Tool{Provider: "script"}},
	}}
	var got string
	refreshPathAfterScriptInstalls(context.Background(), mock, pass1, func(p string) { got = p })
	if want := "/caller/bin:/usr/bin:" + home + "/.bun/bin"; got != want {
		t.Errorf("new PATH = %q, want %q", got, want)
	}
}

func TestAppendHomePathDirs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		current, login, want string
		changed              bool
	}{
		{"/usr/bin", "/home/u/.bun/bin:/usr/bin", "/usr/bin:/home/u/.bun/bin", true},
		{"/usr/bin:/home/u/.bun/bin", "/home/u/.bun/bin", "/usr/bin:/home/u/.bun/bin", false},
		{"/usr/bin", "/usr/local/sbin:/opt/x/bin", "/usr/bin", false},
		{"/usr/bin", "/home/user2/bin", "/usr/bin", false},
		{"", "/home/u/.local/bin", "/home/u/.local/bin", true},
	} {
		got, changed := appendHomePathDirs(tc.current, tc.login, "/home/u")
		if got != tc.want || changed != tc.changed {
			t.Errorf("appendHomePathDirs(%q, %q) = %q, %v; want %q, %v", tc.current, tc.login, got, changed, tc.want, tc.changed)
		}
	}
}

func TestRefreshPathAfterScriptInstalls_SkipsWithoutScriptInstall(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	mock := executor.NewMatchMock().WithFallback(executor.MockCall{})
	pass1 := &isync.SyncResult{Ops: []isync.SyncOp{
		{Kind: isync.OpInstall, Tool: provider.Tool{Provider: "brew"}},
	}}
	called := false
	refreshPathAfterScriptInstalls(context.Background(), mock, pass1, func(string) { called = true })
	if called {
		t.Error("setenv called; want skipped when no script tool installed")
	}
}

func TestRefreshPathAfterScriptInstalls_ShellErrorKeepsPath(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	mock := executor.NewMatchMock(executor.MatchRule{
		Pattern:  "/bin/sh -lic",
		Response: executor.MockCall{Err: context.DeadlineExceeded},
	}).WithFallback(executor.MockCall{})
	pass1 := &isync.SyncResult{Ops: []isync.SyncOp{
		{Kind: isync.OpInstall, Tool: provider.Tool{Provider: "script"}},
	}}
	called := false
	refreshPathAfterScriptInstalls(context.Background(), mock, pass1, func(string) { called = true })
	if called {
		t.Error("setenv called; want skipped when shell probe errors")
	}
}
