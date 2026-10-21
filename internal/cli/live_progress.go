package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"github.com/lkshrk/omni/internal/executor"
)

// liveProgress keeps one rewritable status line under the permanent progress
// lines while package managers run: the current step and the installer's latest
// output line. It redraws only on events, never on a timer, so a sudo password
// prompt on the terminal is never overwritten while it waits. Off a terminal it
// does nothing and only permanent lines are printed.
type liveProgress struct {
	mu     sync.Mutex
	out    io.Writer
	live   bool
	width  int
	step   string
	shown  bool
	latest string
}

// raw is the command's own writer, checked for a terminal; out (cmdOut) may wrap it.
func newLiveProgress(raw, out io.Writer) *liveProgress {
	p := &liveProgress{out: out, width: 80}
	if f, ok := raw.(*os.File); ok && term.IsTerminal(f.Fd()) {
		p.live = true
		if w, _, err := term.GetSize(f.Fd()); err == nil && w > 0 {
			p.width = w
		}
	}
	return p
}

// Context streams the output of commands run under it into the status line.
func (p *liveProgress) Context(ctx context.Context) context.Context {
	if !p.live {
		return ctx
	}
	return executor.WithOutputObserver(ctx, p.Output)
}

// Step prints a permanent progress line and makes it the status line's label.
func (p *liveProgress) Step(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLocked()
	fmt.Fprintf(p.out, "  %s\n", msg)
	p.step = strings.TrimSpace(msg)
	p.latest = ""
}

// Println prints a permanent line, clearing the status line first.
func (p *liveProgress) Println(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLocked()
	fmt.Fprintln(p.out, line)
}

// Output shows one line of command output in the status line. Safe for concurrent use.
func (p *liveProgress) Output(line string) {
	if !p.live {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.latest = line
	p.drawLocked()
}

// Done clears the status line; call it before printing a final summary.
func (p *liveProgress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLocked()
	p.step = ""
	p.latest = ""
}

func (p *liveProgress) drawLocked() {
	if !p.live || p.latest == "" {
		return
	}
	text := "  │ " + p.latest
	if p.step != "" {
		text = "  │ " + strings.TrimSuffix(p.step, "…") + " · " + p.latest
	}
	text = ansi.Truncate(text, p.width-1, "…")
	fmt.Fprintf(p.out, "\r\033[2K\033[2m%s\033[0m", text)
	p.shown = true
}

func (p *liveProgress) clearLocked() {
	if p.shown {
		fmt.Fprint(p.out, "\r\033[2K")
		p.shown = false
	}
}
