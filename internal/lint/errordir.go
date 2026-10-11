package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Business codes are declared once, in the IDL repository's errors/
// directory: one Thrift file per domain, `const i32 Name = code` with a
// comment line above it. make gen turns them into every service's
// kitex_gen/errcode constants and apidoc writes the table into the
// gateway's OpenAPI document for the front ends. These rules keep that
// directory sound and keep bare numbers out of the Go code.

var (
	errConstRE   = regexp.MustCompile(`^\s*const\s+i32\s+(\w+)\s*=\s*(\d+)`)
	bareBizCode  = regexp.MustCompile(`NewBizStatusError\(\s*(\d{3,5})\s*,`)
	bareCodeDecl = regexp.MustCompile(`\bCode\w*\s*(?:int32\s*)?=\s*(\d{3,5})\b`)
)

type errCode struct {
	name, file string
	line       int
}

func checkErrorDir(root string, goFiles []string, dir string, ranges map[string]string) []Finding {
	files, _ := filepath.Glob(filepath.Join(dir, "*.thrift"))
	if len(files) == 0 {
		return nil
	}
	sort.Strings(files)
	var out []Finding
	codes := map[int]errCode{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(f), ".thrift")
		rel := filepath.ToSlash(filepath.Join(filepath.Base(dir), filepath.Base(f)))
		lines := strings.Split(string(data), "\n")
		allowed, hasRange := parseRanges(ranges[base])
		for i, line := range lines {
			m := errConstRE.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			code, _ := strconv.Atoi(m[2])
			if prev, dup := codes[code]; dup && !ignored(line, "error-dup") {
				out = append(out, Finding{rel, i + 1, "error-dup", fmt.Sprintf("%d is already %s in %s: one meaning per code, never reused", code, prev.name, prev.file)})
			} else {
				codes[code] = errCode{m[1], rel, i + 1}
			}
			if hasRange && !inRanges(code, allowed) && !ignored(line, "error-range") {
				out = append(out, Finding{rel, i + 1, "error-range", fmt.Sprintf("%s = %d is outside this file's range %s (lint.error_ranges in idl/devkit.yaml): a code of another domain goes to that domain's file", m[1], code, ranges[base])})
			}
			if (i == 0 || !strings.HasPrefix(strings.TrimSpace(lines[i-1]), "//")) && !ignored(line, "error-comment") {
				out = append(out, Finding{rel, i + 1, "error-comment", fmt.Sprintf("%s has no comment line above it: the comment is the message the front end shows and the documentation prints", m[1])})
			}
		}
	}
	// Bare numbers in Go: the code exists as a generated constant, use it.
	for _, rel := range goFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			if ignored(line, "error-code") {
				continue
			}
			for _, re := range []*regexp.Regexp{bareBizCode, bareCodeDecl} {
				for _, m := range re.FindAllStringSubmatch(line, -1) {
					n, _ := strconv.Atoi(m[1])
					if c, ok := codes[n]; ok {
						out = append(out, Finding{rel, i + 1, "error-code", fmt.Sprintf("%d is errcode.%s (idl/%s): use the generated constant, not the number", n, c.name, c.file)})
					} else {
						out = append(out, Finding{rel, i + 1, "error-code", fmt.Sprintf("business code %d is not declared: add `const i32 <Name> = %d` with a comment to idl/%s/<domain>.thrift (one meaning per code, never reused), run make gen, use errcode.<Name>", n, n, filepath.Base(dir))})
					}
				}
			}
		}
	}
	return out
}

type codeRange struct{ lo, hi int }

func parseRanges(spec string) ([]codeRange, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, false
	}
	var out []codeRange
	for _, part := range strings.Split(spec, ",") {
		lohi := strings.SplitN(strings.TrimSpace(part), "-", 2)
		if len(lohi) != 2 {
			continue
		}
		lo, err1 := strconv.Atoi(strings.TrimSpace(lohi[0]))
		hi, err2 := strconv.Atoi(strings.TrimSpace(lohi[1]))
		if err1 == nil && err2 == nil {
			out = append(out, codeRange{lo, hi})
		}
	}
	return out, len(out) > 0
}

func inRanges(code int, rs []codeRange) bool {
	for _, r := range rs {
		if code >= r.lo && code <= r.hi {
			return true
		}
	}
	return false
}
