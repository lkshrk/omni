package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLiveProgress_OffTerminalPrintsOnlyPermanentLines(t *testing.T) {
	var buf bytes.Buffer
	p := newLiveProgress(&buf, &buf)
	if ctx := context.Background(); p.Context(ctx) != ctx {
		t.Fatal("off a terminal the context must not carry an output observer")
	}
	p.Step("installing jq…")
	p.Output("downloading jq-1.8.tar.zst")
	p.Println("  ✓ done: jq (pacman)")
	p.Done()
	if got, want := buf.String(), "  installing jq…\n  ✓ done: jq (pacman)\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestLiveProgress_StatusLineRedrawsAndClears(t *testing.T) {
	var buf bytes.Buffer
	p := &liveProgress{out: &buf, live: true, width: 60}
	p.Step("installing herdr-tether…")
	p.Output("   Compiling serde v1.0.228 with a very long tail that overflows")
	p.Output("   Compiling tokio v1.48.0")
	p.Println("  ✓ done: herdr-tether (script)")
	got := buf.String()

	if !strings.HasPrefix(got, "  installing herdr-tether…\n") {
		t.Fatalf("step line missing: %q", got)
	}
	if n := strings.Count(got, "\r\033[2K\033[2m"); n != 2 {
		t.Fatalf("want 2 status redraws, got %d: %q", n, got)
	}
	if !strings.Contains(got, "installing herdr-tether · Compiling tokio v1.48.0") {
		t.Fatalf("status line should pair the step with the latest output: %q", got)
	}
	if strings.Contains(got, "overflows") {
		t.Fatalf("status line not truncated to the terminal width: %q", got)
	}
	if !strings.HasSuffix(got, "\r\033[2K  ✓ done: herdr-tether (script)\n") {
		t.Fatalf("status line must be cleared before the permanent line: %q", got)
	}
}
