package lint

import (
	"path/filepath"
	"regexp"
	"strings"
)

// requestIDField finds a RequestId / RequestID being set: a field of a
// composite literal (`RequestId: x`) or an assignment (`r.RequestId = x`).
var requestIDField = regexp.MustCompile(`\bRequestI[dD]\s*(:|=)\s*`)

// requestIDSources are the values a request_id may take: the id of the
// request being handled (req.RequestId, req.GetRequestId()) or the kitexx
// functions built on it. A new UUID, a timestamp, a literal would make a
// retry of the request run the downstream write again.
var requestIDSources = regexp.MustCompile(`^(?:[\w.]+\.RequestI[dD]|[\w.]+\.GetRequestI[dD]\(\)|kitexx\.(?:Sub)?RequestID\(.*)$`)

// checkRequestIDSource reports a request_id set from anything else.
// app/verify.go is exempt: the post-deploy checks run outside any request
// and repeat a fixed id on purpose.
func checkRequestIDSource(rel, code string) (string, bool) {
	if filepath.ToSlash(rel) == "app/verify.go" {
		return "", false
	}
	for _, loc := range requestIDField.FindAllStringSubmatchIndex(code, -1) {
		// `==` is a comparison, `:=` never follows a field name
		if code[loc[2]:loc[3]] == "=" && strings.HasPrefix(code[loc[3]:], "=") {
			continue
		}
		v := valueAt(code[loc[1]:])
		if v == "" || requestIDSources.MatchString(v) {
			continue
		}
		return v, true
	}
	return "", false
}

// valueAt is the expression at the start of s, up to a comma, brace or
// closing parenthesis outside brackets.
func valueAt(s string) string {
	depth := 0
	for i, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return strings.TrimSpace(s[:i])
			}
			depth--
		case ',', ';':
			if depth == 0 {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return strings.TrimSpace(s)
}
