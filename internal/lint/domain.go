package lint

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// A gateway's IDL directory holds one file per domain. The file name, its
// namespace (hz makes a handler package of it) and the domain segment of
// its routes (/v1/<domain>/...) are one word, so a reader finds everything
// about a domain by that word, and two colleagues never edit the same file.
// A domain uses its own structs; what is shared lives in common.thrift.
var (
	namespaceLine = regexp.MustCompile(`^\s*namespace\s+go\s+([\w.]+)`)
	includeLine   = regexp.MustCompile(`^\s*include\s+"([^"]+)"`)
	routePath     = regexp.MustCompile(`api\.(?:get|post|put|delete|patch)\s*=\s*"([^"]+)"`)
	versionedPath = regexp.MustCompile(`^/v\d+/([^/]+)/`)
	goWord        = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
)

// reservedPackage: a namespace that would shadow a package every handler
// imports, or read like one.
var reservedPackage = map[string]bool{"context": true, "app": true, "deps": true, "router": true, "handler": true, "common": true, "hertzx": true, "kitexx": true, "zlog": true, "time": true, "sync": true, "fmt": true, "errors": true, "strings": true, "main": true, "test": true}

func checkDomainFile(rel string, lines []string) []Finding {
	var out []Finding
	base := strings.TrimSuffix(filepath.Base(rel), ".thrift")
	ns, nsLine := "", 0
	hasService := false
	for i, line := range lines {
		if m := namespaceLine.FindStringSubmatch(line); m != nil && ns == "" {
			ns, nsLine = m[1], i+1
		}
		if strings.HasPrefix(strings.TrimSpace(line), "service ") {
			hasService = true
		}
		if m := includeLine.FindStringSubmatch(line); m != nil && !ignored(line, "domain-include") {
			inc := m[1]
			if !strings.HasPrefix(inc, "../") || !strings.Contains(inc, "/common/") {
				out = append(out, Finding{rel, i + 1, "domain-include", fmt.Sprintf("includes %s: a domain uses its own structs; what two domains share goes to ../common/common.thrift (the only include a gateway file has)", inc)})
			}
		}
	}
	if !hasService {
		return out // docs.thrift or a structs-only file
	}
	switch {
	case ns == "":
		out = append(out, Finding{rel, 1, "domain-file", fmt.Sprintf("no `namespace go %s`: the namespace is the handler package hz generates (handler/%s/), one word, the same as the file name", base, base)})
	case ns != base || !goWord.MatchString(ns) || reservedPackage[ns]:
		out = append(out, Finding{rel, nsLine, "domain-file", fmt.Sprintf("namespace go %s: file name, namespace and handler package are one lowercase word (file %s.thrift -> `namespace go %s` -> handler/%s/), not a Go keyword or a package name every handler imports", ns, base, base, base)})
	}
	for i, line := range lines {
		m := routePath.FindStringSubmatch(line)
		if m == nil || ignored(line, "domain-path") {
			continue
		}
		if v := versionedPath.FindStringSubmatch(m[1]); v != nil && ns != "" && v[1] != ns {
			out = append(out, Finding{rel, i + 1, "domain-path", fmt.Sprintf("%s: the route's domain segment is %q but this is the %s domain; routes are /v1/%s/<action> (a route of another domain goes to that domain's file)", m[1], v[1], ns, ns)})
		}
	}
	return out
}
