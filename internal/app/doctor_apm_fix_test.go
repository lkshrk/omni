package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lkshrk/omni/internal/config"
	"github.com/lkshrk/omni/internal/executor"
)

type availExecutor struct {
	executor.MockExecutor
	available map[string]bool
}

func (e *availExecutor) CommandAvailable(name string) bool {
	return e.available[name]
}

func newAPMFixApp(t *testing.T, available map[string]bool) (*App, *availExecutor) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "settings.json")
	if err := config.Save(configPath, &config.RootConfig{Version: config.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	mock := &availExecutor{available: available}
	a := New(configPath)
	a.SetFallbackExecutor(mock)
	return a, mock
}

// serveAPMWheelBody points a at a throwaway wheel pinned to the digest of pinned but serving served, so a
// mismatching pair models a tampered release. It returns where the verified copy lands and the download count.
func serveAPMWheelBody(t *testing.T, a *App, served, pinned []byte) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(served)
	}))
	t.Cleanup(srv.Close)
	a.SetGitHubFallbackAPIForTest(srv.URL, srv.Client())
	sum := sha256.Sum256(pinned)
	const file = "apm_cli-9.9.9+test-py3-none-any.whl"
	a.apmWheelOverride = &apmWheel{URL: srv.URL + "/" + file, SHA256: hex.EncodeToString(sum[:]), File: file}
	path, err := a.apmWheelPath()
	if err != nil {
		t.Fatal(err)
	}
	return path, &hits
}

func serveAPMWheel(t *testing.T, a *App) string {
	t.Helper()
	path, _ := serveAPMWheelBody(t, a, []byte("PK-fake-wheel"), []byte("PK-fake-wheel"))
	return path
}

func TestFixMissingAPMRefusesAWheelThatFailsItsDigest(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"uv": true})
	wheel, _ := serveAPMWheelBody(t, a, []byte("tampered"), []byte("pinned"))
	_, err := a.FixMissingAPM(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("installer ran on an unverified wheel: %#v", mock.Calls)
	}
	if _, statErr := os.Stat(wheel); !os.IsNotExist(statErr) {
		t.Fatalf("unverified wheel left in the cache: %v", statErr)
	}
}

