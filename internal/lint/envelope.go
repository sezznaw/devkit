package lint

import (
	"fmt"
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

// checkRespShapes: a response carries data only, gateway or RPC.
//
// The {code, msg, data} envelope is not written in the IDL. A gateway method
// returns its data struct (common.Empty when there is none) and the handler
// ends with hertzx.OK / Fail, which add the envelope once, at the edge;
// apidoc documents it. An RPC method returns its data struct and a business
// failure is a BizStatusError. A code or msg field in a response struct
// would be a second error channel every caller has to check.
func checkRespShapes(rel string, shapes []respShape, api bool) []Finding {
	var out []Finding
	for _, sh := range shapes {
		for _, f := range sh.fields {
			if (f.name == "code" || f.name == "msg") && !f.ignored {
				how := "a business failure is `return nil, kerrors.NewBizStatusError(code, msg)`"
				if api {
					how = "the handler ends with hertzx.OK(c, &data) or hertzx.Fail(ctx, c, err), which add {code, msg, data} once; the method returns its data struct (common.Empty when there is none)"
				}
				out = append(out, Finding{rel, f.line, "no-envelope", fmt.Sprintf("%s.%s: a response struct carries data only; %s", sh.name, f.name, how)})
			}
		}
	}
	return out
}
