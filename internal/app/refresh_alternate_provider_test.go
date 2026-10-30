package app_test

import (
	"context"
	"testing"

	"github.com/lkshrk/omni/internal/app"
	"github.com/lkshrk/omni/internal/config"
	"github.com/lkshrk/omni/internal/database"
)

func TestRefreshInstalled_DetectsAlternateConfiguredProvider(t *testing.T) {
	t.Parallel()
	brew := &bulkCheckingStub{
		stubProvider: stubProvider{name: "brew", available: true},
		bulk:         map[string]string{},
	}
	bun := &bulkCheckingStub{
		stubProvider: stubProvider{name: "bun", available: true},
		bulk:         map[string]string{"pnpm": "11.11.0"},
	}
	a, cfgPath := newImportApp(t, brew, bun)
	ctx := context.Background()

	if err := saveAppConfig(t, cfgPath, &config.RootConfig{
		Tools: map[string]config.ToolSpec{
			"pnpm": {
				Providers: []config.ToolInstallSpec{
					{Provider: "bun", Package: "pnpm"},
					{Provider: "brew", Package: "pnpm"},
				},
			},
		},
		Groups: []*config.GroupConfig{testHostToolGroup("pnpm")},
	}); err != nil {
		t.Fatalf("saving config: %v", err)
	}
	if err := a.SaveSettings(ctx, config.Settings{
		ProviderPriority: []string{"brew", "bun", "pnpm", "npm"},
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if err := a.DB().UpsertPackageAvailability(ctx, database.PackageAvailability{
		Name: "pnpm", Provider: "brew", Package: "pnpm", Available: true,
	}); err != nil {
		t.Fatalf("UpsertPackageAvailability: %v", err)
	}

	if err := a.RefreshInstalled(ctx, nil); err != nil {
		t.Fatalf("RefreshInstalled: %v", err)
	}

	all, listErr := a.DB().List(ctx)
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(all) != 1 || all[0].Name != "pnpm" {
		t.Fatalf("cached rows = %+v", all)
	}
	got := all[0]
	if got.Provider != "brew" {
		t.Fatalf("resolved provider = %q, want brew (row=%+v)", got.Provider, got)
	}
	if !got.Installed || got.InstalledWith != "bun" || got.Version.String != "11.11.0" {
		t.Fatalf("cache = installed:%v owner:%q version:%q, want true/bun/11.11.0", got.Installed, got.InstalledWith, got.Version.String)
	}

	class := app.ClassifyToolView(viewFromCache(got), app.ToolClassificationContext{})
	if class.SyncStatus != app.ToolSyncWrongProvider {
		t.Fatalf("sync status = %q, want wrong-provider", class.SyncStatus)
	}
}

func TestRefreshInstalled_StaleCachedOwnerFallsThroughToAlternateProvider(t *testing.T) {
	t.Parallel()
	brew := &bulkCheckingStub{
		stubProvider: stubProvider{name: "brew", available: true},
		bulk:         map[string]string{},
	}
	bun := &bulkCheckingStub{
		stubProvider: stubProvider{name: "bun", available: true},
		bulk:         map[string]string{"pnpm": "11.11.0"},
	}
	a, cfgPath := newImportApp(t, brew, bun)
	ctx := context.Background()

	if err := saveAppConfig(t, cfgPath, &config.RootConfig{
		Tools: map[string]config.ToolSpec{
			"pnpm": {
				Providers: []config.ToolInstallSpec{
					{Provider: "bun", Package: "pnpm"},
					{Provider: "brew", Package: "pnpm"},
				},
			},
		},
		Groups: []*config.GroupConfig{testHostToolGroup("pnpm")},
	}); err != nil {
		t.Fatalf("saving config: %v", err)
	}
	if err := a.SaveSettings(ctx, config.Settings{
		ProviderPriority: []string{"brew", "bun", "pnpm", "npm"},
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if err := a.DB().Upsert(ctx, &database.ToolCache{
		Name:          "pnpm",
		Provider:      "brew",
		Package:       "pnpm",
		Installed:     false,
		InstalledWith: "bun",
	}); err != nil {
		t.Fatalf("Upsert stale cache: %v", err)
	}

	if err := a.RefreshInstalled(ctx, nil); err != nil {
		t.Fatalf("RefreshInstalled: %v", err)
	}

	got, err := a.DB().Get(ctx, "pnpm", "brew", "pnpm")
	if err != nil {
		t.Fatalf("Get pnpm: %v", err)
	}
	if !got.Installed || got.InstalledWith != "bun" || got.Version.String != "11.11.0" {
		t.Fatalf("cache = installed:%v owner:%q version:%q, want true/bun/11.11.0", got.Installed, got.InstalledWith, got.Version.String)
	}
}

func TestRefreshProviderInstalled_DetectsAlternateConfiguredProvider(t *testing.T) {
	t.Parallel()
	brew := &bulkCheckingStub{
		stubProvider: stubProvider{name: "brew", available: true},
		bulk:         map[string]string{},
	}
	bun := &bulkCheckingStub{
		stubProvider: stubProvider{name: "bun", available: true},
		bulk:         map[string]string{"pnpm": "11.11.0"},
	}
	a, cfgPath := newImportApp(t, brew, bun)
	ctx := context.Background()

	if err := saveAppConfig(t, cfgPath, &config.RootConfig{
		Tools: map[string]config.ToolSpec{
			"pnpm": {
				Providers: []config.ToolInstallSpec{
					{Provider: "bun", Package: "pnpm"},
					{Provider: "brew", Package: "pnpm"},
				},
			},
		},
		Groups: []*config.GroupConfig{testHostToolGroup("pnpm")},
	}); err != nil {
		t.Fatalf("saving config: %v", err)
	}
	if err := a.SaveSettings(ctx, config.Settings{
		ProviderPriority: []string{"brew", "bun", "pnpm", "npm"},
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	if err := a.RefreshProviderInstalled(ctx, "brew"); err != nil {
		t.Fatalf("RefreshProviderInstalled brew: %v", err)
	}

	got, err := a.DB().Get(ctx, "pnpm", "brew", "pnpm")
	if err != nil {
		t.Fatalf("Get pnpm: %v", err)
	}
	if !got.Installed || got.InstalledWith != "bun" || got.Version.String != "11.11.0" {
		t.Fatalf("cache = installed:%v owner:%q version:%q, want true/bun/11.11.0", got.Installed, got.InstalledWith, got.Version.String)
	}
}

func TestRefreshInstalled_SiblingNodeManagerInstallIsNotMissing(t *testing.T) {
	t.Parallel()
	npm := &bulkCheckingStub{
		stubProvider: stubProvider{name: "npm", available: true},
		bulk:         map[string]string{},
	}
	bun := &bulkCheckingStub{
		stubProvider: stubProvider{name: "bun", available: true},
		bulk:         map[string]string{"@devcontainers/cli": "0.89.0"},
	}
	a, cfgPath := newImportApp(t, npm, bun)
	ctx := context.Background()

	if err := saveAppConfig(t, cfgPath, &config.RootConfig{
		Tools: map[string]config.ToolSpec{
			"@devcontainers/cli": {Providers: []config.ToolInstallSpec{{Provider: "npm", Package: "@devcontainers/cli"}}},
		},
		Groups: []*config.GroupConfig{testHostToolGroup("@devcontainers/cli")},
	}); err != nil {
		t.Fatalf("saving config: %v", err)
	}
	if err := a.SaveSettings(ctx, config.Settings{ProviderPriority: []string{"bun", "pnpm", "npm"}}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	if err := a.RefreshInstalled(ctx, nil); err != nil {
		t.Fatalf("RefreshInstalled: %v", err)
	}

	all, err := a.DB().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("cached rows = %+v", all)
	}
	got := all[0]
	if got.Provider != "npm" || !got.Installed || got.InstalledWith != "bun" || got.Version.String != "0.89.0" {
		t.Fatalf("cache = provider:%q installed:%v owner:%q version:%q, want npm/true/bun/0.89.0", got.Provider, got.Installed, got.InstalledWith, got.Version.String)
	}
	class := app.ClassifyToolView(viewFromCache(got), app.ToolClassificationContext{})
	if class.SyncStatus != app.ToolSyncWrongProvider {
		t.Fatalf("sync status = %q, want wrong-provider", class.SyncStatus)
	}
}

func TestRefreshInstalled_SystemManagersDoNotShareInstalls(t *testing.T) {
	t.Parallel()
	pacman := &bulkCheckingStub{
		stubProvider: stubProvider{name: "pacman", available: true},
		bulk:         map[string]string{},
	}
	brew := &bulkCheckingStub{
		stubProvider: stubProvider{name: "brew", available: true},
		bulk:         map[string]string{"ripgrep": "14.0.0"},
	}
	a, cfgPath := newImportApp(t, pacman, brew)
	ctx := context.Background()

	if err := saveAppConfig(t, cfgPath, &config.RootConfig{
		Tools: map[string]config.ToolSpec{
			"ripgrep": {Providers: []config.ToolInstallSpec{{Provider: "pacman", Package: "ripgrep"}}},
		},
		Groups: []*config.GroupConfig{testHostToolGroup("ripgrep")},
	}); err != nil {
		t.Fatalf("saving config: %v", err)
	}

	if err := a.RefreshInstalled(ctx, nil); err != nil {
		t.Fatalf("RefreshInstalled: %v", err)
	}

	all, err := a.DB().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].Installed {
		t.Fatalf("cached rows = %+v, want one missing row", all)
	}
}

func TestRefreshInstalled_UnavailableSiblingManagerIsNotProbed(t *testing.T) {
	t.Parallel()
	npm := &bulkCheckingStub{
		stubProvider: stubProvider{name: "npm", available: true},
		bulk:         map[string]string{},
	}
	pnpm := &bulkCheckingStub{
		stubProvider: stubProvider{name: "pnpm", available: false},
		bulk:         map[string]string{"@devcontainers/cli": "0.89.0"},
	}
	a, cfgPath := newImportApp(t, npm, pnpm)
	ctx := context.Background()

	if err := saveAppConfig(t, cfgPath, &config.RootConfig{
		Tools: map[string]config.ToolSpec{
			"@devcontainers/cli": {Providers: []config.ToolInstallSpec{{Provider: "npm", Package: "@devcontainers/cli"}}},
		},
		Groups: []*config.GroupConfig{testHostToolGroup("@devcontainers/cli")},
	}); err != nil {
		t.Fatalf("saving config: %v", err)
	}

	if err := a.RefreshInstalled(ctx, nil); err != nil {
		t.Fatalf("RefreshInstalled: %v", err)
	}

	all, err := a.DB().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].Installed {
		t.Fatalf("cached rows = %+v, want one missing row", all)
	}
}

type availabilityCountingStub struct {
	bulkCheckingStub
	availableCalls int
}

func (s *availabilityCountingStub) Available(ctx context.Context) (bool, error) {
	s.availableCalls++
	return s.bulkCheckingStub.Available(ctx)
}

func TestRefreshInstalled_SiblingAvailabilityCheckedOncePerRefresh(t *testing.T) {
	t.Parallel()
	npm := &bulkCheckingStub{
		stubProvider: stubProvider{name: "npm", available: true},
		bulk:         map[string]string{},
	}
	pnpm := &availabilityCountingStub{bulkCheckingStub: bulkCheckingStub{
		stubProvider: stubProvider{name: "pnpm", available: false},
		bulk:         map[string]string{},
	}}
	a, cfgPath := newImportApp(t, npm, pnpm)
	ctx := context.Background()

	names := []string{"@devcontainers/cli", "prettier", "typescript"}
	tools := make(map[string]config.ToolSpec, len(names))
	for _, name := range names {
		tools[name] = config.ToolSpec{Providers: []config.ToolInstallSpec{{Provider: "npm", Package: name}}}
	}
	if err := saveAppConfig(t, cfgPath, &config.RootConfig{
		Tools:  tools,
		Groups: []*config.GroupConfig{testHostToolGroup(names...)},
	}); err != nil {
		t.Fatalf("saving config: %v", err)
	}

	if err := a.RefreshInstalled(ctx, nil); err != nil {
		t.Fatalf("RefreshInstalled: %v", err)
	}
	// One call from the refresh's provider scan, one from the sibling lookup, regardless of how many tools are missing.
	if pnpm.availableCalls != 2 {
		t.Fatalf("pnpm Available calls = %d, want 2 for %d missing tools", pnpm.availableCalls, len(names))
	}
}
