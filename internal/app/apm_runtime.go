package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/lkshrk/omni/internal/apm"
	"github.com/lkshrk/omni/internal/config"
	"github.com/lkshrk/omni/internal/executor"
	"github.com/lkshrk/omni/internal/flock"
)

// apmOverrideEnv names the one APM executable, a path or a PATH name, that omni runs instead of its private copy.
const apmOverrideEnv = "OMNI_APM"

// A copy is usable only once this marker holds the digest of the wheel it was built from; it is written last.
const apmProvisionMarker = ".omni-provisioned"

func apmOverride() string {
	override := strings.TrimSpace(os.Getenv(apmOverrideEnv))
	if strings.ContainsAny(override, `/\`) {
		if abs, err := filepath.Abs(override); err == nil {
			return abs
		}
	}
	return override
}

type privateAPM struct {
	Dir string
	Bin string
}

func venvExecutable(dir, name string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "Scripts", name+".exe")
	}
	return filepath.Join(dir, "bin", name)
}

func privateAPMRoot() (string, error) {
	data, err := config.DefaultDataDir()
	if err != nil {
		return "", fmt.Errorf("resolving omni's APM directory: %w", err)
	}
	return filepath.Join(data, "apm"), nil
}

func privateAPMCopy() (privateAPM, error) {
	root, err := privateAPMRoot()
	if err != nil {
		return privateAPM{}, err
	}
	dir := filepath.Join(root, apmVersionPin)
	return privateAPM{Dir: dir, Bin: venvExecutable(dir, "apm")}, nil
}

func (p privateAPM) complete(digest string) bool {
	raw, err := os.ReadFile(filepath.Join(p.Dir, apmProvisionMarker))
	if err != nil || strings.TrimSpace(string(raw)) != digest {
		return false
	}
	info, err := os.Stat(p.Bin)
	return err == nil && !info.IsDir()
}

func (a *App) apmCommand() (string, bool) {
	if override := apmOverride(); override != "" {
		return override, a.overrideAvailable(override)
	}
	p, err := privateAPMCopy()
	if err != nil {
		return "", false
	}
	return p.Bin, p.complete(a.apmWheel().SHA256)
}

func (a *App) overrideAvailable(command string) bool {
	if strings.ContainsAny(command, `/\`) {
		_, err := exec.LookPath(command)
		return err == nil
	}
	return a.commandAvailable(command)
}

func errAPMOverrideMissing(override string) error {
	return &APMRepairError{Kind: APMRepairMissing, Required: apmVersionPin,
		Err: fmt.Errorf("%w: %s=%s does not resolve to an executable", apm.ErrNotInstalled, apmOverrideEnv, override)}
}

func (a *App) ensureAPM(ctx context.Context) (string, error) {
	if command, ok := a.apmCommand(); ok {
		return command, nil
	}
	if override := apmOverride(); override != "" {
		return "", errAPMOverrideMissing(override)
	}
	return a.provisionPrivateAPM(ctx, false)
}

type apmBuilder struct {
	name    string
	venv    func(dir string) []string
	install func(python, wheel string) []string
}

func manualAPMInstallHint() string {
	return fmt.Sprintf("install uv (https://docs.astral.sh/uv/; required on Windows) or python3 3.10 or newer, then run 'omni doctor --fix'; or set %s to an apm %s executable", apmOverrideEnv, apmVersionPin)
}

func (a *App) apmBuilderFor(ctx context.Context) (apmBuilder, error) {
	if a.commandAvailable("uv") {
		return apmBuilder{
			name: "uv",
			venv: func(dir string) []string { return []string{"uv", "venv", "--quiet", "--python", ">=3.10", dir} },
			install: func(python, wheel string) []string {
				return []string{"uv", "pip", "install", "--quiet", "--python", python, wheel}
			},
		}, nil
	}
	if !a.commandAvailable("python3") {
		return apmBuilder{}, fmt.Errorf("no uv and no python3 found to install apm %s: %s", apmVersionPin, manualAPMInstallHint())
	}
	stdout, stderr, err := a.fallbackExecutor().Run(ctx, "python3", "-c", "import sys; print('%d.%d' % sys.version_info[:2])")
	if err != nil {
		return apmBuilder{}, executor.WrapError(err, "python3 version check", stdout, stderr)
	}
	if !pythonAtLeast(strings.TrimSpace(stdout), 3, 10) {
		return apmBuilder{}, fmt.Errorf("python3 is %s, but apm %s needs 3.10 or newer and no uv was found: %s",
			strings.TrimSpace(stdout), apmVersionPin, manualAPMInstallHint())
	}
	return apmBuilder{
		name: "python3",
		venv: func(dir string) []string { return []string{"python3", "-m", "venv", dir} },
		install: func(python, wheel string) []string {
			return []string{python, "-m", "pip", "install", "--quiet", "--disable-pip-version-check", wheel}
		},
	}, nil
}

func pythonAtLeast(version string, major, minor int) bool {
	majorText, minorText, ok := strings.Cut(version, ".")
	if !ok {
		return false
	}
	gotMajor, err1 := strconv.Atoi(majorText)
	gotMinor, err2 := strconv.Atoi(minorText)
	if err1 != nil || err2 != nil {
		return false
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

func (a *App) provisionPrivateAPM(ctx context.Context, force bool) (string, error) {
	p, err := privateAPMCopy()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(p.Dir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", root, err)
	}
	unlock, err := lockAPMRoot(root, false)
	if err != nil {
		return "", err
	}
	defer unlock()
	digest := a.apmWheel().SHA256
	if !force && p.complete(digest) {
		return p.Bin, nil
	}
	builder, err := a.apmBuilderFor(ctx)
	if err != nil {
		return "", err
	}
	wheel, err := a.ensureAPMWheel(ctx)
	if err != nil {
		return "", fmt.Errorf("fetching pinned apm %s: %w", apmVersionPin, err)
	}
	emitAPMNotice(ctx, fmt.Sprintf("omni: installing apm %s into %s with %s", apmVersionPin, p.Dir, builder.name))
	if runtime.GOOS == "windows" {
		return a.buildStableAPM(ctx, builder, p, wheel, digest)
	}
	buildDir, err := os.MkdirTemp(root, "."+apmVersionPin+"-")
	if err != nil {
		return "", fmt.Errorf("creating temporary apm copy: %w", err)
	}
	build := privateAPM{Dir: buildDir, Bin: venvExecutable(buildDir, "apm")}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(build.Dir)
		}
	}()
	if err := a.buildAPMCopy(ctx, builder, build, wheel, digest); err != nil {
		return "", err
	}
	if err := publishAPMCopy(p.Dir, build.Dir); err != nil {
		return "", err
	}
	published = true
	pruneOtherAPMCopies(root, filepath.Base(p.Dir), filepath.Base(build.Dir))
	return p.Bin, nil
}

// Windows has no unprivileged symlinks, so its copy is rebuilt in place under the exclusive lock.
func (a *App) buildStableAPM(ctx context.Context, builder apmBuilder, p privateAPM, wheel, digest string) (string, error) {
	if err := os.RemoveAll(p.Dir); err != nil {
		return "", fmt.Errorf("removing incomplete apm copy %s: %w", p.Dir, err)
	}
	if err := a.buildAPMCopy(ctx, builder, p, wheel, digest); err != nil {
		_ = os.RemoveAll(p.Dir)
		return "", err
	}
	pruneOtherAPMCopies(filepath.Dir(p.Dir), filepath.Base(p.Dir))
	return p.Bin, nil
}

func publishAPMCopy(link, target string) error {
	tmp := link + ".tmp"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing temporary apm link: %w", err)
	}
	if err := os.Symlink(filepath.Base(target), tmp); err != nil {
		return fmt.Errorf("publishing apm copy: %w", err)
	}
	if info, err := os.Lstat(link); err == nil && info.IsDir() { // migrate the pre-symlink layout.
		if err := os.RemoveAll(link); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("replacing apm copy: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return fmt.Errorf("inspecting apm copy: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publishing apm copy: %w", err)
	}
	return nil
}

func (a *App) buildAPMCopy(ctx context.Context, builder apmBuilder, p privateAPM, wheel, digest string) error {
	for _, step := range [][]string{builder.venv(p.Dir), builder.install(venvExecutable(p.Dir, "python"), wheel)} {
		stdout, stderr, err := a.fallbackExecutor().Run(ctx, step[0], step[1:]...)
		if err != nil {
			return executor.WrapError(err, "installing apm "+apmVersionPin+": "+strings.Join(step, " "), stdout, stderr)
		}
	}
	version, err := a.apmVersionOf(ctx, p.Bin)
	if err != nil {
		return fmt.Errorf("installed apm %s could not be verified: %w", apmVersionPin, err)
	}
	if !apmVersionPinned(version) {
		return fmt.Errorf("installed apm reports %s, want %s", version, apmVersionPin)
	}
	marker := filepath.Join(p.Dir, apmProvisionMarker)
	tmp := marker + ".tmp"
	if err := os.WriteFile(tmp, []byte(digest+"\n"), 0o600); err != nil {
		return fmt.Errorf("recording apm copy: %w", err)
	}
	if err := os.Rename(tmp, marker); err != nil {
		return fmt.Errorf("recording apm copy: %w", err)
	}
	return nil
}

func lockAPMRoot(root string, shared bool) (func(), error) {
	file, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening apm runtime lock: %w", err)
	}
	if shared {
		err = flock.RLock(file)
	} else {
		err = flock.Lock(file)
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("locking apm runtime: %w", err)
	}
	return func() { _ = flock.Unlock(file); _ = file.Close() }, nil
}

func (a *App) withAPMExecutionLock(fn func() error) error {
	if apmOverride() != "" {
		return fn()
	}
	root, err := privateAPMRoot()
	if err != nil {
		return err
	}
	unlock, err := lockAPMRoot(root, true)
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}

// Cleanup is best effort: stale copies that cannot be removed now are retried after the next pin change.
func pruneOtherAPMCopies(root string, keep ...string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".lock" || slices.Contains(keep, name) {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			_ = os.RemoveAll(filepath.Join(root, name))
		}
	}
}

func emitAPMNotice(ctx context.Context, message string) {
	if !executor.EmitOutput(ctx, message) {
		fmt.Fprintln(os.Stderr, message)
	}
}

// pathAPMVersion probes an apm on PATH only so doctor can name a version that differs from omni's copy.
func (a *App) pathAPMVersion(ctx context.Context) (string, bool) {
	if apmOverride() != "" || !a.commandAvailable("apm") {
		return "", false
	}
	version, err := a.apmVersionOf(ctx, "apm")
	if err != nil {
		return "unknown", true
	}
	return version, true
}

var errNoAPMCopy = errors.New("omni's apm copy is not installed yet; it installs on first use or with 'omni doctor --fix'")

func apmMissingMessage() string {
	if override := apmOverride(); override != "" {
		return errAPMOverrideMissing(override).Error()
	}
	return errNoAPMCopy.Error()
}
