package apm

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	commandexec "github.com/lkshrk/omni/internal/executor"
)

func TestOutdatedParsesRichAndSetsCommandEnvironment(t *testing.T) {
	mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stdout: "\x1b[33m│ acme/tool │ v1.0.0 │ v1.1.0 │ outdated │ github.com/acme/tool │\x1b[0m\n│ acme/other │ main │ - │ unknown │ github.com/acme/other │\n[!] 1 outdated dependency found\n"}}}
	got, err := New(mock, Global).Outdated(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := OutdatedResult{Rows: []OutdatedRow{{Package: "acme/tool", Current: "v1.0.0", Latest: "v1.1.0", Source: "github.com/acme/tool"}}, Unknown: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v", got)
	}
	call := mock.Calls[0]
	if !reflect.DeepEqual(call.Args, []string{"outdated", "-g", "--parallel-checks", "4"}) {
		t.Fatalf("args = %v", call.Args)
	}
	for _, want := range []string{"NO_COLOR=1", "COLUMNS=240", "TERM=dumb"} {
		if !strings.Contains(strings.Join(call.Env, "\n"), want) {
			t.Fatalf("env missing %q: %v", want, call.Env)
		}
	}
}

func TestOutdatedParsesPlainAndEmptySuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		want OutdatedResult
	}{
		{"plain", "Package                 Current      Latest       Status         Source\n----------------------------------------------------------------------------------\nacme/tool               1.0.0        1.2.0 (abc)  outdated       acme/tool\nacme/branch             main         -            unknown        acme/branch\n[!] 1 outdated dependency found\n", OutdatedResult{Rows: []OutdatedRow{{Package: "acme/tool", Current: "1.0.0", Latest: "1.2.0 (abc)", Source: "acme/tool"}}, Unknown: 1}},
		{"current", "[✓] All dependencies are up-to-date\n", OutdatedResult{}},
		{"local only", "[✓] No remote dependencies to check\n", OutdatedResult{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stdout: tc.out}}}
			got, err := New(mock, Global).Outdated(t.Context())
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("result=%#v err=%v", got, err)
			}
		})
	}
}

func TestOutdatedRejectsMalformedSummaryAndCommandFailure(t *testing.T) {
	for _, output := range []string{
		"[!] 2 outdated dependencies found\n",
		"│ acme/one │ 1.0 │ 2.0 │ outdated │ git tags │\n[!] 2 outdated dependencies found\n",
		"│ acme/wrapped │ 1.0 │ 2.0\n│ continued │ outdated │ git tags │\n[!] 1 outdated dependency found\n",
	} {
		mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stdout: output}}}
		if _, err := New(mock, Global).Outdated(t.Context()); err == nil {
			t.Fatalf("malformed output accepted: %q", output)
		}
	}
	sentinel := errors.New("exit 1")
	mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stderr: "No lockfile found", Err: sentinel}}}
	if _, err := New(mock, Global).Outdated(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}

// 0.31.0 prints Wanted between Current and Latest whenever any row carries a manifest constraint.
func TestOutdatedParsesTheWantedColumn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stdout string
	}{
		{
			name:   "rich",
			stdout: "│ acme/tool │ v1.0.0 │ v1.0.9 │ v1.1.0 │ outdated │ github.com/acme/tool │\n[!] 1 outdated dependency found\n",
		},
		{
			name: "plain",
			stdout: "Package                 Current      Wanted       Latest       Status         Source\n" +
				"acme/tool               v1.0.0       v1.0.9       v1.1.0       outdated       github.com/acme/tool\n" +
				"[!] 1 outdated dependency found\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &commandexec.MockExecutor{Responses: []commandexec.MockCall{{Stdout: tc.stdout}}}
			result, err := New(mock, Global).Outdated(t.Context())
			if err != nil {
				t.Fatalf("outdated: %v", err)
			}
			if len(result.Rows) != 1 {
				t.Fatalf("rows = %d, want 1: %+v", len(result.Rows), result.Rows)
			}
			row := result.Rows[0]
			if row.Package != "acme/tool" || row.Current != "v1.0.0" || row.Latest != "v1.1.0" {
				t.Fatalf("row = %+v, want acme/tool v1.0.0 → v1.1.0", row)
			}
		})
	}
}