func TestFixMissingAPMReusesVerifiedCachedWheelOffline(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"uv": true})
	mock.Responses = []executor.MockCall{
		{},
		{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"},
	}
	wheel, hits := serveAPMWheelBody(t, a, []byte("not served"), []byte("cached"))
	if err := os.MkdirAll(filepath.Dir(wheel), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wheel, []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || report.Installed != "uv tool install "+wheel {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if hits.Load() != 0 {
		t.Fatalf("downloaded %d times despite a verified cached wheel", hits.Load())
	}
}

func TestFixMissingAPMReplacesACorruptCachedWheel(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"uv": true})
	mock.Responses = []executor.MockCall{
		{},
		{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"},
	}
	wheel, hits := serveAPMWheelBody(t, a, []byte("good"), []byte("good"))
	if err := os.MkdirAll(filepath.Dir(wheel), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wheel, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.FixMissingAPM(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(wheel); err != nil || string(got) != "good" || hits.Load() != 1 {
		t.Fatalf("cached wheel = %q (err %v) after %d downloads, want the served bytes", got, err, hits.Load())
	}
}

func TestFixMissingAPMDownloadsNothingWithoutAnInstaller(t *testing.T) {
	a, _ := newAPMFixApp(t, nil)
	_, hits := serveAPMWheelBody(t, a, []byte("wheel"), []byte("wheel"))
	if _, err := a.FixMissingAPM(context.Background(), false); err == nil {
		t.Fatal("expected the no-installer error")
	}
	if hits.Load() != 0 {
		t.Fatalf("downloaded the wheel %d times with no installer to use it", hits.Load())
	}
}

func TestPinnedAPMWheelMatchesThePinnedVersion(t *testing.T) {
	wheel, err := parseAPMWheel(apmPackagePin)
	if err != nil {
		t.Fatal(err)
	}
	if want := "apm_cli-" + apmVersionPin + "-py3-none-any.whl"; wheel.File != want {
		t.Fatalf("pinned wheel file = %q, want %q", wheel.File, want)
	}
	if strings.Contains(wheel.URL, "#") {
		t.Fatalf("install URL %q keeps the hash fragment that uv and pipx reject", wheel.URL)
	}
}

func TestParseAPMWheelRejectsUnverifiablePins(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	for name, pin := range map[string]string{
		"plain http":      "http://example.test/a-1.0-py3-none-any.whl#sha256=" + digest,
		"no digest":       "https://example.test/a-1.0-py3-none-any.whl",
		"short digest":    "https://example.test/a-1.0-py3-none-any.whl#sha256=abcd",
		"non-hex digest":  "https://example.test/a-1.0-py3-none-any.whl#sha256=" + strings.Repeat("zz", 32),
		"other algorithm": "https://example.test/a-1.0-py3-none-any.whl#md5=" + digest,
		"not a wheel":     "https://example.test/a-1.0.tar.gz#sha256=" + digest,
	} {
		if _, err := parseAPMWheel(pin); err == nil {
			t.Errorf("%s: parseAPMWheel(%q) accepted the pin", name, pin)
		}
	}
}

func TestFixMissingAPMNoopWhenInstalled(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"apm": true, "uv": true})
	mock.Responses = []executor.MockCall{{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"}}
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || !report.AlreadyInstalled {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if len(mock.Calls) != 1 || mock.Calls[0].Name != "apm" {
		t.Fatalf("unexpected calls: %#v", mock.Calls)
	}
}

func TestFixMissingAPMDryRunPlansPreferredInstaller(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"uv": true, "pip3": true})
	report, err := a.FixMissingAPM(context.Background(), true)
	if err != nil || report.Planned != "uv tool install "+a.apmWheel().URL {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("dry run executed commands: %#v", mock.Calls)
	}
}

func TestFixMissingAPMInstallsViaFallbackInstaller(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"pip3": true})
	mock.Responses = []executor.MockCall{
		{},
		{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"},
	}
	wheel := serveAPMWheel(t, a)
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || report.Installed != "pip3 install --user "+wheel {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if len(mock.Calls) != 2 || mock.Calls[0].Name != "pip3" || mock.Calls[1].Name != "apm" {
		t.Fatalf("calls = %#v", mock.Calls)
	}
}

type flipExecutor struct {
	availExecutor
	makeAvailable string
}

func (e *flipExecutor) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	stdout, stderr, err := e.availExecutor.Run(ctx, name, args...)
	if err == nil && e.makeAvailable != "" {
		e.available[e.makeAvailable] = true
	}
	return stdout, stderr, err
}

func TestFixMissingAPMReportsResolvableInstall(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "settings.json")
	if err := config.Save(configPath, &config.RootConfig{Version: config.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	mock := &flipExecutor{availExecutor: availExecutor{available: map[string]bool{"uv": true}}, makeAvailable: "apm"}
	mock.Responses = []executor.MockCall{
		{},
		{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"},
	}
	a := New(configPath)
	a.SetFallbackExecutor(mock)
	wheel := serveAPMWheel(t, a)
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || report.Installed != "uv tool install "+wheel || report.NotOnPATH {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
}

func TestFixMissingAPMRejectsUnverifiableInstall(t *testing.T) {
	a, _ := newAPMFixApp(t, map[string]bool{"uv": true})
	serveAPMWheel(t, a)
	report, err := a.FixMissingAPM(context.Background(), false)
	if err == nil || report.Installed != "" || !strings.Contains(err.Error(), apmVersionPin) {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
}

func TestFixMissingAPMRetriesExternallyManagedPip(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "settings.json")
	if err := config.Save(configPath, &config.RootConfig{Version: config.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	mock := &availExecutor{available: map[string]bool{"pip3": true}}
	mock.Responses = []executor.MockCall{
		{Stderr: "error: externally-managed-environment\n", Err: context.DeadlineExceeded},
		{},
		{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"},
	}
	a := New(configPath)
	a.SetFallbackExecutor(mock)
	wheel := serveAPMWheel(t, a)
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || report.Installed != "pip3 install --user "+wheel+" --break-system-packages" {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if len(mock.Calls) != 3 || mock.Calls[1].Args[len(mock.Calls[1].Args)-1] != "--break-system-packages" || mock.Calls[2].Name != "apm" {
		t.Fatalf("calls = %#v", mock.Calls)
	}
}

func TestFixMissingAPMDoesNotRetryOtherPipFailures(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "settings.json")
	if err := config.Save(configPath, &config.RootConfig{Version: config.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	mock := &availExecutor{available: map[string]bool{"pip3": true}}
	mock.Responses = []executor.MockCall{{Stderr: "network unreachable\n", Err: context.DeadlineExceeded}}
	a := New(configPath)
	a.SetFallbackExecutor(mock)
	serveAPMWheel(t, a)
	_, err := a.FixMissingAPM(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "network unreachable") {
		t.Fatalf("err = %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("calls = %#v", mock.Calls)
	}
}

func TestFixMissingAPMErrorsWithoutInstaller(t *testing.T) {
	a, _ := newAPMFixApp(t, nil)
	_, err := a.FixMissingAPM(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), a.apmWheel().URL) {
		t.Fatalf("err = %v", err)
	}
}

// apm is on PATH but does not match the pin: the first upgrader failing means it was installed by another one, so
// the fix tries the rest before reporting the upgrade impossible.
func TestFixMissingAPMFallsThroughToTheNextUpgrader(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"apm": true, "uv": true, "pipx": true})
	mock.Responses = []executor.MockCall{
		{Stdout: "Agent Package Manager (APM) CLI version 0.0.1\n"},
		{Stderr: "`apm-cli` is not installed\n", Err: errors.New("exit status 1")},
		{Stdout: "upgraded\n"},
		{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"},
	}

	wheel := serveAPMWheel(t, a)
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || report.Upgraded != "pipx install --force "+wheel {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if len(mock.Calls) != 4 {
		t.Fatalf("calls = %#v, want the failed uv upgrade followed by pipx", mock.Calls)
	}
}

func TestFixMissingAPMReprobesAfterSuccessfulUpgrade(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"apm": true, "uv": true, "pipx": true})
	mock.Responses = []executor.MockCall{
		{Stdout: "Agent Package Manager (APM) CLI version 0.0.1\n"},
		{},
		{Stdout: "Agent Package Manager (APM) CLI version 0.0.1\n"},
		{},
		{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"},
	}

	wheel := serveAPMWheel(t, a)
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || report.Upgraded != "pipx install --force "+wheel {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	want := []string{"apm", "uv", "apm", "pipx", "apm"}
	if len(mock.Calls) != len(want) {
		t.Fatalf("calls = %#v", mock.Calls)
	}
	for i, call := range mock.Calls {
		if call.Name != want[i] {
			t.Fatalf("calls = %#v", mock.Calls)
		}
	}
}

func TestFixMissingAPMReportsEveryFailedUpgrader(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"apm": true, "uv": true, "pipx": true})
	serveAPMWheel(t, a)
	mock.Responses = []executor.MockCall{
		{Stdout: "Agent Package Manager (APM) CLI version 0.0.1\n"},
		{Stderr: "uv refused\n", Err: errors.New("exit status 1")},
		{Stderr: "pipx refused\n", Err: errors.New("exit status 1")},
	}

	_, err := a.FixMissingAPM(context.Background(), false)
	if err == nil {
		t.Fatal("every upgrader failed but the fix reported success")
	}
	for _, want := range []string{"uv refused", "pipx refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to name %q", err, want)
		}
	}
}

func newSyncWithoutAPMApp(t *testing.T) (*App, *availExecutor, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "settings.json")
	if err := config.Save(configPath, &config.RootConfig{Version: config.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	if err := os.MkdirAll(filepath.Join(home, ".apm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".apm", "apm.yml"), []byte("name: test\nversion: 1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := &availExecutor{available: nil}
	a := New(configPath)
	a.SetFallbackExecutor(mock)
	return a, mock, home
}

func TestAgentsSyncAllRefusesToInstallWithoutAPM(t *testing.T) {
	a, mock, home := newSyncWithoutAPMApp(t)

	_, err := a.AgentsSyncAll(context.Background(), AgentsSyncAllOptions{})
	if err == nil || !strings.Contains(err.Error(), "omni doctor --fix") {
		t.Fatalf("error lacks install hint: %v", err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("apm invoked despite missing binary: %#v", mock.Calls)
	}
	if _, err := os.Stat(filepath.Join(home, ".apm", "apm.yml")); err != nil {
		t.Fatalf("existing manifest changed: %v", err)
	}
}

func TestAgentsSyncAllInstallsPinnedAPMFirst(t *testing.T) {
	a, _, _ := newSyncWithoutAPMApp(t)
	mock := &flipExecutor{availExecutor: availExecutor{available: map[string]bool{"uv": true}}, makeAvailable: "apm"}
	mock.Responses = []executor.MockCall{
		{},
		{Stdout: "Agent Package Manager (APM) CLI version " + apmVersionPin + "\n"},
	}
	a.SetFallbackExecutor(mock)
	wheel := serveAPMWheel(t, a)

	// The rest of the sync runs against the mock; only the install ordering matters here.
	_, _ = a.AgentsSyncAll(context.Background(), AgentsSyncAllOptions{})
	if len(mock.Calls) == 0 {
		t.Fatal("sync never ran an installer")
	}
	first := mock.Calls[0]
	if got := strings.Join(append([]string{first.Name}, first.Args...), " "); got != "uv tool install "+wheel {
		t.Fatalf("first command = %q, want the pinned APM install", got)
	}
}

func TestAgentsSyncAllDryRunInstallsNothing(t *testing.T) {
	a, _, _ := newSyncWithoutAPMApp(t)
	mock := &flipExecutor{availExecutor: availExecutor{available: map[string]bool{"uv": true}}, makeAvailable: "apm"}
	a.SetFallbackExecutor(mock)

	_, _ = a.AgentsSyncAll(context.Background(), AgentsSyncAllOptions{DryRun: true})
	for _, call := range mock.Calls {
		if call.Name == "uv" {
			t.Fatalf("dry run installed apm: %#v", call)
		}
	}
}
