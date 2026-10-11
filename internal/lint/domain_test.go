package lint

import "testing"

func TestDomainRules(t *testing.T) {
	_, moneyName := moneyPattern(nil)
	good := `include "../common/common.thrift"
namespace go wallet

struct BalanceReq {}
struct Balance {
    1: common.Money balance
}
service WalletService {
    // 余额
    Balance GetBalance(1: BalanceReq req) (api.post="/v1/wallet/getBalance")
}
`
	p := writeIDL(t, "wallet.thrift", good)
	fs, _ := checkIDL(p, true, moneyName)
	for _, f := range fs {
		if f.Rule == "domain-file" || f.Rule == "domain-path" || f.Rule == "domain-include" {
			t.Fatalf("good domain flagged: %v", f)
		}
	}
	bad := `include "member.thrift"
namespace go ser_api

struct X {}
service WalletService {
    // 余额
    X GetBalance(1: X req) (api.post="/v1/member/getBalance")
    // ping
    X Ping(1: X req) (api.get="/ping")
}
`
	p = writeIDL(t, "wallet.thrift", bad)
	fs, _ = checkIDL(p, true, moneyName)
	want := map[string]int{"domain-file": 1, "domain-path": 1, "domain-include": 1}
	for _, f := range fs {
		if _, ok := want[f.Rule]; ok {
			want[f.Rule]--
		}
	}
	for r, n := range want {
		if n != 0 {
			t.Fatalf("%s: expected exactly one finding, remaining %d: %v", r, n, fs)
		}
	}
	// docs-only file: nothing to say
	p = writeIDL(t, "docs.thrift", "// @docs title: x\n")
	fs, _ = checkIDL(p, true, moneyName)
	if len(fs) != 0 {
		t.Fatalf("docs file flagged: %v", fs)
	}
	// RPC files are not domain files
	p = writeIDL(t, "ser-member.thrift", "namespace go ser_member\nstruct R {}\nservice S {\n    // x\n    R Get(1: R req)\n}\n")
	fs, _ = checkIDL(p, false, moneyName)
	for _, f := range fs {
		if f.Rule == "domain-file" {
			t.Fatalf("rpc flagged: %v", f)
		}
	}
}
