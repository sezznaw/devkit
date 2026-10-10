package lint

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sezznaw/devkit/internal/manifest"
)

// checkCommonVersion: the common library's version in go.mod is the team's
// (the template decides it; devkit update moves it). A `go get -u` or a
// hand edit that moves it makes this service build against a library the
// others do not have.
func checkCommonVersion(root string, m *manifest.Manifest) []Finding {
	module, want := "", ""
	for _, c := range m.Components {
		if v := c.Vars["CommonModule"]; v != "" {
			module = v
		}
		if v := c.Vars["CommonVersion"]; v != "" {
			want = v
		}
	}
	if module == "" || want == "" {
		return nil
	}
	have, line := goModRequire(filepath.Join(root, "go.mod"), module)
	if have == "" || have == want {
		return nil
	}
	return []Finding{{"go.mod", line, "common-version", "the common library " + module + " is " + have + " here but the team's version is " + want + ": do not go get or edit it by hand, run devkit update (the template moves every service together)"}}
}

// goModRequire returns the version go.mod requires for module and the line
// it is on; "" when the module is not required.
func goModRequire(path, module string) (string, int) {
	fh, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	n := 0
	for sc.Scan() {
		n++
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(sc.Text()), "require "))
		if len(fields) >= 2 && fields[0] == module {
			return fields[1], n
		}
	}
	return "", 0
}

// checkCommonCheckout: the project's kit-common/ checkout is what go.work
// makes every local build use; an uncommitted change there makes "works
// on my machine" differ from CI, which builds against go.mod.
func checkCommonCheckout(projectDir string) []Finding {
	if projectDir == "" {
		return nil
	}
	dir := filepath.Join(projectDir, "kit-common")
	if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !st.IsDir() {
		return nil
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return nil
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	first := strings.TrimSpace(files[0])
	if i := strings.IndexByte(first, ' '); i > 0 {
		first = strings.TrimSpace(first[i:])
	}
	more := ""
	if len(files) > 1 {
		more = " and " + itoa(len(files)-1) + " more"
	}
	return []Finding{{"kit-common/" + first, 0, "common-checkout", "the project's kit-common/ checkout has local changes (" + first + more + "); go.work makes your build use them while CI builds against the released version. Change the framework in its own repository and release it, then devkit update; `git -C kit-common checkout .` discards the edit"}}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
