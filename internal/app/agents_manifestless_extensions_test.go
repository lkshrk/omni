package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestlessExtensionRejectsMalformedBundleManifest(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest string
		base     string
	}{
		{"array with designated source", `[]`, ""},
		{"null with designated source", `null`, ""},
		{"scalar omp with valid pi", `{"omp":"invalid","pi":{"extensions":[".omp/extensions/main.ts"]}}`, ""},
		{"nested array", `[]`, ".omp/extensions/nested"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			module := filepath.Join(root, "package")
			for rel, contents := range map[string]string{
				filepath.Join(test.base, "package.json"): test.manifest,
				".omp/extensions/main.ts":                "export default function () {}\n",
			} {
				filename := filepath.Join(module, rel)
				if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, ok := manifestlessExtensionEvidence(root, module, false); ok {
				t.Fatal("accepted an install-invalid native bundle manifest")
			}
		})
	}
}

func TestManifestlessExtensionNativeNameUniqueness(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest string
		entries  []string
		want     bool
	}{
		{"explicit duplicate stems", `{"omp":{"extensions":["first/run.ts","second/run.js"]}}`, []string{"first/run.ts", "second/run.js"}, false},
		{"explicit duplicate directory names", `{"omp":{"extensions":["first/run","second/run"]}}`, []string{"first/run/index.ts", "second/run/index.js"}, false},
		{"index and loose file collision", `{"omp":{"extensions":["feature","feature.ts"]}}`, []string{"feature/index.ts", "feature.ts"}, false},
		{"root index uses package name", `{"omp":{"extensions":["index.ts","package.js"]}}`, []string{"index.ts", "package.js"}, false},
		{"designated duplicate stems", "", []string{".omp/extensions/run.ts", ".pi/extensions/run.js"}, false},
		{"designated index collision", "", []string{".omp/extensions/feature/index.ts", ".apm/extensions/feature.js"}, false},
		{"repeated resolved entry deduplicated", `{"omp":{"extensions":["feature","feature/index.ts","./feature/index.ts"]}}`, []string{"feature/index.ts"}, true},
		{"distinct native names", `{"omp":{"extensions":["first.ts","second.js"]}}`, []string{"first.ts", "second.js"}, true},
		{"reserved user factory stem", `{"omp":{"extensions":["apm-hooks-user.ts"]}}`, []string{"apm-hooks-user.ts"}, false},
		{"reserved project index identity", `{"omp":{"extensions":["apm-hooks-project/index.ts"]}}`, []string{"apm-hooks-project/index.ts"}, false},
		{"reserved designated factory", "", []string{".omp/extensions/apm-hooks-user.js"}, false},
		{"old name is not a reserved alias", `{"omp":{"extensions":["apm-hooks.ts"]}}`, []string{"apm-hooks.ts"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			module := filepath.Join(root, "package")
			if err := os.MkdirAll(module, 0o700); err != nil {
				t.Fatal(err)
			}
			if test.manifest != "" {
				if err := os.WriteFile(filepath.Join(module, "package.json"), []byte(test.manifest), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, entry := range test.entries {
				filename := filepath.Join(module, filepath.FromSlash(entry))
				if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte("export default function () {}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, declared, ok := manifestlessExtensionEvidence(root, module, false); ok != test.want || !declared {
				t.Fatalf("extension evidence declared=%v valid=%v; want declared=true valid=%v", declared, ok, test.want)
			}
		})
	}
}

func TestManifestlessExtensionHashesOnlyDeclarationsAndEntries(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "package")
	for _, rel := range []string{
		".omp/extensions/main.ts",
		".omp/extensions/resource.bin",
		".omp/extensions/explicit-only.mjs",
		".pi/extensions",
		".apm/extensions/deployed/.apm-extension.json",
	} {
		filename := filepath.Join(module, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte("source or incidental resource\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	identities, declared, ok := manifestlessExtensionEvidence(root, module, false)
	if !ok || !declared {
		t.Fatalf("extension evidence declared=%v valid=%v; want both true", declared, ok)
	}
	files := 0
	for _, identity := range identities {
		if identity.Absent || identity.Directory {
			continue
		}
		files++
		if identity.RelativePath == ".omp/extensions/main.ts" {
			if identity.Hash == "" || identity.PathOnly {
				t.Fatal("selected entry lacks a content fingerprint")
			}
		} else if identity.Hash != "" || !identity.PathOnly {
			t.Errorf("incidental file %q was content-fingerprinted", identity.RelativePath)
		}
	}
	if files != 5 {
		t.Fatalf("recorded %d files, want all five examined paths", files)
	}
}

func TestManifestlessExtensionRootObjectPrecedence(t *testing.T) {
	for _, test := range []struct {
		name       string
		manifest   string
		designated bool
		declared   bool
		valid      bool
	}{
		{"empty omp suppresses valid pi", `{"omp":{},"pi":{"extensions":["main.ts"]}}`, false, false, true},
		{"empty omp suppresses missing pi entry", `{"omp":{},"pi":{"extensions":["missing.ts"]}}`, false, false, true},
		{"empty omp suppresses malformed pi extensions", `{"omp":{},"pi":{"extensions":42}}`, false, false, true},
		{"null omp permits pi", `{"omp":null,"pi":{"extensions":["main.ts"]}}`, false, true, true},
		{"omp object permits designated fallback", `{"omp":{"name":"demo"},"pi":{"extensions":42}}`, true, true, true},
		{"scalar omp rejects valid pi", `{"omp":false,"pi":{"extensions":["main.ts"]}}`, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			module := filepath.Join(root, "package")
			writeFile(t, filepath.Join(module, "package.json"), test.manifest)
			writeFile(t, filepath.Join(module, "main.ts"), manifestlessNativeEntry)
			if test.designated {
				writeFile(t, filepath.Join(module, ".omp", "extensions", "main.ts"), manifestlessNativeEntry)
			}
			_, declared, valid := manifestlessExtensionEvidence(root, module, false)
			if valid != test.valid || (valid && declared != test.declared) {
				t.Fatalf("extension evidence declared=%v valid=%v; want declared=%v valid=%v", declared, valid, test.declared, test.valid)
			}
		})
	}
}

func TestManifestlessExtensionParkedMissingDeclarations(t *testing.T) {
	for _, base := range []string{"", ".omp/extensions/nested"} {
		for _, entry := range []string{"dist/index.js", "dist"} {
			for _, existingDir := range []bool{false, true} {
				t.Run(base+"/"+entry+"/existing-dir="+fmt.Sprint(existingDir), func(t *testing.T) {
					root := t.TempDir()
					module := filepath.Join(root, "package")
					writeFile(t, filepath.Join(module, base, "package.json"), fmt.Sprintf(`{"omp":{"extensions":[%q]}}`, entry))
					if existingDir {
						if err := os.MkdirAll(filepath.Join(module, base, "dist"), 0o700); err != nil {
							t.Fatal(err)
						}
					}
					for _, allowMissing := range []bool{false, true} {
						_, declared, valid := manifestlessExtensionEvidence(root, module, allowMissing)
						if valid != allowMissing || (valid && !declared) {
							t.Fatalf("allowMissing=%v: declared=%v valid=%v", allowMissing, declared, valid)
						}
					}
				})
			}
		}
	}
}

func TestManifestlessExtensionParkedRejectsMalformedPaths(t *testing.T) {
	for _, entry := range []string{"../outside.ts", "dist/../../outside.ts", `dist\index.js`, "/tmp/absent-extension.js"} {
		t.Run(entry, func(t *testing.T) {
			root := t.TempDir()
			module := filepath.Join(root, "package")
			writeFile(t, filepath.Join(module, "package.json"), fmt.Sprintf(`{"omp":{"extensions":[%q]}}`, entry))
			if _, _, valid := manifestlessExtensionEvidence(root, module, true); valid {
				t.Fatal("parked evidence accepted malformed declaration")
			}
		})
	}
}
