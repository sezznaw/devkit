package lint

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Every rule name a Finding can carry is listed in Rules, exactly once.
func TestRulesListEveryFindingName(t *testing.T) {
	listed := map[string]int{}
	for _, r := range Rules {
		listed[r.Name]++
		if r.Summary == "" {
			t.Errorf("%s has no summary", r.Name)
		}
	}
	for name, n := range listed {
		if n > 1 {
			t.Errorf("%s listed %d times", name, n)
		}
	}
	quoted := regexp.MustCompile(`"([a-z][a-z-]+)"`)
	files, _ := filepath.Glob("*.go")
	used := map[string]bool{}
	for _, f := range files {
		if filepath.Base(f) == "rules.go" || filepath.Ext(f[:len(f)-3]) == "_test" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`Finding\{[^}]*\}`).FindAll(src, -1) {
			for _, q := range quoted.FindAllSubmatch(m, -1) {
				used[string(q[1])] = true
			}
		}
	}
	for name := range used {
		if listed[name] == 0 {
			t.Errorf("Finding uses rule %q but Rules does not list it", name)
		}
	}
}
