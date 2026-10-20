package pacman

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/lkshrk/omni/internal/executor"
)

func (p *Provider) BaseProfilePackages(ctx context.Context) (map[string]bool, error) {
	return omarchyDefaults(ctx, p.exec), nil
}

func (p *AUR) BaseProfilePackages(ctx context.Context) (map[string]bool, error) {
	return omarchyDefaults(ctx, p.exec), nil
}

// omarchyDefaults returns Omarchy's default package set, or nil when the host is not Omarchy.
func omarchyDefaults(ctx context.Context, exec executor.Executor) map[string]bool {
	if stdout, _, err := exec.Run(ctx, "omarchy-pkg-defaults"); err == nil {
		if set := packageNames(stdout); len(set) > 0 {
			return set
		}
	}
	// Installs predating omarchy-pkg-defaults ship only the base list.
	for _, dir := range omarchyPaths() {
		data, err := os.ReadFile(filepath.Join(dir, "install", "omarchy-base.packages"))
		if err == nil {
			return packageNames(string(data))
		}
	}
	return nil
}

func omarchyPaths() []string {
	var dirs []string
	if env := strings.TrimSpace(os.Getenv("OMARCHY_PATH")); env != "" {
		dirs = append(dirs, env)
	}
	dirs = append(dirs, "/usr/share/omarchy")
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "omarchy"))
	}
	return dirs
}

func packageNames(list string) map[string]bool {
	set := make(map[string]bool)
	for _, line := range strings.Split(list, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if name := strings.TrimSpace(line); name != "" {
			set[name] = true
		}
	}
	return set
}
