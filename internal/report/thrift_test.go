package report

import "testing"

const sampleDiff = `diff --git a/ser-member/ser-member.thrift b/ser-member/ser-member.thrift
--- a/ser-member/ser-member.thrift
+++ b/ser-member/ser-member.thrift
@@ -40,0 +41,4 @@
+struct UpdateAvatarReq {
+    1: string request_id
+    2: i64 uid
+}
@@ -80,0 +85,2 @@
+    // 更新头像
+    common.Empty UpdateAvatar(1: UpdateAvatarReq req)
@@ -90 +97 @@
-    MemberProfile GetProfile(1: GetProfileReq req)
+    MemberProfile GetProfile(1: GetProfileReq req, 2: bool full)
@@ -95 +102,0 @@
-    StatsResp OldStats(1: StatsReq req)
diff --git a/errors/member.thrift b/errors/member.thrift
--- a/errors/member.thrift
+++ b/errors/member.thrift
@@ -12,0 +13,2 @@
+// 头像太大
+const i32 MemberAvatarTooLarge = 2007
diff --git a/ser-api/member.thrift b/ser-api/member.thrift
--- a/ser-api/member.thrift
+++ b/ser-api/member.thrift
@@ -30,0 +31,3 @@
+    // 更新头像
+    // @public
+    common.Empty UpdateAvatar(1: UpdateAvatarReq req) (api.post="/v1/member/updateAvatar")
`

func TestParseThriftDiff(t *testing.T) {
	ch := parseThriftDiff(sampleDiff, func(file, method string) string { return "Svc" })
	if len(ch.Methods) != 3 {
		t.Fatalf("methods: %+v", ch.Methods)
	}
	if ch.Methods[0].Name != "UpdateAvatar" || ch.Methods[0].Service != "Svc" || ch.Methods[0].Route != "" {
		t.Errorf("rpc method: %+v", ch.Methods[0])
	}
	if ch.Methods[2].Route != "/v1/member/updateAvatar" {
		t.Errorf("route: %+v", ch.Methods[2])
	}
	if len(ch.Structs) != 1 || ch.Structs[0].Name != "UpdateAvatarReq" {
		t.Errorf("structs: %+v", ch.Structs)
	}
	if len(ch.Codes) != 1 || ch.Codes[0].Code != 2007 || ch.Codes[0].Message != "头像太大" {
		t.Errorf("codes: %+v", ch.Codes)
	}
	if len(ch.Removed) != 1 || ch.Removed[0].Name != "OldStats" {
		t.Errorf("removed (GetProfile is edited, OldStats removed): %+v", ch.Removed)
	}
	if len(ch.Flags) != 1 || ch.Flags[0].Name != "@public" {
		t.Errorf("flags: %+v", ch.Flags)
	}
}

// The template's /ping sample is @public by design: a new gateway must not
// raise "confirm this @public with somebody". An @public on a real method
// in the same file still does.
func TestPublicOnPingIsNotFlagged(t *testing.T) {
	diff := `diff --git a/ser-x-api/system.thrift b/ser-x-api/system.thrift
--- a/ser-x-api/system.thrift
+++ b/ser-x-api/system.thrift
@@ -0,0 +1,6 @@
+service SystemService {
+    // 健康检查
+    // @public
+    Pong Ping(1: PingReq req) (api.post="/ping")
+}
diff --git a/ser-x-api/member.thrift b/ser-x-api/member.thrift
--- a/ser-x-api/member.thrift
+++ b/ser-x-api/member.thrift
@@ -10,0 +11,3 @@
+    // 注册
+    // @public
+    common.Empty Register(1: RegisterReq req) (api.post="/v1/member/register")
`
	ch := parseThriftDiff(diff, nil)
	if len(ch.Methods) != 2 {
		t.Fatalf("methods: %+v", ch.Methods)
	}
	if len(ch.Flags) != 1 || ch.Flags[0].File != "ser-x-api/member.thrift" {
		t.Errorf("flags (only Register's @public): %+v", ch.Flags)
	}
}
