package app

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeSafetyBundlePrefix(root string) string {
	return root + "/extensions/" + fmt.Sprintf("package-%x", sha256.Sum256([]byte("acme/demo"))) + "/"
}

func nativeSafetyFixture(t *testing.T, packageType string, files map[string]string, payloads ...string) manifestlessSkillFixture {
	t.Helper()
	t.Setenv("PI_CODING_AGENT_DIR", "")
	prefix := nativeSafetyBundlePrefix(".omp/agent")
	deployed := []string{prefix + "package.json", prefix + ".apm-extension.json"}
	for _, payload := range payloads {
		deployed = append(deployed, prefix+payload)
	}
	f := newManifestlessSkillFixture(t, "acme/demo", "", packageType, deployed)
	for relative, content := range files {
		writeFile(t, filepath.Join(f.module, filepath.FromSlash(relative)), content)
	}
	return f
}

func nativeSafetyDeclaredFiles() map[string]string {
	return map[string]string{
		"package.json": `{"omp":{"extensions":["index.ts"]}}`,
		"index.ts":     "export default function () {}\n",
	}
}

func TestManifestlessNativeAcceptedExtensionDeclarations(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		entry string
	}{
		{"omp root", nativeSafetyDeclaredFiles(), "index.ts"},
		{"pi root", map[string]string{"package.json": `{"pi":{"extensions":["main.js"]}}`, "main.js": "export default () => {};"}, "main.js"},
		{"explicit mjs", map[string]string{"package.json": `{"omp":{"extensions":["main.mjs"]}}`, "main.mjs": "export default () => {};"}, "main.mjs"},
		{"explicit cjs", map[string]string{"package.json": `{"pi":{"extensions":["main.cjs"]}}`, "main.cjs": "module.exports = () => {};"}, "main.cjs"},
		{"directory ts index", map[string]string{"package.json": `{"omp":{"extensions":["src"]}}`, "src/index.ts": "export default () => {};"}, "src/index.ts"},
		{"directory js index", map[string]string{"package.json": `{"pi":{"extensions":["./src/"]}}`, "src/index.js": "export default () => {};"}, "src/index.js"},
		{"root directory index", map[string]string{"package.json": `{"omp":{"extensions":["."]}}`, "index.ts": "export default () => {};"}, "index.ts"},
	}
	for _, dir := range []string{".omp/extensions", ".pi/extensions", ".apm/extensions"} {
		for _, ext := range []string{"ts", "js"} {
			entry := dir + "/loose." + ext
			tests = append(tests, struct {
				name  string
				files map[string]string
				entry string
			}{dir + " loose " + ext, map[string]string{entry: "export default () => {};"}, entry})
			entry = dir + "/nested/index." + ext
			tests = append(tests, struct {
				name  string
				files map[string]string
				entry string
			}{dir + " nested index " + ext, map[string]string{entry: "export default () => {};"}, entry})
		}
		for _, namespace := range []string{"omp", "pi"} {
			entry := dir + "/nested/src/main.mjs"
			tests = append(tests, struct {
				name  string
				files map[string]string
				entry string
			}{dir + " nested " + namespace, map[string]string{dir + "/nested/package.json": `{"` + namespace + `":{"extensions":["src/main.mjs"]}}`, entry: "export default () => {};"}, entry})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := nativeSafetyFixture(t, "extension_package", tt.files, tt.entry)
			requireManifestlessKnownEmpty(t, f)
		})
	}
}

func TestManifestlessNativeAcceptsSkillMix(t *testing.T) {
	for _, packageType := range []string{"skill_bundle", "claude_skill"} {
		t.Run(packageType, func(t *testing.T) {
			f := nativeSafetyFixture(t, packageType, nativeSafetyDeclaredFiles(), "index.ts")
			if packageType == "claude_skill" {
				writeSkillManifest(t, f.module, "SKILL.md")
			} else {
				writeSkillManifest(t, f.module, "skills/demo/SKILL.md")
			}
			f.rows[0].lockEvidence.DeployedFiles = append(f.rows[0].lockEvidence.DeployedFiles, ".agents/skills/demo/SKILL.md", ".claude/skills/demo/SKILL.md")
			requireManifestlessKnownEmpty(t, f)
		})
	}
}

