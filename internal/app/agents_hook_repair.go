package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lkshrk/omni/internal/apm"
)

var claudeSettingsRel = filepath.Join(".claude", "settings.json")

type settingsLink struct {
	path, raw, target string
}

func captureSettingsLink(home string) settingsLink {
	path := filepath.Join(home, claudeSettingsRel)
	raw, err := os.Readlink(path)
	if err != nil {
		return settingsLink{}
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return settingsLink{}
	}
	return settingsLink{path: path, raw: raw, target: target}
}

// restore undoes apm's atomic rename over a dotfiles symlink: the new content goes to the link target.
func (l settingsLink) restore() error {
	if l.target == "" {
		return nil
	}
	info, err := os.Lstat(l.path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	content, err := os.ReadFile(l.path)
	if err != nil {
		return err
	}
	perm := info.Mode().Perm()
	if targetInfo, err := os.Stat(l.target); err == nil {
		perm = targetInfo.Mode().Perm()
	}
	if err := replaceFile(l.target, content, perm); err != nil {
		return err
	}
	tmp := l.path + ".omni-link"
	_ = os.Remove(tmp)
	if err := os.Symlink(l.raw, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func replaceFile(path string, content []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(content)
	err = errors.Join(err, tmp.Chmod(perm), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// apm deploys a plugin's hook scripts without .claude-plugin/plugin.json, which some (codex-plugin-cc) read at import.
func deployHookPluginManifests(home string, lock apmLockfile, modulesRoot string) error {
	var errs []error
	for _, dep := range lock.Dependencies {
		hookDirs := make(map[string]bool)
		for _, deployed := range dep.DeployedFiles {
			parts := strings.Split(deployed, "/")
			if len(parts) >= 4 && parts[0] == ".claude" && parts[1] == "hooks" && validHookDir(parts[2]) {
				hookDirs[parts[2]] = true
			}
		}
		if len(hookDirs) == 0 {
			continue
		}
		module, ok := resolveModulePath(modulesRoot, apmPackageSource(dep.RepoURL, dep.VirtualPath))
		if !ok {
			continue
		}
		manifest, err := os.ReadFile(filepath.Join(module, ".claude-plugin", "plugin.json"))
		if err != nil {
			continue
		}
		for dir := range hookDirs {
			dest := filepath.Join(home, ".claude", "hooks", dir, ".claude-plugin", "plugin.json")
			if current, err := os.ReadFile(dest); err == nil && bytes.Equal(current, manifest) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := os.WriteFile(dest, manifest, 0o644); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func validHookDir(name string) bool {
	return name != "." && filepath.IsLocal(name) && !strings.ContainsRune(name, '\\')
}

func repairClaudeHooks(home string, link settingsLink) error {
	if err := link.restore(); err != nil {
		return fmt.Errorf("restore settings.json symlink: %w", err)
	}
	if _, err := portableHookPaths(home); err != nil {
		return fmt.Errorf("anchor hook paths to $HOME: %w", err)
	}
	lock, err := readAPMLockfile()
	if err != nil {
		return err
	}
	dir, err := apm.GlobalWorkspaceDir()
	if err != nil {
		return err
	}
	if err := deployHookPluginManifests(home, lock, filepath.Join(dir, "apm_modules")); err != nil {
		return fmt.Errorf("deploy hook plugin manifests: %w", err)
	}
	return nil
}
