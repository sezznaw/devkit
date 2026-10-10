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

func TestNoEnvelope(t *testing.T) {
	good := `include "../common/common.thrift"
struct Pong {
    1: string message
}
struct Profile {
    1: i64 uid
}
service S {
    // ping
    Pong Ping(1: PingReq req) (api.get="/ping")
    // out
    common.Empty Logout(1: LogoutReq req) (api.post="/v1/auth/logout")
    // profile
    Profile GetProfile(1: GetProfileReq req) (api.post="/v1/member/getProfile")
}
`
	for _, api := range []bool{true, false} {
		if fs := rules(lintIDL(t, api, good), "no-envelope"); len(fs) != 0 {
			t.Fatalf("api=%v: good flagged: %v", api, fs)
		}
	}
	bad := `struct PingResp {
    1: i32 code
    2: string msg
    3: optional Pong data
}
struct Other {
    1: i32 code
}
struct Fine {
    1: i32 code  //devkit:lint-ignore no-envelope
}
service S {
    // ping
    PingResp Ping(1: PingReq req) (api.get="/ping")
    // other
    Other Get(1: GetReq req) (api.post="/v1/x/get")
    // fine
    Fine Fine(1: FineReq req) (api.post="/v1/x/fine")
}
`
	for _, api := range []bool{true, false} {
		fs := rules(lintIDL(t, api, bad), "no-envelope")
		if len(fs) != 3 { // PingResp.code, PingResp.msg, Other.code
			t.Fatalf("api=%v: want 3, got %d: %v", api, len(fs), fs)
		}
	}
}
