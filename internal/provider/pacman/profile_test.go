package pacman_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lkshrk/omni/internal/executor"
)

func TestBaseProfileFromOmarchyPkgDefaults(t *testing.T) {
	t.Setenv("OMARCHY_PATH", t.TempDir())
	p, m := newPacman(executor.MockCall{Stdout: "hyprland\nwaybar\n\nwalker\n"})
	set, err := p.BaseProfilePackages(context.Background())
	if err != nil {
		t.Fatalf("BaseProfilePackages: %v", err)
	}
	if len(set) != 3 || !set["hyprland"] || !set["walker"] {
		t.Errorf("set = %v, want hyprland, waybar, walker", set)
	}
	if m.Calls[0].Name != "omarchy-pkg-defaults" {
		t.Errorf("probe = %q, want omarchy-pkg-defaults", m.Calls[0].Name)
	}
}

func TestBaseProfileFallsBackToBaseList(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "install"), 0o755); err != nil {
		t.Fatal(err)
	}
	list := "# Omarchy core package list\n\nbat\nhyprland # compositor\n  waybar  \n"
	if err := os.WriteFile(filepath.Join(dir, "install", "omarchy-base.packages"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMARCHY_PATH", dir)
	p, _ := newAUR(executor.MockCall{Err: errors.New("not found")})
	set, err := p.BaseProfilePackages(context.Background())
	if err != nil {
		t.Fatalf("BaseProfilePackages: %v", err)
	}
	if len(set) != 3 || !set["bat"] || !set["hyprland"] || !set["waybar"] {
		t.Errorf("set = %v, want bat, hyprland, waybar", set)
	}
}

func TestBaseProfileNilOffOmarchy(t *testing.T) {
	t.Setenv("OMARCHY_PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, err := os.Stat("/usr/share/omarchy"); err == nil {
		t.Skip("host has /usr/share/omarchy")
	}
	p, _ := newPacman(executor.MockCall{Err: errors.New("not found")})
	set, err := p.BaseProfilePackages(context.Background())
	if err != nil || set != nil {
		t.Errorf("BaseProfilePackages = (%v, %v), want (nil, nil)", set, err)
	}
}
