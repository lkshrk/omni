package app

import (
	"context"
	"fmt"
)

type APMInstallFixReport struct {
	AlreadyInstalled bool
	Planned          string
	Installed        string
}

func (a *App) FixMissingAPM(ctx context.Context, dryRun bool) (APMInstallFixReport, error) {
	if override := apmOverride(); override != "" {
		if !a.overrideAvailable(override) {
			return APMInstallFixReport{}, errAPMOverrideMissing(override)
		}
		version, err := a.APMVersion(ctx)
		if err != nil {
			return APMInstallFixReport{}, err
		}
		if !apmVersionPinned(version) {
			return APMInstallFixReport{}, fmt.Errorf("%s=%s is apm %s, not the pinned %s; omni does not install over an override",
				apmOverrideEnv, override, version, apmVersionPin)
		}
		return APMInstallFixReport{AlreadyInstalled: true}, nil
	}
	p, err := privateAPMCopy()
	if err != nil {
		return APMInstallFixReport{}, err
	}
	if p.complete(a.apmWheel().SHA256) {
		if version, err := a.APMVersion(ctx); err == nil && apmVersionPinned(version) {
			return APMInstallFixReport{AlreadyInstalled: true}, nil
		}
	}
	if dryRun {
		builder, err := a.apmBuilderFor(ctx)
		if err != nil {
			return APMInstallFixReport{}, err
		}
		return APMInstallFixReport{Planned: fmt.Sprintf("apm %s into %s with %s", apmVersionPin, p.Dir, builder.name)}, nil
	}
	if _, err := a.provisionPrivateAPM(ctx, true); err != nil {
		return APMInstallFixReport{}, err
	}
	a.forgetAPMProbes()
	return APMInstallFixReport{Installed: fmt.Sprintf("apm %s into %s", apmVersionPin, p.Dir)}, nil
}
