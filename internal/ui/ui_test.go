package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPlainOutput(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{Out: &buf, Total: 2, TTY: false}
	r.Step("first", func(s *Step) error {
		s.Log("detail %d", 1)
		w := s.Writer()
		w.Write([]byte("line a\nline b\n"))
		return nil
	})
	r.Step("second", func(s *Step) error { return errors.New("boom") })
	out := buf.String()
	for _, want := range []string{"[1/2] first ...", "        detail 1", "        line a", "        line b", "[1/2] first ✓", "[2/2] second ✗"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\r") || strings.Contains(out, "\033") {
		t.Error("plain mode must not emit terminal control sequences")
	}
}

func TestProgressPlain(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{Out: &buf, Total: 1, TTY: false}
	r.Step("download", func(s *Step) error {
		data := bytes.Repeat([]byte("x"), 3000)
		pr := s.Progress(bytes.NewReader(data), int64(len(data)), "go.tar.gz")
		var sink bytes.Buffer
		sink.ReadFrom(pr)
		return nil
	})
	if !strings.Contains(buf.String(), "go.tar.gz 3 KB") {
		t.Errorf("progress summary missing:\n%s", buf.String())
	}
}
