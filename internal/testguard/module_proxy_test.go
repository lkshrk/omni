package testguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParentModuleProxyIsEmptyWithoutAPopulatedCache(t *testing.T) {
	previous := parentModuleDownload
	t.Cleanup(func() { parentModuleDownload = previous })

	parentModuleDownload = ""
	if got := ParentModuleProxy(); got != "" {
		t.Fatalf("proxy with no parent sandbox = %q", got)
	}
	parentModuleDownload = filepath.Join(t.TempDir(), "missing", "download")
	if got := ParentModuleProxy(); got != "" {
		t.Fatalf("proxy for a missing cache = %q", got)
	}
	file := filepath.Join(t.TempDir(), "download")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	parentModuleDownload = file
	if got := ParentModuleProxy(); got != "" {
		t.Fatalf("proxy for a regular file = %q", got)
	}
}

func TestParentModuleProxyServesAPopulatedCacheAsAFileURL(t *testing.T) {
	previous := parentModuleDownload
	t.Cleanup(func() { parentModuleDownload = previous })

	dir := filepath.Join(t.TempDir(), "cache", "download")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	parentModuleDownload = dir
	got := ParentModuleProxy()
	if !strings.HasPrefix(got, "file:///") || strings.Contains(got, `\`) {
		t.Fatalf("proxy is not a well-formed file URL: %q", got)
	}
	if !strings.HasSuffix(got, filepath.ToSlash(dir)) {
		t.Fatalf("proxy %q does not point at %q", got, dir)
	}
}
