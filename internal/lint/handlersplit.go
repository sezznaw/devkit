package lint

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// An RPC service's handler is one package (Kitex wants one struct for the
// whole service, and Go keeps a type's methods in one package), so it is
// split by file: handler.go holds the struct, New and Ping; every other RPC
// method lives in a topic file (profile.go, credential.go, status.go, ...)
// together with the event consumers and delayed-task handlers of that topic,
// and codes.go holds the business codes. kitex never rewrites handler/, so
// the methods can be anywhere; this rule keeps them where a reader expects.

// rpcMethod: `func (s *XxxImpl) Name(ctx context.Context, req *api.NameReq)`.
var rpcMethod = regexp.MustCompile(`^func \(\w+ \*\w+\) (\w+)\(ctx context\.Context, req \*\w+\.\w+\)`)

const defaultHandlerMethods = 8

func checkHandlerSplit(root string, limit int) []Finding {
	if limit <= 0 {
		limit = defaultHandlerMethods
	}
	files, _ := filepath.Glob(filepath.Join(root, "handler", "*.go"))
	sort.Strings(files)
	var out []Finding
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("handler", filepath.Base(f)))
		methods, lines := rpcMethods(f)
		if filepath.Base(f) == "handler.go" {
			for i, m := range methods {
				if m != "Ping" && !ignored(lines[i], "handler-split") {
					out = append(out, Finding{rel, lineOf(lines[i]), "handler-split", fmt.Sprintf("%s: handler.go holds the struct, New and Ping only; an RPC method goes to the topic file it belongs to (handler/<topic>.go, a noun: profile.go, credential.go, status.go), with the event consumers and delayed-task handlers of that topic", m)})
				}
			}
			continue
		}
		if len(methods) > limit {
			out = append(out, Finding{rel, 0, "handler-split", fmt.Sprintf("%d RPC methods in one file (the limit is %d, lint.handler_methods in idl/devkit.yaml): split it by topic, one noun per file", len(methods), limit)})
		}
	}
	return out
}

// rpcMethods lists the RPC method names of a Go file and, for each, the
// line it starts on (encoded as "<n>:<text>" for ignored and lineOf).
func rpcMethods(path string) (names, lines []string) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		if m := rpcMethod.FindStringSubmatch(sc.Text()); m != nil {
			names = append(names, m[1])
			lines = append(lines, fmt.Sprintf("%d:%s", n, sc.Text()))
		}
	}
	return names, lines
}

func lineOf(tagged string) int {
	var n int
	fmt.Sscanf(tagged, "%d:", &n)
	return n
}
