//go:build integration

package integration_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/vttest"
)

func TestTUIWaitOwnerSurvivesTimeoutCleanup(t *testing.T) {
	wantErr := errors.New("process stopped")
	release := make(chan struct{})
	var calls atomic.Int32
	done := startTUIWait(func() error {
		calls.Add(1)
		<-release
		return wantErr
	})

	if _, ok := awaitTUIWait(done, time.Nanosecond); ok {
		t.Fatal("wait unexpectedly completed before cleanup")
	}
	close(release)
	err, ok := awaitTUIWait(done, time.Second)
	if !ok {
		t.Fatal("cleanup did not receive the waiter result")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("wait error = %v, want %v", err, wantErr)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Wait called %d times, want 1", got)
	}
}

func TestResizeTUISurvivesConcurrentCursorOutput(t *testing.T) {
	stressResize(t, resizeTerm)
}

// The emulator answers cursor moves through callbacks that take the terminal lock while holding its own.
func stressResize(t *testing.T, resize func(*vttest.Terminal, int, int) error) {
	t.Helper()
	term, err := vttest.NewTerminal(t, 80, 24)
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", "-c", `while :; do printf '\033[H\033[5;5Hx\033[10;10Hy\033[24;80Hz'; done`)
	if err := term.Start(cmd); err != nil {
		t.Fatalf("start cursor writer: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	const rounds = 300
	done := make(chan error, 1)
	go func() {
		for i := range rounds {
			if err := resize(term, 80+i%40, 24+i%10); err != nil {
				done <- fmt.Errorf("resize round %d: %w", i, err)
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("resize deadlocked against concurrent cursor output")
	}
	if w, h := term.Emulator.Width(), term.Emulator.Height(); w != 80+(rounds-1)%40 || h != 24+(rounds-1)%10 {
		t.Fatalf("emulator size = %dx%d after the last resize", w, h)
	}
}

func TestHarnessAvoidsUnsafeTerminalMethods(t *testing.T) {
	inverting := regexp.MustCompile(`\bterm\.(Resize|SendText|SendKey|SendMouse|Paste|Snapshot|Close)\(`)
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("list integration test sources: %v", err)
	}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		code := regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAll(src, nil)
		for _, m := range inverting.FindAllSubmatch(code, -1) {
			t.Errorf("%s calls term.%s; vttest's Terminal methods race or take its lock before the emulator's, so use term.Emulator or resizeTUI", file, m[1])
		}
	}
}
