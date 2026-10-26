package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestlessSkillSourceLayouts(t *testing.T) {
	for _, test := range []struct {
		name        string
		packageType string
		files       []string
		directories []string
		deployed    []string
		want        bool
		positive    string
		negative    string
	}{
		{name: "root single skill", packageType: "claude_skill", files: []string{"SKILL.md"}, deployed: []string{"installed-name"}, want: true, positive: "SKILL.md"},
		{name: "single skill unselected", packageType: "claude_skill", files: []string{"SKILL.md"}, want: true, positive: "SKILL.md"},
		{name: "single skill missing", packageType: "claude_skill", want: false},
		{name: "single skill multiple deployed", packageType: "claude_skill", files: []string{"SKILL.md"}, deployed: []string{"one", "two"}, want: false},
		{name: "bundle subset", packageType: "skill_bundle", files: []string{"skills/one/SKILL.md", "skills/two/SKILL.md"}, deployed: []string{"two"}, want: true, positive: "skills/one/SKILL.md"},
		{name: "bundle unselected", packageType: "skill_bundle", files: []string{"skills/one/SKILL.md"}, want: true, positive: "skills/one/SKILL.md"},
		{name: "bundle missing root source", packageType: "skill_bundle", files: []string{".apm/skills/one/SKILL.md"}, deployed: []string{"one"}, want: false},
		{name: "bundle inventory missing", packageType: "skill_bundle", files: []string{"skills/one/SKILL.md"}, deployed: []string{"two"}, want: false},
		{name: "native normalized skills", packageType: "extension_package", files: []string{".apm/skills/one/SKILL.md"}, deployed: []string{"one"}, want: true, positive: ".apm/skills/one/SKILL.md", negative: "skills"},
		{name: "hook empty root fallback", packageType: "hook_package", files: []string{".apm/skills/one/SKILL.md"}, directories: []string{"skills/unused"}, deployed: []string{"one"}, want: true, positive: ".apm/skills/one/SKILL.md", negative: "skills/unused/SKILL.md"},
		{name: "root bundle wins", packageType: "extension_package", files: []string{"skills/one/SKILL.md", ".apm/skills/two/SKILL.md"}, deployed: []string{"one"}, want: true, positive: "skills/one/SKILL.md"},
		{name: "normalized inventory shadowed", packageType: "hook_package", files: []string{"skills/one/SKILL.md", ".apm/skills/two/SKILL.md"}, deployed: []string{"two"}, want: false},
		{name: "native no skills", packageType: "extension_package", want: true},
		{name: "case collision", packageType: "skill_bundle", files: []string{"skills/one/SKILL.md", "skills/ONE/SKILL.md"}, deployed: []string{"one"}, want: false},
		{name: "wrong filename case", packageType: "skill_bundle", files: []string{"skills/one/skill.md"}, deployed: []string{"one"}, want: false},
		{name: "wrong inventory case", packageType: "skill_bundle", files: []string{"skills/one/SKILL.md"}, deployed: []string{"ONE"}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			module := filepath.Join(root, "package")
			if err := os.MkdirAll(module, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, rel := range test.directories {
				if err := os.MkdirAll(filepath.Join(module, filepath.FromSlash(rel)), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, rel := range test.files {
				filename := filepath.Join(module, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte("# Skill\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			deployed := make(map[string]struct{})
			for _, name := range test.deployed {
				deployed[name] = struct{}{}
			}
			identities, ok := manifestlessSkillSourceEvidence(root, module, test.packageType, deployed)
			if ok != test.want {
				t.Fatalf("manifestlessSkillSourceEvidence() valid = %v, want %v", ok, test.want)
			}
			if !ok {
				return
			}
			positive, negative := test.positive == "", test.negative == ""
			for _, identity := range identities {
				if identity.RelativePath == test.positive && !identity.Absent && identity.Hash != "" {
					positive = true
				}
				if identity.RelativePath == test.negative && identity.Absent && len(identity.Parents) != 0 {
					negative = true
				}
			}
			if !positive || !negative {
				t.Fatalf("missing source evidence: positive=%v negative=%v", positive, negative)
			}
		})
	}
}

func TestManifestlessSkillSourceRejectsLinks(t *testing.T) {
	for _, linked := range []string{"skills", "skills/one", "skills/one/SKILL.md"} {
		t.Run(linked, func(t *testing.T) {
			root := t.TempDir()
			module := filepath.Join(root, "package")
			external := t.TempDir()
			if err := os.MkdirAll(filepath.Join(external, "one"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(external, "one", "SKILL.md"), []byte("# Skill\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(module, filepath.FromSlash(linked))
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			target := external
			if linked != "skills" {
				target = filepath.Join(external, "one")
			}
			if linked == "skills/one/SKILL.md" {
				target = filepath.Join(target, "SKILL.md")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if _, ok := manifestlessSkillSourceEvidence(root, module, "skill_bundle", map[string]struct{}{"one": {}}); ok {
				t.Fatal("accepted linked skill source")
			}
		})
	}
}