// APM deploys a skill under every enabled target's skills root; a host with OpenCode enabled locks
// .config/opencode/skills/<name> next to .agents and .claude, and that must not make ownership unprovable.
func TestManifestlessNativeAcceptsSkillsDeployedUnderTargetSpecificRoots(t *testing.T) {
	for _, deployed := range []string{
		".config/opencode/skills/demo/SKILL.md",
		".kiro/skills/demo/SKILL.md",
		".grok/skills/demo/SKILL.md",
		".hermes/skills/demo/SKILL.md",
	} {
		t.Run(deployed, func(t *testing.T) {
			f := nativeSafetyFixture(t, "claude_skill", nativeSafetyDeclaredFiles(), "index.ts")
			writeSkillManifest(t, f.module, "SKILL.md")
			f.rows[0].lockEvidence.DeployedFiles = append(f.rows[0].lockEvidence.DeployedFiles, ".agents/skills/demo/SKILL.md", deployed)
			requireManifestlessKnownEmpty(t, f)
		})
	}
}

func TestManifestlessNativeRejectsSkillsOutsideTheCatalogRoots(t *testing.T) {
	for _, deployed := range []string{
		".cursor/skills/demo/SKILL.md",
		".config/opencode/commands/demo/SKILL.md",
		".config/opencode/skills/SKILL.md",
	} {
		t.Run(deployed, func(t *testing.T) {
			f := nativeSafetyFixture(t, "claude_skill", nativeSafetyDeclaredFiles(), "index.ts")
			writeSkillManifest(t, f.module, "SKILL.md")
			f.rows[0].lockEvidence.DeployedFiles = append(f.rows[0].lockEvidence.DeployedFiles, ".agents/skills/demo/SKILL.md", deployed)
			requireManifestlessUnavailable(t, f)
		})
	}
}

func TestManifestlessNativeAcceptsHookDeclarations(t *testing.T) {
	for _, dir := range []string{"hooks", ".apm/hooks"} {
		for name, declaration := range map[string]string{
			"flat":           `{"SessionStart":[{"command":"printf ready"}]}`,
			"wrapped nested": `{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"printf ready"}]}]}}`,
		} {
			t.Run(dir+"/"+name, func(t *testing.T) {
				t.Setenv("PI_CODING_AGENT_DIR", "")
				f := newManifestlessSkillFixture(t, "acme/demo", "", "hook_package", []string{".omp/agent/extensions/apm-hooks-user/index.ts", ".omp/agent/apm-hooks-runtime.json"})
				writeFile(t, filepath.Join(f.module, dir, "session.json"), declaration)
				requireManifestlessKnownEmpty(t, f)
			})
		}
	}
}

func TestManifestlessNativeMixedHookSourceWithNativeOutput(t *testing.T) {
	for _, packageType := range []string{"extension_package", "hook_package"} {
		t.Run(packageType, func(t *testing.T) {
			f := nativeSafetyFixture(t, packageType, nativeSafetyDeclaredFiles(), "index.ts")
			writeFile(t, filepath.Join(f.module, "hooks", "session.json"), `{"SessionStart":[{"command":"printf ready"}]}`)
			requireManifestlessKnownEmpty(t, f)
		})
	}
}

