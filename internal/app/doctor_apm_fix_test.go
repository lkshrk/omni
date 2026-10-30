package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
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
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	mock.Responses = pinnedBuildResponses()
	wheel, hits := serveAPMWheelBody(t, a, []byte("not served"), []byte("cached"))
	if err := os.MkdirAll(filepath.Dir(wheel), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wheel, []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || !strings.HasPrefix(report.Installed, "apm "+apmVersionPin+" into ") {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if hits.Load() != 0 {
		t.Fatalf("downloaded %d times despite a verified cached wheel", hits.Load())
	}
	if got := mock.Calls[1].Args[len(mock.Calls[1].Args)-1]; got != wheel {
		t.Fatalf("installed %q, want the verified cached wheel %q", got, wheel)
	}
}

func TestFixMissingAPMReplacesACorruptCachedWheel(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	mock.Responses = pinnedBuildResponses()
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

func TestFixMissingAPMDownloadsNothingWithoutABuilder(t *testing.T) {
	a, _ := newPrivateAPMApp(t, nil)
	_, hits := serveAPMWheelBody(t, a, []byte("wheel"), []byte("wheel"))
	if _, err := a.FixMissingAPM(context.Background(), false); err == nil {
		t.Fatal("expected the no-builder error")
	}
	if hits.Load() != 0 {
		t.Fatalf("downloaded the wheel %d times with no builder to use it", hits.Load())
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

func TestFixMissingAPMNoopWhenTheCopyIsPinned(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	serveAPMWheel(t, a)
	writeCompleteAPMCopy(t, a)
	mock.Responses = []executor.MockCall{{Stdout: "APM CLI version " + apmVersionPin + "\n"}}
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || !report.AlreadyInstalled {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("calls = %#v, want only the version probe", mock.Calls)
	}
}

func TestFixMissingAPMRebuildsACopyThatReportsAnotherVersion(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	serveAPMWheel(t, a)
	writeCompleteAPMCopy(t, a)
	mock.Responses = append([]executor.MockCall{{Stdout: "APM CLI version 0.27.0\n"}}, pinnedBuildResponses()...)
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || report.Installed == "" {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if got := mock.Calls[1]; got.Name != "uv" || got.Args[0] != "venv" {
		t.Fatalf("second call = %#v, want the rebuild", got)
	}
}

func TestFixMissingAPMDryRunPlansWithoutRunning(t *testing.T) {
	a, mock := newPrivateAPMApp(t, map[string]bool{"uv": true})
	report, err := a.FixMissingAPM(context.Background(), true)
	if err != nil || !strings.HasSuffix(report.Planned, " with uv") || !strings.Contains(report.Planned, apmVersionPin) {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("dry run ran commands: %#v", mock.Calls)
	}
}

func TestFixMissingAPMNeverInstallsOverAnOverride(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"apm": true, "uv": true})
	t.Setenv(apmOverrideEnv, "apm")
	mock.Responses = []executor.MockCall{{Stdout: "APM CLI version 0.27.0\n"}}
	_, err := a.FixMissingAPM(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "does not install over an override") {
		t.Fatalf("err = %v", err)
	}
	if len(mock.Calls) != 1 || mock.Calls[0].Name != "apm" {
		t.Fatalf("calls = %#v, want only the override's version probe", mock.Calls)
	}
}

func TestFixMissingAPMAcceptsAPinnedOverride(t *testing.T) {
	a, mock := newAPMFixApp(t, map[string]bool{"apm": true})
	t.Setenv(apmOverrideEnv, "apm")
	mock.Responses = []executor.MockCall{{Stdout: "APM CLI version " + apmVersionPin + "\n"}}
	report, err := a.FixMissingAPM(context.Background(), false)
	if err != nil || !report.AlreadyInstalled {
		t.Fatalf("report = %#v, err = %v", report, err)
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
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv(apmOverrideEnv, "")
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

func TestAgentsSyncAllRefusesToRunWithoutABuilder(t *testing.T) {
	a, mock, home := newSyncWithoutAPMApp(t)

	_, err := a.AgentsSyncAll(context.Background(), AgentsSyncAllOptions{})
	if err == nil || !strings.Contains(err.Error(), "omni doctor --fix") {
		t.Fatalf("error lacks install hint: %v", err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("commands ran without apm: %#v", mock.Calls)
	}
	if _, err := os.Stat(filepath.Join(home, ".apm", "apm.yml")); err != nil {
		t.Fatalf("existing manifest changed: %v", err)
	}
}

func TestAgentsSyncAllProvisionsPinnedAPMFirst(t *testing.T) {
	a, _, _ := newSyncWithoutAPMApp(t)
	mock := &venvExecutor{availExecutor: availExecutor{available: map[string]bool{"uv": true}}}
	mock.Responses = pinnedBuildResponses()
	a.SetFallbackExecutor(mock)
	serveAPMWheel(t, a)

	// The rest of the sync runs against the mock; only the provisioning order matters here.
	_, _ = a.AgentsSyncAll(context.Background(), AgentsSyncAllOptions{})
	if len(mock.Calls) < 3 {
		t.Fatalf("calls = %#v, want the provisioning steps", mock.Calls)
	}
	if first := mock.Calls[0]; first.Name != "uv" || first.Args[0] != "venv" {
		t.Fatalf("first command = %#v, want omni's APM copy built first", first)
	}
}

func TestAgentsSyncAllDryRunInstallsNothing(t *testing.T) {
	a, _, _ := newSyncWithoutAPMApp(t)
	mock := &venvExecutor{availExecutor: availExecutor{available: map[string]bool{"uv": true}}}
	a.SetFallbackExecutor(mock)

	_, _ = a.AgentsSyncAll(context.Background(), AgentsSyncAllOptions{DryRun: true})
	for _, call := range mock.Calls {
		if call.Name == "uv" || call.Name == "python3" {
			t.Fatalf("dry run installed apm: %#v", call)
		}
	}
}

func TestAgentsSyncAllDryRunWithoutACopyReportsItAndRunsNothing(t *testing.T) {
	a, _, _ := newSyncWithoutAPMApp(t)
	mock := &venvExecutor{availExecutor: availExecutor{available: map[string]bool{"uv": true}}}
	a.SetFallbackExecutor(mock)
	_, err := a.AgentsSyncAll(context.Background(), AgentsSyncAllOptions{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "not installed yet") {
		t.Fatalf("err = %v, want the missing-copy message", err)
	}
	if len(mock.Calls) != 0 {
		t.Fatalf("dry run ran commands: %#v", mock.Calls)
	}
}
