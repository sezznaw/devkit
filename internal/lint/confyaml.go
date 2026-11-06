package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// confYAMLRule: every conf/*.yaml of the service parses the way the service
// reads it at start-up (devkit-common config.LoadFile: ${VAR} and
// ${VAR:-default} expanded, then yaml.v3, which also refuses a key written
// twice in one mapping). A file that does not parse never lets the service
// start, and the rules that read conf (callers-coverage, rpc-uid-from-ctx)
// skip such a file; before this rule the pipeline stayed green and the
// release did not come up (2026-11-05: `allow` written twice in one
// callers rule of a service's conf/dev.yaml).
//
// conf-yaml：服务的每份 conf/*.yaml 都要能按服务启动时的方式解析（devkit-common config.LoadFile：先展开
// ${VAR} / ${VAR:-默认值}，再用 yaml.v3 解析，同一层写两遍的键也算错）。解析不了服务就起不来，而读 conf 的
// 规则（callers-coverage、rpc-uid-from-ctx）会跳过这样的文件；以前流水线全绿、发布后起不来（2026-11-05
// 某服务 conf/dev.yaml 的一条 callers 规则写了两个 allow）。
const confYAMLRule = "conf-yaml"

var yamlLine = regexp.MustCompile(`line (\d+)`)

func checkConfYAML(root string) []Finding {
	files, _ := filepath.Glob(filepath.Join(root, "conf", "*.yaml"))
	sort.Strings(files)
	var out []Finding
	for _, f := range files {
		rel := filepath.ToSlash(filepath.Join("conf", filepath.Base(f)))
		b, err := os.ReadFile(f)
		if err != nil {
			out = append(out, Finding{rel, 0, confYAMLRule, fmt.Sprintf("cannot read the file: %v", err)})
			continue
		}
		var v any
		if err := yaml.Unmarshal([]byte(os.Expand(string(b), expandConf)), &v); err != nil {
			msg := strings.Join(strings.Fields(strings.ReplaceAll(err.Error(), "\n", "; ")), " ")
			out = append(out, Finding{rel, firstYAMLLine(err), confYAMLRule, fmt.Sprintf(
				"the service cannot read this file and will not start: %s. Fix the YAML (the usual cause is a key written twice in one block, or a wrong indentation)", msg)})
		}
	}
	return out
}

// expandConf is devkit-common config.Expand: ${VAR:-default} takes the
// default when VAR is unset or empty, ${VAR} the variable's value.
func expandConf(key string) string {
	if name, def, ok := strings.Cut(key, ":-"); ok {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return def
	}
	return os.Getenv(key)
}

// firstYAMLLine is the first "line N" yaml.v3 put in its error, 0 when none.
func firstYAMLLine(err error) int {
	m := yamlLine.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
