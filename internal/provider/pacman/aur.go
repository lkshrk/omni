package pacman

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lkshrk/omni/internal/executor"
	"github.com/lkshrk/omni/internal/provider"
)

// AURHelpers lists the supported AUR helpers in preference order.
var AURHelpers = []string{"yay", "paru"}

// AUR installs through yay or paru and owns foreign (non-repo) packages; pacman owns native ones.
type AUR struct {
	exec   executor.Executor
	mu     sync.Mutex
	helper string
}

func NewAUR(exec executor.Executor) *AUR {
	return &AUR{exec: exec}
}

func (p *AUR) Name() string        { return "aur" }
func (p *AUR) Description() string { return "aur — Arch User Repository via yay or paru" }

func (p *AUR) Available(ctx context.Context) (bool, error) {
	return p.resolveHelper(ctx) != "", nil
}

func (p *AUR) resolveHelper(ctx context.Context) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.helper != "" {
		return p.helper
	}
	for _, h := range AURHelpers {
		if _, _, err := p.exec.Run(ctx, h, "--version"); err == nil {
			p.helper = h
			return h
		}
	}
	return ""
}

func (p *AUR) helperOrErr(ctx context.Context) (string, error) {
	h := p.resolveHelper(ctx)
	if h == "" {
		return "", fmt.Errorf("aur: no AUR helper found (tried: %s)", strings.Join(AURHelpers, ", "))
	}
	return h, nil
}

func (p *AUR) Install(ctx context.Context, tool provider.Tool) error {
	return p.run(ctx, provider.PrivilegeActionInstall, tool)
}

func (p *AUR) Upgrade(ctx context.Context, tool provider.Tool) error {
	return p.run(ctx, provider.PrivilegeActionUpgrade, tool)
}

func (p *AUR) Uninstall(ctx context.Context, tool provider.Tool) error {
	return p.run(ctx, provider.PrivilegeActionUninstall, tool)
}

// Helpers refuse to run as root and elevate via sudo themselves; -n makes that fail fast instead of prompting.
func (p *AUR) run(ctx context.Context, action provider.PrivilegeAction, tool provider.Tool) error {
	h, err := p.helperOrErr(ctx)
	if err != nil {
		return err
	}
	pkg := tool.EffectivePackage()
	args := append([]string{"--sudoflags=-n"}, aurArgs(h, action, pkg)...)
	return provider.RunCmd(ctx, p.exec, fmt.Sprintf("%s %s %s", h, action, pkg), h, args...)
}

func aurArgs(helper string, action provider.PrivilegeAction, pkg string) []string {
	if action == provider.PrivilegeActionUninstall {
		return []string{"-Rs", "--noconfirm", pkg}
	}
	args := []string{"-S", "--needed", "--noconfirm"}
	if helper == "paru" {
		args = append(args, "--skipreview")
	} else {
		args = append(args, "--answerdiff=None", "--answerclean=None")
	}
	return append(args, pkg)
}

func (p *AUR) PrivilegePlan(_ context.Context, action provider.PrivilegeAction, tool provider.Tool) (provider.PrivilegePlan, error) {
	return provider.SystemPrivilegePlan(p.Name(), action, tool), nil
}

// PrivilegeCommand returns the helper itself, never sudo-wrapped: yay and paru abort when run as root.
func (p *AUR) PrivilegeCommand(action provider.PrivilegeAction, tool provider.Tool) (string, []string, bool) {
	h := p.resolveHelper(context.Background())
	if h == "" {
		return "", nil, false
	}
	return h, aurArgs(h, action, tool.EffectivePackage()), true
}

func (p *AUR) IsInstalled(ctx context.Context, tool provider.Tool) (bool, string, error) {
	stdout, _, err := p.exec.Run(ctx, "pacman", "-Qm", tool.EffectivePackage())
	if err != nil {
		return false, "", nil // non-zero exit → not installed or not foreign
	}
	_, version := parsePacmanQLine(strings.TrimSpace(stdout))
	return true, version, nil
}

func (p *AUR) ListInstalled(ctx context.Context) ([]provider.InstalledTool, error) {
	return listQueried(ctx, p.exec, p.Name(), "-Qqem", "-Qm")
}

func (p *AUR) InstalledMap(ctx context.Context) (map[string]string, error) {
	return installedMapQueried(ctx, p.exec, "-Qqem", "-Qm")
}

func (p *AUR) Describe(ctx context.Context, tool provider.Tool) (string, error) {
	h, err := p.helperOrErr(ctx)
	if err != nil {
		return "", err
	}
	stdout, stderr, err := p.exec.Run(ctx, h, "-Si", "--aur", tool.EffectivePackage())
	if err != nil {
		return "", executor.WrapError(err, fmt.Sprintf("%s -Si %s", h, tool.EffectivePackage()), stdout, stderr)
	}
	return parsePacmanDescription(stdout), nil
}
