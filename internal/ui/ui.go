// Package ui renders step-by-step progress for long-running commands.
//
// On a terminal each step shows a spinner while running and a check mark or
// cross with its duration when done; sub-lines (tool output, details) are
// indented under the step. When stderr is not a terminal (CI, pipes) the same
// information is printed as plain lines without escape sequences.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	indent  = "        "
	okMark  = "✓"
	errMark = "✗"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Runner prints numbered steps to Out.
type Runner struct {
	Out   io.Writer
	Total int
	TTY   bool

	n     int
	mu    sync.Mutex
	start time.Time
}

// New creates a runner writing to stderr, detecting whether it is a terminal.
func New(total int) *Runner {
	return &Runner{Out: os.Stderr, Total: total, TTY: IsTerminal(os.Stderr), start: time.Now()}
}

// IsTerminal reports whether f is a character device (an interactive terminal).
func IsTerminal(f *os.File) bool {
	if os.Getenv("CI") != "" || os.Getenv("DEVKIT_PLAIN") != "" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Step is passed to the function running one step.
type Step struct {
	r       *Runner
	title   string
	spin    chan struct{}
	done    chan struct{}
	started time.Time
	lines   int
}

// Step runs fn as the next numbered step.
func (r *Runner) Step(title string, fn func(s *Step) error) error {
	r.n++
	s := &Step{r: r, title: title, started: time.Now()}
	s.begin()
	err := fn(s)
	s.end(err)
	return err
}

func (r *Runner) label(n int, title string) string {
	st := For(r.Out)
	return st.Dim(fmt.Sprintf("[%d/%d]", n, r.Total)) + " " + title
}

// result renders the closing line of a step.
func (r *Runner) result(n int, title string, err error, elapsed time.Duration) string {
	st := For(r.Out)
	mark := st.Success(okMark)
	if err != nil {
		mark = st.Failure(errMark)
		title = st.Red(title)
	}
	return fmt.Sprintf("%s %s %s", r.label(n, title), mark, st.Dim(elapsed.String()))
}

func (s *Step) begin() {
	r := s.r
	if !r.TTY {
		fmt.Fprintf(r.Out, "%s ...\n", r.label(r.n, s.title))
		return
	}
	s.spin = make(chan struct{})
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		i := 0
		for {
			select {
			case <-s.spin:
				return
			case <-time.After(80 * time.Millisecond):
				r.mu.Lock()
				fmt.Fprintf(r.Out, "\r%s %s", r.label(r.n, s.title), spinnerFrames[i%len(spinnerFrames)])
				r.mu.Unlock()
				i++
			}
		}
	}()
}

func (s *Step) stopSpinner() {
	if s.spin == nil {
		return
	}
	close(s.spin)
	<-s.done
	s.spin = nil
	s.r.mu.Lock()
	fmt.Fprintf(s.r.Out, "\r\033[K")
	s.r.mu.Unlock()
}

func (s *Step) end(err error) {
	r := s.r
	elapsed := time.Since(s.started).Round(100 * time.Millisecond)
	s.stopSpinner()
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintln(r.Out, r.result(r.n, s.title, err, elapsed))
}

// Log prints a detail line under the current step.
func (s *Step) Log(format string, args ...any) {
	s.stopSpinnerForOutput()
	s.r.mu.Lock()
	fmt.Fprintln(s.r.Out, For(s.r.Out).Line(indent+fmt.Sprintf(format, args...)))
	s.r.mu.Unlock()
	s.lines++
}

// Logf is the func(string, ...any) form of Log for APIs that take a logger.
func (s *Step) Logf(format string, args ...any) { s.Log(format, args...) }

// Writer returns a writer that indents streamed subprocess output under the step.
func (s *Step) Writer() io.Writer {
	s.stopSpinnerForOutput()
	return &indentWriter{s: s}
}

// stopSpinnerForOutput freezes the spinner line before other output is written
// so that lines do not interleave with \r redraws.
func (s *Step) stopSpinnerForOutput() {
	if s.spin != nil {
		s.stopSpinner()
		s.r.mu.Lock()
		fmt.Fprintf(s.r.Out, "%s\n", s.r.label(s.r.n, s.title))
		s.r.mu.Unlock()
	}
}

type indentWriter struct {
	s   *Step
	buf []byte
}

func (w *indentWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if strings.TrimSpace(line) == "" {
			continue
		}
		w.s.r.mu.Lock()
		st := For(w.s.r.Out)
		if styled := st.Line(indent + line); styled != indent+line {
			fmt.Fprintln(w.s.r.Out, styled)
		} else {
			fmt.Fprintln(w.s.r.Out, indent+st.Dim(line))
		}
		w.s.r.mu.Unlock()
		w.s.lines++
	}
	return len(p), nil
}

// Progress wraps r and renders a download bar on the step while it is read.
func (s *Step) Progress(r io.Reader, total int64, label string) io.Reader {
	s.stopSpinnerForOutput()
	return &progressReader{s: s, r: r, total: total, label: label, last: time.Now().Add(-time.Second)}
}

type progressReader struct {
	s     *Step
	r     io.Reader
	total int64
	n     int64
	label string
	last  time.Time
	done  bool
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	now := time.Now()
	if err == io.EOF || now.Sub(p.last) > 150*time.Millisecond {
		p.last = now
		p.render(err == io.EOF)
	}
	return n, err
}

func (p *progressReader) render(final bool) {
	r := p.s.r
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.TTY {
		if final && !p.done {
			p.done = true
			fmt.Fprintf(r.Out, indent+"%s %s\n", p.label, human(p.n))
		}
		return
	}
	width := 30
	var bar string
	if p.total > 0 {
		filled := int(float64(width) * float64(p.n) / float64(p.total))
		if filled > width {
			filled = width
		}
		bar = fmt.Sprintf("[%s%s] %3d%% %s/%s", strings.Repeat("█", filled), strings.Repeat(" ", width-filled),
			p.n*100/p.total, human(p.n), human(p.total))
	} else {
		bar = human(p.n)
	}
	fmt.Fprintf(r.Out, "\r\033[K"+indent+"%s %s", p.label, bar)
	if final && !p.done {
		p.done = true
		fmt.Fprint(r.Out, "\n")
		p.s.lines++
	}
}

func human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// Done prints the final summary line.
func (r *Runner) Done(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := For(r.Out)
	fmt.Fprintf(r.Out, "\n%s %s %s\n", st.Success(okMark), st.Bold(fmt.Sprintf(format, args...)),
		st.Dim("("+time.Since(r.start).Round(100*time.Millisecond).String()+")"))
}

// Failed prints the final failure line.
func (r *Runner) Failed(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := For(r.Out)
	fmt.Fprintf(r.Out, "\n%s %s\n", st.Failure(errMark), st.Red(err.Error()))
}
