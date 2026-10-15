package lint

import "testing"

func TestRequestIDSource(t *testing.T) {
	for _, tc := range []struct {
		rel, code string
		bad       bool
	}{
		{"handler/a.go", `deps.Admin.CreateRole(ctx, &ser_admin.CreateRoleReq{RequestId: req.RequestID, Name: req.Name})`, false},
		{"handler/a.go", `r := &x.DebitReq{RequestId: req.GetRequestId(), Amount: a}`, false},
		{"handler/a.go", `RequestId: kitexx.RequestID(ctx),`, false},
		{"handler/a.go", `RequestId: kitexx.SubRequestID(ctx, "fee"), Amount: fee}`, false},
		{"handler/a.go", `in.RequestId = kitexx.SubRequestID(ctx, "stake")`, false},
		{"handler/a.go", `if r.RequestId == "" {`, false},
		{"handler/a.go", `RequestId string ` + "`json:\"request_id\"`", false},
		{"app/verify.go", `RequestId: "verify",`, false},
		{"handler/a.go", `RequestId: uuid.NewString(), Amount: a}`, true},
		{"handler/a.go", `RequestId: "fixed",`, true},
		{"handler/a.go", `RequestId: fmt.Sprintf("%d", time.Now().UnixNano()),`, true},
		{"repo/a.go", `in.RequestId = strconv.FormatInt(rt.ID.Next(), 10)`, true},
		{"handler/a.go", `RequestId: req.RequestId + ":fee",`, true},
		{"handler/a.go", `RequestId: id,`, true},
	} {
		if _, bad := checkRequestIDSource(tc.rel, tc.code); bad != tc.bad {
			t.Errorf("%s %q: flagged %v, want %v", tc.rel, tc.code, bad, tc.bad)
		}
	}
}
