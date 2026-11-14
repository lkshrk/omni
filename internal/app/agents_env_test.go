package app

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestAgentsEnvFromLockfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".apm", "apm.lock.yaml"), `mcp_configs:
  litellm-tools:
    transport: http
    url: https://llm.example/mcp
    headers:
      Authorization: Bearer ${LITELLM_API}
  github:
    transport: stdio
    command: gh-mcp
    args:
    - --t=${HF}
    - --plain
    - --org=<ORG>
    env:
      TOKEN: ${GH}
      OLD: ${env:LEGACY}
      LITERAL: value
      LOWER: <lower>
mcp_target_servers:
  claude: [litellm-tools, github]
  codex: [litellm-tools]
  omp: [litellm-tools]
`)
	got, err := (&App{}).AgentsEnv()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"claude": {"GH", "HF", "LEGACY", "LITELLM_API", "ORG"},
		"codex":  {"LITELLM_API"},
		"omp":    {"LITELLM_API"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AgentsEnv = %v, want %v", got, want)
	}
}

func TestAgentsEnvEmptyWithoutLockfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := (&App{}).AgentsEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("AgentsEnv = %v, want empty", got)
	}
}