func TestManifestlessNativeRejectsUnprovenDeclarations(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{"arbitrary package", map[string]string{"package.json": `{"name":"demo"}`, "index.ts": "export default () => {};"}},
		{"arbitrary index", map[string]string{"index.ts": "export default () => {};"}},
		{"empty declaration", map[string]string{"package.json": `{"omp":{"extensions":[]}}`, "index.ts": ""}},
		{"null declaration", map[string]string{"package.json": `{"omp":{"extensions":null}}`, "index.ts": ""}},
		{"string declaration", map[string]string{"package.json": `{"omp":{"extensions":"index.ts"}}`, "index.ts": ""}},
		{"nonstring entry", map[string]string{"package.json": `{"omp":{"extensions":[42]}}`, "index.ts": ""}},
		{"empty entry", map[string]string{"package.json": `{"omp":{"extensions":[""]}}`, "index.ts": ""}},
		{"malformed json", map[string]string{"package.json": `{"omp":`, "index.ts": ""}},
		{"unbuilt entry", map[string]string{"package.json": `{"omp":{"extensions":["dist/index.js"]}}`, "index.ts": ""}},
		{"directory without index", map[string]string{"package.json": `{"omp":{"extensions":["src"]}}`, "src/main.ts": ""}},
		{"escaping entry", map[string]string{"package.json": `{"omp":{"extensions":["../index.ts"]}}`, "index.ts": ""}},
		{"absolute entry", map[string]string{"package.json": `{"omp":{"extensions":["/tmp/index.ts"]}}`, "index.ts": ""}},
		{"backslash entry", map[string]string{"package.json": `{"omp":{"extensions":["src\\index.ts"]}}`, "index.ts": ""}},
		{"invalid omp cannot fall through", map[string]string{"package.json": `{"omp":{"extensions":[]},"pi":{"extensions":["index.ts"]}}`, "index.ts": ""}},
		{"designated case collision", map[string]string{".omp/extensions/demo.ts": "", ".omp/extensions/Demo.ts": ""}},
		{"nested empty declaration", map[string]string{".pi/extensions/demo/package.json": `{"pi":{"extensions":[]}}`, ".pi/extensions/demo/index.ts": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := nativeSafetyFixture(t, "extension_package", tt.files)
			requireManifestlessUnavailable(t, f)
		})
	}
}

func TestManifestlessNativeRejectsUnsafeSourcePaths(t *testing.T) {
	for _, relative := range []string{"package.json", "index.ts", ".omp/extensions", ".pi/extensions/demo", ".apm/extensions/demo.ts", "hooks/session.json", ".apm/hooks"} {
		for _, targetKind := range []string{"file", "directory", "missing"} {
			t.Run(relative+"/"+targetKind, func(t *testing.T) {
				files := nativeSafetyDeclaredFiles()
				delete(files, relative)
				if strings.Contains(relative, "/extensions") {
					delete(files, "package.json")
					files[".omp/extensions/safe.ts"] = "export default () => {};"
					if relative == ".omp/extensions" {
						delete(files, ".omp/extensions/safe.ts")
						files[".pi/extensions/safe.ts"] = "export default () => {};"
					}
				}
				f := nativeSafetyFixture(t, "extension_package", files, "index.ts")
				target := filepath.Join(t.TempDir(), "target")
				switch targetKind {
				case "file":
					writeFile(t, target, "export default () => {};")
				case "directory":
					mkdirManifestless(t, target)
				}
				path := filepath.Join(f.module, filepath.FromSlash(relative))
				mkdirManifestless(t, filepath.Dir(path))
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
				requireManifestlessUnavailable(t, f)
			})
		}
	}
}

func TestManifestlessNativeRejectsInvalidInventories(t *testing.T) {
	prefix := nativeSafetyBundlePrefix(".omp/agent")
	for name, inventory := range map[string][]string{
		"empty":                        nil,
		"unknown root":                 {".unknown/extensions/demo/index.ts"},
		"missing source payload":       {prefix + "missing.ts"},
		"directory payload":            {prefix + "src"},
		"escape":                       {prefix + "../index.ts"},
		"dot segment":                  {prefix + "./index.ts"},
		"double separator":             {prefix + "/index.ts"},
		"backslash":                    {prefix + `src\index.ts`},
		"whitespace":                   {" " + prefix + "index.ts"},
		"trailing slash":               {prefix + "index.ts/"},
		"wrong runtime":                {".omp/agent/extensions/apm-hooks-user/index.ts"},
		"reserved name":                {".omp/agent/extensions/apm-hooks-user/package.json"},
		"project runtime in user root": {".omp/agent/extensions/apm-hooks-project/index.ts"},
		"invalid name":                 {".omp/agent/extensions/bad:name/index.ts"},
		"multiple bundles":             {prefix + "index.ts", ".omp/agent/extensions/other/index.ts"},
		"bundle case collision":        {".omp/agent/extensions/Demo/index.ts", ".omp/agent/extensions/demo/index.ts"},
		"unknown mixed path":           {prefix + "index.ts", ".claude/mcp.json"},
	} {
		t.Run(name, func(t *testing.T) {
			f := nativeSafetyFixture(t, "extension_package", nativeSafetyDeclaredFiles(), "index.ts")
			mkdirManifestless(t, filepath.Join(f.module, "src"))
			f.rows[0].lockEvidence.DeployedFiles = inventory
			requireManifestlessUnavailable(t, f)
		})
	}
	for _, packageType := range []string{"unknown", "extension", "hook", "hybrid", ""} {
		t.Run("package type "+packageType, func(t *testing.T) {
			f := nativeSafetyFixture(t, "extension_package", nativeSafetyDeclaredFiles(), "index.ts")
			f.rows[0].lockEvidence.PackageType = packageType
			requireManifestlessUnavailable(t, f)
		})
	}
}

