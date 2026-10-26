package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// apmWheel is the verified fork build Omni installs: where to fetch it, its sha256, and the wheel file name
// that installers parse the version out of.
type apmWheel struct {
	URL    string
	SHA256 string
	File   string
}

var pinnedAPMWheel = mustParseAPMWheel(apmPackagePin)

func mustParseAPMWheel(pin string) apmWheel {
	wheel, err := parseAPMWheel(pin)
	if err != nil {
		panic(err)
	}
	return wheel
}

// parseAPMWheel splits a pip direct URL of the form https://host/name.whl#sha256=<hex>.
func parseAPMWheel(pin string) (apmWheel, error) {
	u, err := url.Parse(pin)
	if err != nil {
		return apmWheel{}, fmt.Errorf("apm wheel pin %q: %w", pin, err)
	}
	if u.Scheme != "https" {
		return apmWheel{}, fmt.Errorf("apm wheel pin %q must be an https URL", pin)
	}
	digest, ok := strings.CutPrefix(u.Fragment, "sha256=")
	if !ok {
		return apmWheel{}, fmt.Errorf("apm wheel pin %q lacks a #sha256= fragment", pin)
	}
	if raw, err := hex.DecodeString(digest); err != nil || len(raw) != 32 {
		return apmWheel{}, fmt.Errorf("apm wheel pin %q: sha256 must be 64 hex characters", pin)
	}
	file := path.Base(u.Path)
	if !strings.HasSuffix(file, ".whl") {
		return apmWheel{}, fmt.Errorf("apm wheel pin %q must name a .whl file", pin)
	}
	u.Fragment = ""
	return apmWheel{URL: u.String(), SHA256: strings.ToLower(digest), File: file}, nil
}

func (a *App) apmWheel() apmWheel {
	if a.apmWheelOverride != nil {
		return *a.apmWheelOverride
	}
	return pinnedAPMWheel
}

func (a *App) apmWheelPath() (string, error) {
	root, err := a.cacheRoot()
	if err != nil {
		return "", fmt.Errorf("resolving apm wheel cache: %w", err)
	}
	return filepath.Join(root, "apm", a.apmWheel().File), nil
}

// ensureAPMWheel returns a local wheel whose bytes match the pinned digest. A cached copy that still matches is
// reused, so reinstalling needs no network; anything else is downloaded and refused unless it verifies.
func (a *App) ensureAPMWheel(ctx context.Context) (string, error) {
	wheel := a.apmWheel()
	dest, err := a.apmWheelPath()
	if err != nil {
		return "", err
	}
	if verifyFileChecksum(dest, wheel.SHA256, apmDistribution) == nil {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", fmt.Errorf("creating apm wheel cache: %w", err)
	}
	if err := a.downloadFallbackAsset(ctx, apmDistribution, wheel.URL, dest); err != nil {
		return "", err
	}
	if err := verifyFileChecksum(dest, wheel.SHA256, apmDistribution); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return dest, nil
}
