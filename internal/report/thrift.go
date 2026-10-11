package report

import (
	"os"
	"regexp"
	"strings"
)

// IDLChanges is what changed in the Thrift files of the IDL repository,
// read from the diff so the report names interfaces, not line numbers.
type IDLChanges struct {
	Methods []IDLMethod `json:"methods,omitempty"`
	Structs []IDLItem   `json:"structs,omitempty"`
	Codes   []IDLCode   `json:"codes,omitempty"`
	Removed []IDLItem   `json:"removed,omitempty"`
	// Flags are added annotation lines that a person must have agreed to
	// (@public, @perm, @limit, @audit) with the file they appear in.
	Flags []IDLItem `json:"flags,omitempty"`
}

// IDLMethod is one added service method.
type IDLMethod struct {
	File    string `json:"file"`
	Service string `json:"service,omitempty"`
	Name    string `json:"name"`
	Route   string `json:"route,omitempty"`
}

// IDLItem is a named thing in a file (struct, removed line, annotation).
type IDLItem struct {
	File string `json:"file"`
	Kind string `json:"kind,omitempty"`
	Name string `json:"name"`
}

// IDLCode is one added error code with the comment line above it, which
// is the message the frontend shows.
type IDLCode struct {
	File    string `json:"file"`
	Name    string `json:"name"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

var (
	reDiffFile  = regexp.MustCompile(`^\+\+\+ b/(.+)$`)
	reMethod    = regexp.MustCompile(`^([+-])\s*(?:oneway\s+)?([A-Za-z_][\w.]*(?:<[^>]*>)?)\s+([A-Za-z_]\w*)\s*\(`)
	reRoute     = regexp.MustCompile(`api\.(?:post|get|put|delete|patch)\s*=\s*"([^"]+)"`)
	reStruct    = regexp.MustCompile(`^([+-])\s*(struct|union|enum|exception|typedef)\s+(?:[\w.<>, ]+\s+)?([A-Za-z_]\w*)`)
	reConst     = regexp.MustCompile(`^([+-])\s*const\s+i32\s+([A-Za-z_]\w*)\s*=\s*(\d+)`)
	reComment   = regexp.MustCompile(`^\+\s*(?://+|#)\s*(.*?)\s*$`)
	reFlag      = regexp.MustCompile(`^\+.*(@public|@perm\s+[\w.]+|@limit\s+\S+|@audit\b)`)
	reServiceLn = regexp.MustCompile(`^\s*service\s+([A-Za-z_]\w*)`)
	keywords    = map[string]bool{"struct": true, "service": true, "const": true, "include": true, "namespace": true, "enum": true, "typedef": true, "exception": true, "union": true, "throws": true, "extends": true, "required": true, "optional": true}
)

// parseThriftDiff reads a unified diff (git diff -U0) of thrift files.
// serviceOf resolves which service a method belongs to by reading the
// current file; it may be nil.
func parseThriftDiff(diff string, serviceOf func(file, method string) string) *IDLChanges {
	ch := &IDLChanges{}
	file := ""
	lastComment := ""
	for _, line := range strings.Split(diff, "\n") {
		if m := reDiffFile.FindStringSubmatch(line); m != nil {
			file = m[1]
			lastComment = ""
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") {
			continue
		}
		if file == "" || !strings.HasSuffix(file, ".thrift") {
			continue
		}
		if m := reComment.FindStringSubmatch(line); m != nil {
			lastComment = m[1]
			if f := reFlag.FindStringSubmatch(line); f != nil {
				ch.Flags = append(ch.Flags, IDLItem{File: file, Kind: "annotation", Name: strings.TrimSpace(f[1])})
			}
			continue
		}
		if m := reConst.FindStringSubmatch(line); m != nil {
			code := 0
			for _, c := range m[3] {
				code = code*10 + int(c-'0')
			}
			if m[1] == "+" {
				ch.Codes = append(ch.Codes, IDLCode{File: file, Name: m[2], Code: code, Message: lastComment})
			} else {
				ch.Removed = append(ch.Removed, IDLItem{File: file, Kind: "const", Name: m[2]})
			}
			lastComment = ""
			continue
		}
		if m := reStruct.FindStringSubmatch(line); m != nil {
			if m[1] == "+" {
				ch.Structs = append(ch.Structs, IDLItem{File: file, Kind: m[2], Name: m[3]})
			} else {
				ch.Removed = append(ch.Removed, IDLItem{File: file, Kind: m[2], Name: m[3]})
			}
			lastComment = ""
			continue
		}
		if m := reMethod.FindStringSubmatch(line); m != nil && !keywords[m[2]] && !strings.HasPrefix(strings.TrimSpace(line[1:]), "service") {
			if m[1] == "+" {
				meth := IDLMethod{File: file, Name: m[3]}
				if r := reRoute.FindStringSubmatch(line); r != nil {
					meth.Route = r[1]
				}
				if serviceOf != nil {
					meth.Service = serviceOf(file, m[3])
				}
				ch.Methods = append(ch.Methods, meth)
			} else {
				ch.Removed = append(ch.Removed, IDLItem{File: file, Kind: "method", Name: m[3]})
			}
			lastComment = ""
			continue
		}
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			lastComment = ""
		}
	}
	// Something removed here and added somewhere (edited signature, or moved
	// to another file in a split) is a change, not a removal.
	added := map[string]bool{}
	for _, m := range ch.Methods {
		added["method/"+m.Name] = true
	}
	for _, s := range ch.Structs {
		added[s.Kind+"/"+s.Name] = true
	}
	for _, c := range ch.Codes {
		added["const/"+c.Name] = true
	}
	var removed []IDLItem
	for _, r := range ch.Removed {
		if !added[r.Kind+"/"+r.Name] {
			removed = append(removed, r)
		}
	}
	ch.Removed = removed
	// The same annotation in one file once.
	seen := map[string]bool{}
	var flags []IDLItem
	for _, f := range ch.Flags {
		k := f.File + "/" + f.Name
		if !seen[k] {
			seen[k] = true
			flags = append(flags, f)
		}
	}
	ch.Flags = flags
	return ch
}

// serviceOfMethod finds the `service X {` that encloses the first
// definition of method in the thrift file at path.
func serviceOfMethod(path, method string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	service := ""
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(method) + `\s*\(`)
	for _, line := range strings.Split(string(b), "\n") {
		if m := reServiceLn.FindStringSubmatch(line); m != nil {
			service = m[1]
			continue
		}
		if service != "" && re.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
			return service
		}
	}
	return ""
}

// Empty says whether nothing changed in the IDL.
func (c *IDLChanges) Empty() bool {
	return c == nil || len(c.Methods)+len(c.Structs)+len(c.Codes)+len(c.Removed)+len(c.Flags) == 0
}