func TestManifestlessNativeRejectsMalformedHooks(t *testing.T) {
	for name, declaration := range map[string]string{
		"invalid json": `{`, "empty": `{}`, "empty events": `{"SessionStart":[]}`,
		"empty command":      `{"SessionStart":[{"command":""}]}`,
		"wrong command type": `{"SessionStart":[{"command":42}]}`,
		"malformed wrapped":  `{"hooks":{"SessionStart":"command"}}`,
		"empty nested":       `{"hooks":{"SessionStart":[{"hooks":[]}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PI_CODING_AGENT_DIR", "")
			f := newManifestlessSkillFixture(t, "acme/demo", "", "hook_package", []string{".omp/agent/extensions/apm-hooks-user/index.ts", ".omp/agent/apm-hooks-runtime.json"})
			writeFile(t, filepath.Join(f.module, "hooks", "session.json"), declaration)
			requireManifestlessUnavailable(t, f)
		})
	}
}

func TestManifestlessNativeProofRejectsCarrierAdditions(t *testing.T) {
	carriers := append([]string{"apm.yml"}, apmManifestlessServiceCarriers...)
	for _, carrier := range carriers {
		for _, kind := range []string{"file", "symlink", "directory"} {
			t.Run(carrier+"/"+kind, func(t *testing.T) {
				f := nativeSafetyFixture(t, "extension_package", nativeSafetyDeclaredFiles(), "index.ts")
				evidence := f.evidence()
				if len(evidence.Unavailable) != 0 {
					t.Fatalf("initial evidence unavailable: %#v", evidence)
				}
				path := filepath.Join(f.module, filepath.FromSlash(carrier))
				mkdirManifestless(t, filepath.Dir(path))
				switch kind {
				case "file":
					writeFile(t, path, "{}")
				case "directory":
					mkdirManifestless(t, path)
				case "symlink":
					if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), path); err != nil {
						t.Fatal(err)
					}
				}
				if err := verifyAgentsModuleManifestIdentities(evidence.Manifests); err == nil {
					t.Fatal("carrier addition did not invalidate proof")
				}
				if carrier != "apm.yml" || kind != "file" {
					requireManifestlessUnavailable(t, f)
				}
			})
		}
	}
}

func TestManifestlessNativeProofPinsPositiveFiles(t *testing.T) {
	for _, relative := range []string{"package.json", "index.ts", "hooks/session.json"} {
		t.Run(relative, func(t *testing.T) {
			files := nativeSafetyDeclaredFiles()
			files["assets/data.json"] = `{"value":1}`
			files["hooks/session.json"] = `{"SessionStart":[{"command":"printf ready"}]}`
			f := nativeSafetyFixture(t, "extension_package", files, "index.ts", "assets/data.json")
			evidence := f.evidence()
			if len(evidence.Unavailable) != 0 {
				t.Fatalf("initial evidence unavailable: %#v", evidence)
			}
			if err := verifyAgentsModuleManifestIdentities(evidence.Manifests); err != nil {
				t.Fatalf("unchanged proof: %v", err)
			}
			writeFile(t, filepath.Join(f.module, filepath.FromSlash(relative)), files[relative]+"\n ")
			if err := verifyAgentsModuleManifestIdentities(evidence.Manifests); err == nil {
				t.Fatal("changed source accepted")
			}
		})
	}
}

func TestManifestlessNativeAgentDirectoryUsesAPMPathSemantics(t *testing.T) {
	for _, name := range []string{"absolute inside home", "absolute outside home", "relative workspace", "literal tilde", "spaces", "home itself"} {
		t.Run(name, func(t *testing.T) {
			f := nativeSafetyFixture(t, "extension_package", nativeSafetyDeclaredFiles(), "index.ts")
			home := filepath.Dir(f.workspace)
			var value, root string
			switch name {
			case "absolute inside home":
				value, root = filepath.Join(home, "native", "agent"), "native/agent"
			case "absolute outside home":
				value = filepath.Join(t.TempDir(), "agent")
				root = filepath.ToSlash(value)
			case "relative workspace":
				value, root = "native/agent", ".apm/native/agent"
			case "literal tilde":
				value, root = "~/native", ".apm/~/native"
			case "spaces":
				value, root = " native agent ", ".apm/ native agent "
			case "home itself":
				value, root = home, "."
			}
			t.Setenv("PI_CODING_AGENT_DIR", value)
			prefix := nativeSafetyBundlePrefix(root)
			if root == "." {
				prefix = strings.TrimPrefix(prefix, "./")
			}
			f.rows[0].lockEvidence.DeployedFiles = []string{prefix + "package.json", prefix + ".apm-extension.json", prefix + "index.ts"}
			requireManifestlessKnownEmpty(t, f)
			f.rows[0].lockEvidence.DeployedFiles = []string{nativeSafetyBundlePrefix(".omp/agent") + "index.ts"}
			requireManifestlessUnavailable(t, f)
			writeFile(t, filepath.Join(f.module, "hooks", "session.json"), `{"SessionStart":[{"command":"printf ready"}]}`)
			f.rows[0].lockEvidence.PackageType = "hook_package"
			f.rows[0].lockEvidence.DeployedFiles = []string{
				filepath.ToSlash(filepath.Join(root, "extensions/apm-hooks-user/index.ts")),
				filepath.ToSlash(filepath.Join(root, "apm-hooks-runtime.json")),
			}
			requireManifestlessKnownEmpty(t, f)
			f.rows[0].lockEvidence.DeployedFiles = []string{".omp/agent/extensions/apm-hooks-user/index.ts", ".omp/agent/apm-hooks-runtime.json"}
			requireManifestlessUnavailable(t, f)
		})
	}
}

func TestManifestlessNativeAcceptsLargePayloadsAndPinsEntrypointContent(t *testing.T) {
	const payloadSize = (1 << 20) + 1
	files := map[string]string{
		"package.json":        `{"omp":{"extensions":["index.js"]}}`,
		"index.js":            "export default () => {};\n//" + strings.Repeat("x", payloadSize-len("export default () => {};\n//")),
		"assets/resource.bin": strings.Repeat("a", payloadSize),
	}
	f := nativeSafetyFixture(t, "extension_package", files, "index.js", "assets/resource.bin")
	evidence := f.evidence()
	if len(evidence.Unavailable) != 0 || len(evidence.Children) != 0 {
		t.Fatalf("large native payload rejected: %#v", evidence)
	}
	if err := verifyAgentsModuleManifestIdentities(evidence.Manifests); err != nil {
		t.Fatalf("unchanged large native payload: %v", err)
	}
	path := filepath.Join(f.module, "index.js")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	content := files["index.js"]
	writeFile(t, path, content[:len(content)-1]+"z")
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := verifyAgentsModuleManifestIdentities(evidence.Manifests); err == nil {
		t.Fatal("same-length large entrypoint mutation with restored mtime accepted")
	}
}

func TestManifestlessNativeAgentDirectoryResolvesSymlinksBeforeParentSegments(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		t.Run(fmt.Sprintf("absolute=%t", absolute), func(t *testing.T) {
			f := nativeSafetyFixture(t, "extension_package", nativeSafetyDeclaredFiles(), "index.ts")
			home := filepath.Dir(f.workspace)
			target := filepath.Join(home, "resolved", "child")
			mkdirManifestless(t, target)
			link := filepath.Join(f.workspace, "native-link")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			value := "native-link/../agent"
			if absolute {
				value = f.workspace + "/" + value
			}
			t.Setenv("PI_CODING_AGENT_DIR", value)
			prefix := nativeSafetyBundlePrefix("resolved/agent")
			f.rows[0].lockEvidence.DeployedFiles = []string{prefix + "package.json", prefix + ".apm-extension.json", prefix + "index.ts"}
			requireManifestlessKnownEmpty(t, f)
			f.rows[0].lockEvidence.DeployedFiles = []string{nativeSafetyBundlePrefix(".apm/agent") + "index.ts"}
			requireManifestlessUnavailable(t, f)
		})
	}
}

func TestManifestlessNativeAcceptsAPMSkillMixAndHonorsRootPrecedence(t *testing.T) {
	for _, packageType := range []string{"extension_package", "hook_package"} {
		for _, rootSkill := range []string{"", "demo", "other"} {
			t.Run(packageType+"/root="+rootSkill, func(t *testing.T) {
				f := nativeSafetyFixture(t, packageType, nativeSafetyDeclaredFiles(), "index.ts")
				if packageType == "hook_package" {
					writeFile(t, filepath.Join(f.module, "hooks", "session.json"), `{"SessionStart":[{"command":"printf ready"}]}`)
					f.rows[0].lockEvidence.DeployedFiles = []string{".omp/agent/extensions/apm-hooks-user/index.ts", ".omp/agent/apm-hooks-runtime.json"}
				}
				writeSkillManifest(t, f.module, ".apm/skills/demo/SKILL.md")
				if rootSkill != "" {
					writeSkillManifest(t, f.module, "skills/"+rootSkill+"/SKILL.md")
				}
				f.rows[0].lockEvidence.DeployedFiles = append(f.rows[0].lockEvidence.DeployedFiles, ".agents/skills/demo/SKILL.md")
				if rootSkill == "other" {
					requireManifestlessUnavailable(t, f)
				} else {
					requireManifestlessKnownEmpty(t, f)
				}
			})
		}
	}
}

func TestManifestlessNativeSkillTypesAllowEmptySelectionOnlyWithSkillSource(t *testing.T) {
	for _, packageType := range []string{"claude_skill", "skill_bundle"} {
		for _, source := range []string{"", "SKILL.md", "skills/demo/SKILL.md"} {
			t.Run(packageType+"/source="+source, func(t *testing.T) {
				f := nativeSafetyFixture(t, packageType, nativeSafetyDeclaredFiles(), "index.ts")
				if source != "" {
					writeSkillManifest(t, f.module, source)
				}
				validSource := packageType == "claude_skill" && source == "SKILL.md" || packageType == "skill_bundle" && source == "skills/demo/SKILL.md"
				if validSource {
					requireManifestlessKnownEmpty(t, f)
				} else {
					requireManifestlessUnavailable(t, f)
				}
			})
		}
	}
}

func TestManifestlessNativeProofPinsResourcePathIdentity(t *testing.T) {
	for _, replacement := range []string{"regular file", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			files := nativeSafetyDeclaredFiles()
			files["assets/resource.bin"] = "resource"
			f := nativeSafetyFixture(t, "extension_package", files, "index.ts", "assets/resource.bin")
			evidence := f.evidence()
			if len(evidence.Unavailable) != 0 {
				t.Fatalf("initial evidence unavailable: %#v", evidence)
			}
			path := filepath.Join(f.module, "assets", "resource.bin")
			original := filepath.Join(t.TempDir(), "original")
			if err := os.Rename(path, original); err != nil {
				t.Fatal(err)
			}
			if replacement == "symlink" {
				if err := os.Symlink(original, path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, path, files["assets/resource.bin"])
			}
			if err := verifyAgentsModuleManifestIdentities(evidence.Manifests); err == nil {
				t.Fatal("resource path replacement accepted")
			}
			if replacement == "symlink" {
				requireManifestlessUnavailable(t, f)
			}
		})
	}
}

func TestManifestlessHookInventoryRequiresUserScopeBridge(t *testing.T) {
	for name, bridge := range map[string]string{
		"obsolete unscoped bridge":       ".omp/agent/extensions/apm-hooks/index.ts",
		"project bridge in user root":    ".omp/agent/extensions/apm-hooks-project/index.ts",
		"project bridge in project root": ".omp/extensions/apm-hooks-project/index.ts",
		"unknown user bridge payload":    ".omp/agent/extensions/apm-hooks-user/package.json",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PI_CODING_AGENT_DIR", "")
			f := newManifestlessSkillFixture(t, "acme/demo", "", "hook_package", []string{bridge, ".omp/agent/apm-hooks-runtime.json"})
			writeFile(t, filepath.Join(f.module, "hooks", "session.json"), `{"SessionStart":[{"command":"printf ready"}]}`)
			requireManifestlessUnavailable(t, f)
		})
	}
}
