package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Styler adds ANSI colours when its stream is a terminal that wants them.
//
// Colour marks where to look, nothing more: green for success, red for
// failure, yellow for "needs your attention", bold for headings, dim for
// secondary detail. Output that is piped, captured by CI or run with NO_COLOR
// stays free of escape sequences so logs remain readable.
type Styler struct{ on bool }

var (
	// Stdout and Stderr are decided once, per stream: `devkit update | tee log`
	// must not colour the pipe just because stderr is a terminal.
	Stdout = NewStyler(os.Stdout)
	Stderr = NewStyler(os.Stderr)
)

// NewStyler enables colour for f when it is an interactive terminal, unless
// the environment says otherwise. CLICOLOR_FORCE=1 forces it on (handy for
// `| less -R` and for tests).
func NewStyler(f *os.File) Styler {
	if v := os.Getenv("CLICOLOR_FORCE"); v != "" && v != "0" {
		return Styler{on: true}
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return Styler{}
	}
	return Styler{on: IsTerminal(f)}
}

// For returns the styler matching an output stream.
func For(w io.Writer) Styler {
	switch w {
	case io.Writer(os.Stdout):
		return Stdout
	case io.Writer(os.Stderr):
		return Stderr
	}
	return Styler{}
}

func (s Styler) wrap(code, t string) string {
	if !s.on || t == "" {
		return t
	}
	return "\033[" + code + "m" + t + "\033[0m"
}

func (s Styler) Bold(t string) string   { return s.wrap("1", t) }
func (s Styler) Dim(t string) string    { return s.wrap("2", t) }
func (s Styler) Red(t string) string    { return s.wrap("31", t) }
func (s Styler) Green(t string) string  { return s.wrap("32", t) }
func (s Styler) Yellow(t string) string { return s.wrap("33", t) }
func (s Styler) Cyan(t string) string   { return s.wrap("36", t) }

// Heading is bold; Success, Failure and Attention are the bold coloured forms.
func (s Styler) Heading(t string) string   { return s.wrap("1", t) }
func (s Styler) Success(t string) string   { return s.wrap("1;32", t) }
func (s Styler) Failure(t string) string   { return s.wrap("1;31", t) }
func (s Styler) Attention(t string) string { return s.wrap("1;33", t) }

// Line colours one progress line by its leading marker, so that the code
// producing the lines (the installer, git helpers, hooks) stays unaware of
// terminals:
//
//   - file      created or updated      green marker
//   - file      deleted                 red marker
//     ! file      skipped, needs a merge  yellow, whole line
//     ~ file      backup written          cyan marker
//     = file      left alone              dim
//     $ command   hook being run          dim
//     warning: …                          yellow
func (s Styler) Line(line string) string {
	if !s.on {
		return line
	}
	trimmed := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(trimmed)]
	switch {
	case strings.HasPrefix(trimmed, "+ "):
		return indent + s.Green("+") + trimmed[1:]
	case strings.HasPrefix(trimmed, "- "):
		return indent + s.Red("-") + trimmed[1:]
	case strings.HasPrefix(trimmed, "! "):
		return indent + s.Yellow(trimmed)
	case strings.HasPrefix(trimmed, "~ "):
		return indent + s.Cyan("~") + trimmed[1:]
	case strings.HasPrefix(trimmed, "= "), strings.HasPrefix(trimmed, "$ "):
		return indent + s.Dim(trimmed)
	case strings.HasPrefix(trimmed, "warning:"):
		return indent + s.Yellow(trimmed)
	case strings.HasPrefix(trimmed, "updating "), strings.HasPrefix(trimmed, "installing "), strings.HasPrefix(trimmed, "removing "):
		return indent + s.Bold(trimmed)
	case strings.Contains(trimmed, "were modified locally and not updated"):
		return indent + s.Yellow(trimmed)
	case strings.HasPrefix(trimmed, "running ") && strings.HasSuffix(trimmed, " hooks"):
		return indent + s.Dim(trimmed)
	case strings.HasSuffix(trimmed, "(installed now)"):
		return indent + strings.TrimSuffix(trimmed, "(installed now)") + s.Green("(installed now)")
	}
	return line
}

// LineWriter styles text streamed from a subprocess line by line: lines with a
// known marker get its colour, everything else is dimmed as secondary detail.
// With colour off it passes the bytes through untouched.
func LineWriter(w io.Writer) io.Writer {
	st := For(w)
	if !st.on {
		return w
	}
	return &lineWriter{w: w, st: st}
}

type lineWriter struct {
	w   io.Writer
	st  Styler
	buf []byte
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := strings.IndexByte(string(l.buf), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(l.buf[:i]), "\r")
		l.buf = l.buf[i+1:]
		if styled := l.st.Line(line); styled != line {
			fmt.Fprintln(l.w, styled)
		} else {
			fmt.Fprintln(l.w, l.st.Dim(line))
		}
	}
	return len(p), nil
}

// Cell is one table cell; Style is applied after the layout is computed, so
// escape sequences never disturb the column widths (text/tabwriter would count
// them as visible characters).
type Cell struct {
	Text  string
	Style func(string) string
}

// C builds a cell. A nil style leaves the text as is.
func C(text string, style func(string) string) Cell { return Cell{text, style} }

// Table prints rows in aligned columns with two spaces between them. The first
// row is the header.
func Table(w io.Writer, rows [][]Cell) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, 0)
	for _, r := range rows {
		for i, c := range r {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			if n := utf8.RuneCountInString(c.Text); n > widths[i] {
				widths[i] = n
			}
		}
	}
	st := For(w)
	for ri, r := range rows {
		var b strings.Builder
		for i, c := range r {
			text := c.Text
			pad := widths[i] - utf8.RuneCountInString(text)
			switch {
			case ri == 0:
				text = st.Dim(text)
			case c.Style != nil:
				text = c.Style(text)
			}
			b.WriteString(text)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", pad+2))
			}
		}
		fmt.Fprintln(w, b.String())
	}
}
