package pacman_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lkshrk/omni/internal/executor"
	"github.com/lkshrk/omni/internal/provider"
	"github.com/lkshrk/omni/internal/provider/pacman"
)

func newAUR(responses ...executor.MockCall) (*pacman.AUR, *executor.MockExecutor) {
	m := &executor.MockExecutor{Responses: responses}
	return pacman.NewAUR(m), m
}

func aurTool(name string) provider.Tool {
	return provider.Tool{Name: name, Provider: "aur", Package: name}
}

func TestAURAvailablePrefersYay(t *testing.T) {
	p, m := newAUR(executor.MockCall{Stdout: "yay v12.4.2"})
	ok, err := p.Available(context.Background())
	if err != nil || !ok {
		t.Fatalf("Available() = (%v, %v), want (true, nil)", ok, err)
	}
	if m.Calls[0].Name != "yay" {
		t.Errorf("probed %q first, want yay", m.Calls[0].Name)
	}
}

func TestAURAvailableFallsBackToParu(t *testing.T) {
	p, m := newAUR(executor.MockCall{Err: errors.New("not found")}, executor.MockCall{Stdout: "paru v2.0.4"})
	if ok, _ := p.Available(context.Background()); !ok {
		t.Fatal("Available() = false, want true via paru")
	}
	if err := p.Install(context.Background(), aurTool("walker-bin")); err != nil {
		t.Fatalf("Install: %v", err)
	}
	last := m.Calls[len(m.Calls)-1]
	if last.Name != "paru" || !slices.Contains(last.Args, "--skipreview") {
		t.Errorf("install call = %s %v, want paru with --skipreview", last.Name, last.Args)
	}
}

func TestAURUnavailableWithoutHelper(t *testing.T) {
	notFound := executor.MockCall{Err: errors.New("not found")}
	p, _ := newAUR(notFound, notFound, notFound, notFound)
	ok, err := p.Available(context.Background())
	if err != nil || ok {
		t.Fatalf("Available() = (%v, %v), want (false, nil)", ok, err)
	}
	if err := p.Install(context.Background(), aurTool("x")); err == nil || !strings.Contains(err.Error(), "no AUR helper") {
		t.Fatalf("Install error = %v, want no AUR helper", err)
	}
}

func TestAURInstallIsNonInteractive(t *testing.T) {
	p, m := newAUR(executor.MockCall{Stdout: "yay v12"}, executor.MockCall{})
	if err := p.Install(context.Background(), aurTool("1password-cli")); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := strings.Join(m.Calls[1].Args, " ")
	want := "--sudoflags=-n -S --needed --noconfirm --answerdiff=None --answerclean=None 1password-cli"
	if m.Calls[1].Name != "yay" || got != want {
		t.Errorf("install = %s %s, want yay %s", m.Calls[1].Name, got, want)
	}
}

func TestAURUninstallRemovesOrphans(t *testing.T) {
	p, m := newAUR(executor.MockCall{Stdout: "yay v12"}, executor.MockCall{})
	if err := p.Uninstall(context.Background(), aurTool("walker-bin")); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := strings.Join(m.Calls[1].Args, " "); got != "--sudoflags=-n -Rs --noconfirm walker-bin" {
		t.Errorf("uninstall args = %q", got)
	}
}

func TestAURInstallErrorIncludesStderr(t *testing.T) {
	p, _ := newAUR(executor.MockCall{Stdout: "yay v12"}, executor.MockCall{Err: errors.New("exit 1"), Stderr: "sudo: a password is required"})
	err := p.Install(context.Background(), aurTool("x"))
	if err == nil || !strings.Contains(err.Error(), "a password is required") {
		t.Fatalf("Install error = %v, want stderr detail", err)
	}
	if plan, ok := provider.ClassifyPrivilegeError(err); !ok || !plan.RequiresPrivilege() {
		t.Errorf("sudo failure not classified as privilege error: %v", err)
	}
}

func TestAURPrivilegeCommandIsNotSudoWrapped(t *testing.T) {
	p, _ := newAUR(executor.MockCall{Stdout: "yay v12"})
	cmd, args, ok := p.PrivilegeCommand(provider.PrivilegeActionInstall, aurTool("x"))
	if !ok || cmd != "yay" || slices.Contains(args, "--sudoflags=-n") {
		t.Errorf("PrivilegeCommand = (%q, %v, %v), want interactive yay without -n", cmd, args, ok)
	}
}

func TestAURListsOnlyExplicitForeignPackages(t *testing.T) {
	p, m := newAUR(
		executor.MockCall{Stdout: "walker-bin\n"},
		executor.MockCall{Stdout: "walker-bin 0.12.0-1\nlibfoo 1.0-1\n"},
	)
	tools, err := p.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "walker-bin" || tools[0].Version != "0.12.0-1" || tools[0].Provider != "aur" {
		t.Errorf("ListInstalled = %+v", tools)
	}
	if m.Calls[0].Args[0] != "-Qqem" || m.Calls[1].Args[0] != "-Qm" {
		t.Errorf("queries = %v %v, want -Qqem then -Qm", m.Calls[0].Args, m.Calls[1].Args)
	}
}

func TestAURIsInstalledUsesForeignQuery(t *testing.T) {
	p, m := newAUR(executor.MockCall{Stdout: "walker-bin 0.12.0-1"})
	ok, ver, err := p.IsInstalled(context.Background(), aurTool("walker-bin"))
	if err != nil || !ok || ver != "0.12.0-1" {
		t.Fatalf("IsInstalled = (%v, %q, %v)", ok, ver, err)
	}
	if strings.Join(m.Calls[0].Args, " ") != "-Qm walker-bin" {
		t.Errorf("args = %v, want -Qm walker-bin", m.Calls[0].Args)
	}
}

func TestPacmanListsOnlyNativePackages(t *testing.T) {
	p, m := newPacman(executor.MockCall{Stdout: "ripgrep\n"}, executor.MockCall{Stdout: "ripgrep 15.1.0-2\n"})
	if _, err := p.ListInstalled(context.Background()); err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if m.Calls[0].Args[0] != "-Qqen" || m.Calls[1].Args[0] != "-Qn" {
		t.Errorf("queries = %v %v, want -Qqen then -Qn", m.Calls[0].Args, m.Calls[1].Args)
	}
}

func TestAURListEmptyWhenNoForeignPackages(t *testing.T) {
	noMatch := executor.MockCall{Err: errors.New("exit status 1")}
	p, _ := newAUR(noMatch, noMatch)
	m, err := p.InstalledMap(context.Background())
	if err != nil || len(m) != 0 {
		t.Fatalf("InstalledMap() = (%v, %v), want empty map and nil error", m, err)
	}
}
