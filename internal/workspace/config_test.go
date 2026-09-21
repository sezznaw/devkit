package workspace

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestFindAndLoadConfig(t *testing.T) {
	ws := t.TempDir()
	if Find(ws) != "" {
		t.Fatal("no config expected yet")
	}
	if err := SaveConfig(ws, &Config{ModulePrefix: "x.com/a", IdlRepo: "a/idl", Vars: map[string]string{"NacosAddr": "n:1"}}); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(ws, "svc", "cmd")
	os.MkdirAll(nested, 0o755)
	if got := Find(nested); got != ws {
		t.Fatalf("Find = %q, want %q", got, ws)
	}
	cfg, err := LoadConfig(ws)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModulePrefix != "x.com/a" || cfg.IdlRepo != "a/idl" || cfg.Vars["NacosAddr"] != "n:1" {
		t.Errorf("loaded %+v", cfg)
	}
	empty, err := LoadConfig(t.TempDir())
	if err != nil || empty.ModulePrefix != "" {
		t.Errorf("missing file must give empty config, got %+v %v", empty, err)
	}
}

func TestWriteTemplateIsLoadableAndPrefilled(t *testing.T) {
	dir := t.TempDir()
	if err := WriteTemplate(dir, &Config{IdlRepo: "sezznaw/shop-idl", CommonRepo: "someone/fork"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("template must be valid YAML: %v", err)
	}
	if cfg.ModulePrefix != "" || cfg.IdlRepo != "sezznaw/shop-idl" || cfg.GoPrivate {
		t.Errorf("unexpected template values: %+v", cfg)
	}
	// The common library is fixed; the template must not offer it as a setting.
	raw, _ := os.ReadFile(filepath.Join(dir, ConfigFile))
	if strings.Contains(string(raw), "common_repo") || cfg.CommonRepo != "" {
		t.Errorf("template must not mention common_repo:\n%s", raw)
	}
}

// The owner could not read devkit.yaml while the two languages ran into each
// other: an English comment line never touches a Chinese one, and no line
// carries an English sentence followed by Chinese.
func TestTemplateKeepsTheLanguagesApart(t *testing.T) {
	dir := t.TempDir()
	if err := WriteTemplate(dir, &Config{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	isCJK := func(r rune) bool {
		return (r >= 0x3000 && r <= 0x303f) || (r >= 0x4e00 && r <= 0x9fff) || (r >= 0xff00 && r <= 0xffef)
	}
	commentedKey := regexp.MustCompile(`^#\s{0,3}[A-Za-z_]+:(\s|$)`)
	words := regexp.MustCompile(`[A-Za-z]{2,}`)
	kind := func(line string) string {
		s := strings.TrimSpace(line)
		switch {
		case !strings.HasPrefix(s, "#"):
			return "none"
		case strings.Trim(s, "#-= ") == "":
			return "blank"
		case strings.IndexFunc(s, isCJK) >= 0:
			return "zh"
		case commentedKey.MatchString(s):
			return "key"
		}
		// An example (a URL, a path, a command) reads the same in both
		// languages; English is a line with a few plain words in it.
		plain := 0
		for _, w := range strings.Fields(strings.TrimLeft(s, "# ")) {
			if !strings.ContainsAny(w, "./:=_$<>{}\"`-") && words.MatchString(w) {
				plain++
			}
		}
		if plain < 3 {
			return "example"
		}
		return "en"
	}
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		if i > 0 {
			a, b := kind(lines[i-1]), kind(l)
			if (a == "en" && b == "zh") || (a == "zh" && b == "en") {
				t.Errorf("line %d: the English and the Chinese comment touch:\n%s\n%s", i+1, lines[i-1], l)
			}
		}
		if at := strings.IndexFunc(l, isCJK); at > 0 {
			before := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l[:at]), "# "))
			if len(words.FindAllString(before, -1)) >= 4 && strings.ContainsAny(before[len(before)-1:], ".:;)") {
				t.Errorf("line %d: English and Chinese on one line:\n%s", i+1, l)
			}
		}
	}
}
