package render

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRendersTemplatesAndPaths(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "files", "pkg", "{{.Pkg}}", "x.go.tmpl"), "package {{.Pkg}} // {{.Module}}\n")
	write(t, filepath.Join(src, "files", "README.md"), "# {{not rendered}}\n")

	plan, err := Build(src, map[string]string{"Pkg": "logger", "Module": "example.com/app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(plan.Files))
	}
	if plan.Files[0].Rel != "README.md" || string(plan.Files[0].Content) != "# {{not rendered}}\n" {
		t.Errorf("verbatim file wrong: %+v", plan.Files[0])
	}
	if plan.Files[1].Rel != "pkg/logger/x.go" || string(plan.Files[1].Content) != "package logger // example.com/app\n" {
		t.Errorf("template file wrong: %q %q", plan.Files[1].Rel, plan.Files[1].Content)
	}

	root := t.TempDir()
	hashes, err := plan.Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hashes["pkg/logger/x.go"]; !ok {
		t.Errorf("hash missing: %v", hashes)
	}
	if _, err := os.Stat(filepath.Join(root, "pkg", "logger", "x.go")); err != nil {
		t.Error(err)
	}
}

func TestBuildFailsOnMissingVar(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "files", "a.go.tmpl"), "{{.Nope}}")
	if _, err := Build(src, map[string]string{}); err == nil {
		t.Fatal("expected error for missing variable")
	}
}

func TestPascalAndFuncs(t *testing.T) {
	cases := map[string]string{"order": "Order", "order-item": "OrderItem", "order_item": "OrderItem", "": ""}
	for in, want := range cases {
		if got := Pascal(in); got != want {
			t.Errorf("Pascal(%q) = %q, want %q", in, got, want)
		}
	}
	out, err := renderString("x", "{{.S | title}} {{.S | camel}} {{.S | snake}} {{.S | upper}}", map[string]string{"S": "order-item"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "OrderItem orderItem order_item ORDER-ITEM" {
		t.Errorf("got %q", out)
	}
}

func TestEmptyRenderedTemplateProducesNoFile(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "files", "a.yml.tmpl"), "{{if eq .CI \"gitlab\"}}stages: [build]\n{{end}}")
	write(t, filepath.Join(src, "files", "b.yml.tmpl"), "{{if eq .CI \"github\"}}name: ci\n{{end}}")
	write(t, filepath.Join(src, "files", "empty.txt"), "")
	plan, err := Build(src, map[string]string{"CI": "gitlab"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range plan.Files {
		got = append(got, f.Rel)
	}
	if len(got) != 2 || got[0] != "a.yml" || got[1] != "empty.txt" {
		t.Fatalf("files = %v; want [a.yml empty.txt] (verbatim empty files are kept, empty templates are not)", got)
	}
}
