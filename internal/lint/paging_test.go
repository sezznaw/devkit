package lint

import "testing"

func TestPagingRule(t *testing.T) {
	bad := `include "../common/common.thrift"
namespace go member

struct ListReq {
    1: i32 page
    2: i32 page_size
}
struct MemberList {
    1: list<Profile> items
    2: i64 total
}
struct Stats {
    1: i64 total
}
`
	fs := rules(lintIDL(t, true, bad), "paging")
	if len(fs) != 3 { // page, page_size, MemberList.total; Stats.total is a statistic, not paging
		t.Fatalf("want 3 paging findings, got %d: %v", len(fs), fs)
	}
	good := `include "../common/common.thrift"
namespace go member

struct ListReq {
    1: common.PageReq page
}
struct MemberList {
    1: list<Profile> items
    2: common.PageInfo page
}
`
	if fs := rules(lintIDL(t, true, good), "paging"); len(fs) != 0 {
		t.Fatalf("good flagged: %v", fs)
	}
}
