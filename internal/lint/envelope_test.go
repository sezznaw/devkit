package lint

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func lintIDL(t *testing.T, api bool, src string) []Finding {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.thrift")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	fs, err := checkIDL(p, api, regexp.MustCompile(`^$`))
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func rules(fs []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestRespShape(t *testing.T) {
	good := `struct Pong {
    1: string message
}
struct PingResp {
    1: i32 code
    2: string msg
    3: optional Pong data
}
struct LogoutResp {
    1: i32 code
    2: string msg
}
`
	if fs := rules(lintIDL(t, true, good), "resp-shape"); len(fs) != 0 {
		t.Fatalf("good shapes flagged: %v", fs)
	}
	bad := `struct TokenResp {
    1: i32 code
    2: string msg
    3: optional string token
}
struct PlainResp {
    1: string message
}
struct ListResp {
    1: i32 code
    2: string msg
    3: optional list<Item> data
}
struct ReqResp {
    1: i32 code
    2: string msg
    3: Item data
}
struct OkResp {
    1: i32 code
    2: string msg
    3: optional Item data  //devkit:lint-ignore resp-shape
}
`
	fs := rules(lintIDL(t, true, bad), "resp-shape")
	if len(fs) != 4 {
		t.Fatalf("want 4 findings, got %d: %v", len(fs), fs)
	}
	// RPC side: the same structs are fine except for code/msg.
	rpc := rules(lintIDL(t, false, good), "rpc-no-envelope")
	if len(rpc) != 4 { // PingResp code+msg, LogoutResp code+msg
		t.Fatalf("rpc-no-envelope: want 4, got %d: %v", len(rpc), rpc)
	}
	if fs := rules(lintIDL(t, false, "struct GetResp {\n    1: Profile profile\n}\nstruct ListResp {\n    1: list<Profile> items\n    2: i64 total\n}\n"), "rpc-no-envelope"); len(fs) != 0 {
		t.Fatalf("plain rpc flagged: %v", fs)
	}
}
