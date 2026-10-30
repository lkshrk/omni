package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lkshrk/omni/internal/apm"
	"github.com/lkshrk/omni/internal/config"
	"github.com/lkshrk/omni/internal/executor"
)

type venvExecutor struct {
	availExecutor
	venvs int
	mu    sync.Mutex
}

func (e *venvExecutor) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	dir := ""
	switch {
	case name == "uv" && len(args) > 0 && args[0] == "venv":
		dir = args[len(args)-1]
	case name == "python3" && len(args) == 3 && args[0] == "-m" && args[1] == "venv":
		dir = args[2]
	}
	stdout, stderr, err := e.availExecutor.Run(ctx, name, args...)
	if dir != "" && err == nil {
		e.mu.Lock()
		e.venvs++
		e.mu.Unlock()
		for _, exe := range []string{"apm", "python"} {
			path := venvExecutable(dir, exe)
			if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
				return "", "", mkErr
			}
			if wErr := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); wErr != nil {
				return "", "", wErr
			}
		}
	}
	return stdout, stderr, err
}

func newPrivateAPMApp(t *testing.T, available map[string]bool) (*App, *venvExecutor) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "settings.json")
	if err := config.Save(configPath, &config.RootConfig{Version: config.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(apmOverrideEnv, "")
	mock := &venvExecutor{availExecutor: availExecutor{available: available}}
	a := New(configPath)
	a.SetFallbackExecutor(mock)
	return a, mock
}

func pinnedBuildResponses() []executor.MockCall {
	return []executor.MockCall{{}, {}, {Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"}}
}

func writeCompleteAPMCopy(t *testing.T, a *App) privateAPM {
	t.Helper()
	p, err := privateAPMCopy()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p.Bin), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Bin, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Dir, apmProvisionMarker), []byte(a.apmWheel().SHA256+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix layout: Windows builds the copy in place and needs an .exe override")
	}
}

func callLine(call executor.MockCall) string {
	return strings.Join(append([]string{call.Name}, call.Args...), " ")
}

func TestAPMOverrideIsRunAndNeverProvisioned(t *testing.T) {
	skipOnWindows(t)
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	override := filepath.Join(t.TempDir(), "apm")
	if err := os.WriteFile(override, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(apmOverrideEnv, override)
	command, err := a.ensureAPM(context.Background())
	if err != nil || command != override {
		t.Fatalf("ensureAPM = %q, %v; want the override", command, err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("calls = %#v, want none", mock.Calls)
	}
	if client := a.APMClient(apm.Global); client == nil {
		t.Fatal("no client")
	}
}

func TestAPMOverrideThatDoesNotResolveIsAnError(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	t.Setenv(apmOverrideEnv, filepath.Join(t.TempDir(), "missing-apm"))
	_, err := a.ensureAPM(context.Background())
	if !errors.Is(err, apm.ErrNotInstalled) || !strings.Contains(err.Error(), apmOverrideEnv) {
		t.Fatalf("err = %v, want a missing-override error", err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("calls = %#v, want none", mock.Calls)
	}
}

func TestEnsureAPMProvisionsWithUVOnceAndReusesTheCopy(t *testing.T) {
	skipOnWindows(t)
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true, "python3": true})
	mock.Responses = pinnedBuildResponses()
	wheel, hits := serveAPMWheelBody(t, a, []byte("wheel"), []byte("wheel"))
	command, err := a.ensureAPM(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p, _ := privateAPMCopy()
	if command != p.Bin || !strings.HasSuffix(filepath.Dir(p.Dir), filepath.Join("omni", "apm")) || filepath.Base(p.Dir) != apmVersionPin {
		t.Fatalf("command = %q, copy = %#v", command, p)
	}
	if len(mock.Calls) != 3 {
		t.Fatalf("calls = %#v", mock.Calls)
	}
	buildDir := mock.Calls[0].Args[len(mock.Calls[0].Args)-1]
	if !strings.HasPrefix(filepath.Base(buildDir), "."+apmVersionPin+"-") {
		t.Fatalf("build directory = %q", buildDir)
	}
	want := []string{
		"uv venv --quiet --python >=3.10 " + buildDir,
		"uv pip install --quiet --python " + venvExecutable(buildDir, "python") + " " + wheel,
		venvExecutable(buildDir, "apm") + " --version",
	}
	for i, call := range mock.Calls {
		if got := callLine(call); got != want[i] {
			t.Fatalf("call %d = %q, want %q", i, got, want[i])
		}
	}
	if again, err := a.ensureAPM(context.Background()); err != nil || again != p.Bin || len(mock.Calls) != 3 || hits.Load() != 1 {
		t.Fatalf("second ensureAPM = %q, %v after %d calls and %d downloads; want reuse without network", again, err, len(mock.Calls), hits.Load())
	}
}

func TestEnsureAPMProvisionsWithPython3WithoutUV(t *testing.T) {
	skipOnWindows(t)
	a, mock := newPrivateAPMApp(t, map[string]bool{"python3": true})
	mock.Responses = append([]executor.MockCall{{Stdout: "3.12\n"}}, pinnedBuildResponses()...)
	wheel := serveAPMWheel(t, a)
	if _, err := a.ensureAPM(context.Background()); err != nil {
		t.Fatal(err)
	}
	buildDir := mock.Calls[1].Args[2]
	python := venvExecutable(buildDir, "python")
	for i, want := range []string{
		"python3 -c import sys; print('%d.%d' % sys.version_info[:2])",
		"python3 -m venv " + buildDir,
		python + " -m pip install --quiet --disable-pip-version-check " + wheel,
		venvExecutable(buildDir, "apm") + " --version",
	} {
		if got := callLine(mock.Calls[i]); got != want {
			t.Fatalf("call %d = %q, want %q", i, got, want)
		}
	}
}

func TestEnsureAPMRefusesPythonOlderThan310(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"python3": true})
	mock.Responses = []executor.MockCall{{Stdout: "3.9\n"}}
	_, hits := serveAPMWheelBody(t, a, []byte("wheel"), []byte("wheel"))
	_, err := a.ensureAPM(context.Background())
	if err == nil || !strings.Contains(err.Error(), "3.10") || !strings.Contains(err.Error(), "uv") {
		t.Fatalf("err = %v, want it to name python3 3.10 and uv", err)
	}
	if hits.Load() != 0 || mock.venvs != 0 {
		t.Fatalf("downloaded %d times and built %d venvs for an unusable python", hits.Load(), mock.venvs)
	}
}

func TestEnsureAPMWithoutUVOrPython3NamesBoth(t *testing.T) {
	a, mock := newPrivateAPMApp(t, nil)
	_, err := a.ensureAPM(context.Background())
	if err == nil || !strings.Contains(err.Error(), "uv") || !strings.Contains(err.Error(), "python3") {
		t.Fatalf("err = %v, want it to name uv and python3", err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("calls = %#v", mock.Calls)
	}
}

func TestFailedProvisioningLeavesNoUsableCopy(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	mock.Responses = []executor.MockCall{{}, {Err: errors.New("network down")}}
	serveAPMWheel(t, a)
	if _, err := a.ensureAPM(context.Background()); err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("err = %v", err)
	}
	p, _ := privateAPMCopy()
	if _, err := os.Stat(p.Dir); !os.IsNotExist(err) {
		t.Fatalf("half-built copy left at %s: %v", p.Dir, err)
	}
	if _, installed := a.apmCommand(); installed {
		t.Fatal("a failed build reports an installed copy")
	}
}

func TestProvisioningRejectsAWrongVersion(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	mock.Responses = []executor.MockCall{{}, {}, {Stdout: "APM CLI version 0.27.0\n"}}
	serveAPMWheel(t, a)
	if _, err := a.ensureAPM(context.Background()); err == nil || !strings.Contains(err.Error(), "0.27.0") {
		t.Fatalf("err = %v", err)
	}
	if _, installed := a.apmCommand(); installed {
		t.Fatal("a copy reporting the wrong version is in use")
	}
}

func TestACopyWithoutItsMarkerIsRebuilt(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	serveAPMWheel(t, a)
	p := writeCompleteAPMCopy(t, a)
	if err := os.Remove(filepath.Join(p.Dir, apmProvisionMarker)); err != nil {
		t.Fatal(err)
	}
	if _, installed := a.apmCommand(); installed {
		t.Fatal("a copy without its marker counts as installed")
	}
	mock.Responses = pinnedBuildResponses()
	if _, err := a.ensureAPM(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mock.venvs != 1 {
		t.Fatalf("built %d venvs, want the incomplete copy rebuilt once", mock.venvs)
	}
}

func TestProvisioningANewPinRemovesOlderCopies(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	mock.Responses = pinnedBuildResponses()
	serveAPMWheel(t, a)
	root, _ := privateAPMRoot()
	old := filepath.Join(root, "0.35.0+omni.1")
	if err := os.MkdirAll(filepath.Join(old, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ensureAPM(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old copy %s still present: %v", old, err)
	}
	if _, installed := a.apmCommand(); !installed {
		t.Fatal("the new copy is not installed")
	}
}

func TestConcurrentProvisioningBuildsOnce(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	mock.Responses = pinnedBuildResponses()
	serveAPMWheel(t, a)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = a.provisionPrivateAPM(context.Background(), false)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if mock.venvs != 1 {
		t.Fatalf("built %d venvs, want one", mock.venvs)
	}
}

func TestRequirePinnedAPMProvisionsOnFirstNeed(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	mock.Responses = append(pinnedBuildResponses(), executor.MockCall{Stdout: "APM CLI version " + apmVersionPin + "\n"})
	serveAPMWheel(t, a)
	if a.APMAvailable() {
		t.Fatal("APMAvailable reported a copy that does not exist yet")
	}
	if err := a.requirePinnedAPM(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.APMAvailable() {
		t.Fatal("APMAvailable stale after provisioning")
	}
}

func TestDoctorWarnsBeforeTheCopyExistsAndNeverProvisions(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	result := &DoctorResult{}
	a.doctorAPMVersion(context.Background(), result, &config.RootConfig{})
	if len(result.Checks) != 1 || result.Checks[0].Status != DoctorStatusWarn || !strings.Contains(result.Checks[0].Message, "doctor --fix") {
		t.Fatalf("checks = %+v", result.Checks)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("doctor ran commands: %#v", mock.Calls)
	}
}

func TestDoctorNamesAMismatchedAPMOnPath(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"apm": true})
	serveAPMWheel(t, a)
	p := writeCompleteAPMCopy(t, a)
	mock.Responses = []executor.MockCall{
		{Stdout: "APM CLI version 0.27.0\n"},
		{Stdout: "APM CLI version " + apmVersionPin + "\n"},
	}
	result := &DoctorResult{}
	a.doctorAPMVersion(context.Background(), result, &config.RootConfig{})
	check := result.Checks[0]
	details := strings.Join(check.Details, "\n")
	if check.Status != DoctorStatusOK || !strings.Contains(details, "apm 0.27.0 on PATH is not used by omni") || !strings.Contains(details, p.Bin) {
		t.Fatalf("check = %+v", check)
	}
	if mock.Calls[0].Name != "apm" || mock.Calls[1].Name != p.Bin {
		t.Fatalf("calls = %#v", mock.Calls)
	}
}

func TestALegacyCopyDirectoryIsReplacedByThePublishedLink(t *testing.T) {
	skipOnWindows(t)
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	serveAPMWheel(t, a)
	p := writeCompleteAPMCopy(t, a)
	if err := os.Remove(filepath.Join(p.Dir, apmProvisionMarker)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("gone", p.Dir+".tmp"); err != nil {
		t.Fatal(err)
	}
	mock.Responses = pinnedBuildResponses()
	if _, err := a.ensureAPM(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(p.Dir)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("stable path %s is not the published link: %v", p.Dir, err)
	}
	if _, installed := a.apmCommand(); !installed {
		t.Fatal("the migrated copy is not installed")
	}
}

func TestAForcedRebuildRepointsTheLinkAndRemovesTheOldBuild(t *testing.T) {
	skipOnWindows(t)
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	serveAPMWheel(t, a)
	mock.Responses = append(pinnedBuildResponses(), pinnedBuildResponses()...)
	if _, err := a.provisionPrivateAPM(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	p, _ := privateAPMCopy()
	first, err := os.Readlink(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.provisionPrivateAPM(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	second, err := os.Readlink(p.Dir)
	if err != nil || second == first {
		t.Fatalf("link = %q (err %v), want it repointed from %q", second, err, first)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p.Dir), first)); !os.IsNotExist(err) {
		t.Fatalf("old build %s still present: %v", first, err)
	}
}

func TestAnOldPinsDanglingLinkIsPruned(t *testing.T) {
	skipOnWindows(t)
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	serveAPMWheel(t, a)
	root, _ := privateAPMRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "0.35.0+omni.1")
	if err := os.Symlink(".0.35.0+omni.1-123", old); err != nil {
		t.Fatal(err)
	}
	mock.Responses = pinnedBuildResponses()
	if _, err := a.ensureAPM(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatalf("old pin link %s still present: %v", old, err)
	}
}

func TestARelativeOverrideIsResolvedFromTheWorkingDirectory(t *testing.T) {
	skipOnWindows(t)
	a, _ := newPrivateAPMApp(t, nil)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("bin", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("bin", "apm"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(apmOverrideEnv, filepath.Join("bin", "apm"))
	command, installed := a.apmCommand()
	if !installed || command != filepath.Join(dir, "bin", "apm") {
		t.Fatalf("apmCommand = %q, %v; want the absolute override", command, installed)
	}
}

func TestANonExecutableOverrideIsNotAvailable(t *testing.T) {
	skipOnWindows(t)
	a, _ := newPrivateAPMApp(t, nil)
	override := filepath.Join(t.TempDir(), "apm")
	if err := os.WriteFile(override, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(apmOverrideEnv, override)
	if _, err := a.ensureAPM(context.Background()); !errors.Is(err, apm.ErrNotInstalled) {
		t.Fatalf("err = %v, want a missing-override error", err)
	}
}

func TestProvisioningWaitsForARunningAPMCommand(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	serveAPMWheel(t, a)
	mock.Responses = pinnedBuildResponses()
	root, _ := privateAPMRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	running, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_ = a.withAPMExecutionLock(func() error {
			close(running)
			<-release
			return nil
		})
	}()
	<-running
	go func() {
		_, err := a.provisionPrivateAPM(context.Background(), false)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("provisioning finished while an APM command held the runtime lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
