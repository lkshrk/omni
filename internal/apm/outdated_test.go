package apm

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	commandexec "github.com/lkshrk/omni/internal/executor"
)

func TestOutdatedParsesJSONAndSetsCommandEnvironment(t *testing.T) {
	output := `{
  "dependencies": [
    {"package": "acme/tool", "current": "v1.0.0", "latest": "v1.1.0", "status": "outdated", "source": "git tags", "wanted": null, "outside_constraint": false, "extra_tags": []},
    {"package": "acme/branch", "current": "(default)", "latest": "41f619d6", "status": "outdated", "source": "git branch", "wanted": null, "outside_constraint": false, "extra_tags": []},
    {"package": "acme/current", "current": "v2.0.0", "latest": "v2.0.0", "status": "up-to-date", "source": "git tags"},
    {"package": "acme/broken", "current": "(none)", "latest": "-", "status": "unknown", "source": ""},
    {"package": "acme/local", "current": "local", "latest": "-", "status": "not-comparable", "source": "replacement: ./local"}
  ]
}`
	mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stdout: output}}}
	got, err := New(mock, Global).Outdated(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := OutdatedResult{Rows: []OutdatedRow{
		{Package: "acme/tool", Current: "v1.0.0", Latest: "v1.1.0", Source: "git tags"},
		{Package: "acme/branch", Current: "(default)", Latest: "41f619d6", Source: "git branch"},
	}, Unknown: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
	call := mock.Calls[0]
	if !reflect.DeepEqual(call.Args, []string{"outdated", "-g", "--json", "--parallel-checks", "4"}) {
		t.Fatalf("args = %v", call.Args)
	}
	if !strings.Contains(strings.Join(call.Env, "\n"), "NO_COLOR=1") {
		t.Fatalf("env missing NO_COLOR=1: %v", call.Env)
	}
}

func TestOutdatedEmptyDependenciesIsCurrent(t *testing.T) {
	mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stdout: `{"dependencies": []}` + "\n"}}}
	got, err := New(mock, Global).Outdated(t.Context())
	if err != nil || !reflect.DeepEqual(got, OutdatedResult{}) {
		t.Fatalf("result=%#v err=%v", got, err)
	}
}

func TestOutdatedRejectsMalformedOutputAndCommandFailure(t *testing.T) {
	for _, output := range []string{
		"",
		"[✓] All dependencies are up-to-date\n",
		"| acme/tool | v1.0.0 | v1.1.0 | outdated | git tags |\n",
		`{}`,
		`{"dependencies": null}`,
		`{"error": "no_lockfile"}`,
	} {
		mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stdout: output}}}
		if _, err := New(mock, Global).Outdated(t.Context()); err == nil {
			t.Fatalf("malformed output accepted: %q", output)
		}
	}
	sentinel := errors.New("exit 1")
	mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stderr: `{"error": "no_lockfile"}`, Err: sentinel}}}
	if _, err := New(mock, Global).Outdated(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}
