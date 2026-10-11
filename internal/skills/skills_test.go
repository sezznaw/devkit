package skills

import (
	"regexp"
	"strings"
	"testing"
)

func TestBuiltinSkillsAreWellFormedAndProjectFree(t *testing.T) {
	names := Names()
	if len(names) < 4 {
		t.Fatalf("names: %v", names)
	}
	leak := regexp.MustCompile(`(?i)sportsbook|\bser-[a-z]+|gitlab\.sk1k2b|turtle`)
	for _, n := range names {
		files, ok := Files(n)
		if !ok || files["SKILL.md"] == nil {
			t.Fatalf("%s: no SKILL.md", n)
		}
		md := string(files["SKILL.md"])
		if !strings.HasPrefix(md, "---\nname: "+n+"\n") {
			t.Errorf("%s: frontmatter name must equal the directory", n)
		}
		if Description(n) == "" {
			t.Errorf("%s: no description", n)
		}
		if m := leak.FindString(md); m != "" {
			t.Errorf("%s mentions a project (%q); built-in skills are project independent", n, m)
		}
		if !strings.HasPrefix(n, "devkit-") {
			t.Errorf("%s: built-in skills are prefixed devkit-", n)
		}
	}
}
