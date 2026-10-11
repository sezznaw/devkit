package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestErrorDir(t *testing.T) {
	root := t.TempDir()
	idl := filepath.Join(root, "idl", "errors")
	os.MkdirAll(idl, 0o755)
	os.MkdirAll(filepath.Join(root, "svc", "handler"), 0o755)
	w := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(idl, "member.thrift"), "namespace go errcode\n\n// 会员不存在\nconst i32 MemberNotFound = 2001\nconst i32 NoComment = 2002\n// 越界\nconst i32 Stray = 3001\n")
	w(filepath.Join(idl, "wallet.thrift"), "namespace go errcode\n\n// 重复的号\nconst i32 Again = 2001\n")
	w(filepath.Join(root, "svc", "handler", "x.go"), "package handler\nfunc f() { _ = kerrors.NewBizStatusError(2001, \"x\"); _ = kerrors.NewBizStatusError(2999, \"y\") }\nconst CodeOld int32 = 2002\n")
	ranges := map[string]string{"member": "2000-2999", "wallet": "3000-3999"}
	fs := checkErrorDir(filepath.Join(root, "svc"), []string{"handler/x.go"}, idl, ranges)
	got := map[string]int{}
	for _, f := range fs {
		got[f.Rule]++
	}
	want := map[string]int{"error-comment": 1, "error-range": 2, "error-dup": 1, "error-code": 3}
	for r, n := range want {
		if got[r] != n {
			t.Fatalf("%s: want %d got %d: %v", r, n, got[r], fs)
		}
	}
}
