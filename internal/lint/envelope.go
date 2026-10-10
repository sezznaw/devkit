package lint

import (
	"fmt"
	"strings"
)

// respShape is a *Resp struct as the IDL declares it.
type respShape struct {
	name   string
	line   int
	fields []respField
}

type respField struct {
	id       string
	optional bool
	typ      string
	name     string
	line     int
	ignored  bool
}

// checkRespShapes: one envelope, decided once for everybody.
//
// A gateway (api) Resp is exactly `1: i32 code  2: string msg` and, when
// the method returns something, `3: optional <Struct> data`: no other
// field, no other name, no other order, data a struct (a string or a list
// is wrapped so the client always reads data.<field>). The same shape for
// /ping and for the richest report.
//
// An RPC Resp carries data only. A business failure is a BizStatusError
// (kerrors.NewBizStatusError); the gateway turns it into the envelope's
// code and msg once, at the edge. A code or msg field in an RPC Resp would
// open a second error channel every caller has to check.
func checkRespShapes(rel string, shapes []respShape, api bool) []Finding {
	var out []Finding
	for _, sh := range shapes {
		if api {
			if msg := envelopeProblem(sh); msg != "" && !shapeIgnored(sh) {
				out = append(out, Finding{rel, sh.line, "resp-shape", fmt.Sprintf("%s: %s; every gateway response is `1: i32 code  2: string msg  3: optional <Struct> data` (idl/README.md 响应壳)", sh.name, msg)})
			}
			continue
		}
		for _, f := range sh.fields {
			if (f.name == "code" || f.name == "msg") && !f.ignored {
				out = append(out, Finding{rel, f.line, "rpc-no-envelope", fmt.Sprintf("%s.%s: an RPC response carries data only; a business failure is `return nil, kerrors.NewBizStatusError(code, msg)` and the gateway turns it into the {code, msg, data} envelope once, at the edge", sh.name, f.name)})
			}
		}
	}
	return out
}

func shapeIgnored(sh respShape) bool {
	for _, f := range sh.fields {
		if f.ignored {
			return true
		}
	}
	return false
}

// envelopeProblem says what is wrong with a gateway Resp, "" when nothing.
func envelopeProblem(sh respShape) string {
	fs := sh.fields
	if len(fs) < 2 || len(fs) > 3 {
		return fmt.Sprintf("has %d field(s), the envelope has 2 or 3", len(fs))
	}
	if fs[0].id != "1" || fs[0].name != "code" || fs[0].typ != "i32" || fs[0].optional {
		return fmt.Sprintf("field 1 is `%s: %s %s`, must be `1: i32 code`", fs[0].id, fs[0].typ, fs[0].name)
	}
	if fs[1].id != "2" || fs[1].name != "msg" || fs[1].typ != "string" || fs[1].optional {
		return fmt.Sprintf("field 2 is `%s: %s %s`, must be `2: string msg`", fs[1].id, fs[1].typ, fs[1].name)
	}
	if len(fs) == 3 {
		d := fs[2]
		switch {
		case d.id != "3" || d.name != "data":
			return fmt.Sprintf("field 3 is `%s: %s %s`, must be `3: optional <Struct> data`", d.id, d.typ, d.name)
		case !d.optional:
			return "data must be optional (absent on failure)"
		case isScalarOrContainer(d.typ):
			return fmt.Sprintf("data is %s: wrap it in a struct (e.g. `struct %s { 1: %s value }`) so clients always read data.<field>", d.typ, strings.TrimSuffix(sh.name, "Resp"), d.typ)
		}
	}
	return ""
}

func isScalarOrContainer(typ string) bool {
	switch typ {
	case "bool", "byte", "i8", "i16", "i32", "i64", "double", "string", "binary":
		return true
	}
	return strings.HasPrefix(typ, "list<") || strings.HasPrefix(typ, "map<") || strings.HasPrefix(typ, "set<")
}
