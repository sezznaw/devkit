package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestStylerOffAddsNothing(t *testing.T) {
	s := Styler{}
	for _, got := range []string{s.Green("x"), s.Failure("x"), s.Line("  + file"), s.Line("warning: y")} {
		if strings.Contains(got, "\033") {
			t.Errorf("colour is off, yet got escapes: %q", got)
		}
	}
}

func TestLineColoursByMarker(t *testing.T) {
	s := Styler{on: true}
	cases := map[string]string{
		"  + Makefile":                  "\033[32m+\033[0m Makefile",
		"  - old.go":                    "\033[31m-\033[0m old.go",
		"  ! Makefile  (modified)":      "\033[33m! Makefile  (modified)\033[0m",
		"  ~ Makefile.bak  (backup)":    "\033[36m~\033[0m Makefile.bak  (backup)",
		"  $ go mod tidy":               "\033[2m$ go mod tidy\033[0m",
		"warning: git pull failed":      "\033[33mwarning: git pull failed\033[0m",
		"updating kitex-service a -> b": "\033[1mupdating kitex-service a -> b\033[0m",
	}
	for in, wantTail := range cases {
		got := s.Line(in)
		if !strings.HasSuffix(got, wantTail) {
			t.Errorf("Line(%q) = %q, want it to end with %q", in, got, wantTail)
		}
		if indent := in[:len(in)-len(strings.TrimLeft(in, " "))]; !strings.HasPrefix(got, indent) {
			t.Errorf("Line(%q) lost its indentation: %q", in, got)
		}
	}
	if plain := "  git 2.54.0"; s.Line(plain) != plain {
		t.Errorf("a line without a marker must stay untouched")
	}
	// A path that merely contains a dash must not be read as a deletion.
	if got := s.Line("  + conf/dev-local.yaml"); strings.Contains(got, "\033[31m") {
		t.Errorf("misread as deletion: %q", got)
	}
}

// stripANSI removes SGR sequences, to measure what the user actually sees.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestTableStaysAlignedWithColours(t *testing.T) {
	on := Styler{on: true}
	rows := [][]Cell{
		{C("SERVICE", nil), C("INSTALLED", nil), C("LATEST", nil)},
		{C("game", nil), C("0.2.0", nil), C("0.3.1", on.Yellow)},
		{C("user-profile", nil), C("0.3.1", nil), C("0.3.1 (up to date)", on.Green)},
	}
	var buf bytes.Buffer
	Table(&buf, rows)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.Contains(buf.String(), "\033[33m0.3.1\033[0m") {
		t.Fatalf("unexpected output:\n%q", buf.String())
	}
	// Once colours are stripped, each column must start at the same visible
	// offset in every row (header included).
	h, a, b := stripANSI(lines[0]), stripANSI(lines[1]), stripANSI(lines[2])
	if i, j, k := strings.Index(h, "INSTALLED"), strings.Index(a, "0.2.0"), strings.Index(b, "0.3.1"); i != j || j != k {
		t.Errorf("second column misaligned (%d/%d/%d):\n%s\n%s\n%s", i, j, k, h, a, b)
	}
	if i, j, k := strings.Index(h, "LATEST"), strings.LastIndex(a, "0.3.1"), strings.LastIndex(b, "0.3.1 (up"); i != j || j != k {
		t.Errorf("third column misaligned (%d/%d/%d):\n%s\n%s\n%s", i, j, k, h, a, b)
	}
}

func TestStepResultIsColouredOnlyWhenEnabled(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{Out: &buf, Total: 1} // a buffer is never a terminal
	r.Step("plain", func(s *Step) error { s.Log("+ file"); return nil })
	if strings.Contains(buf.String(), "\033") {
		t.Errorf("writing to a non-terminal must not emit escapes: %q", buf.String())
	}
}
