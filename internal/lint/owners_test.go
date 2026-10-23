package lint

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func ownersRun(t *testing.T, root string, fn func(goFiles []string) []Finding) []Finding {
	t.Helper()
	goFiles, err := businessGoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	return fn(goFiles)
}

func findingLines(fs []Finding, rule string) []int {
	var out []int
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f.Line)
		}
	}
	return out
}

func TestRedisKeyOwner(t *testing.T) {
	root := t.TempDir()
	reg := filepath.Join(root, "idl", "redis-keys.txt")
	uidWrite(t, reg, `# 登记表
site:capi: ser-admin
admin:login: ser-admin
admin:login:role: ser-admin-api
auth: ser-auth ser-api
pkg lib/sitecapi ser-admin
broken
`)
	uidWrite(t, filepath.Join(root, "handler", "x.go"), `package handler

import (
	"context"
	"gitlab.example.com/x/backend/lib/sitecapi"
)

func a(ctx context.Context) {
	_ = "site:capi:index:" + "t"        // 别人的前缀 → 报
	_ = "member:login:7"                // 没登记 → 不报
	_ = "auth:sid:" + "x"               // ser-member 不在 auth 的允许名单 → 报
	_ = "admin:login:role:7"            // 最长匹配归 ser-admin-api → 报
	_ = "site:capi"                     // 裸词不算键 → 不报
	_ = "site:capi:venue:" //devkit:lint-ignore redis-key-owner
	_ = redisx.WithLock(ctx, rdb, "site:capi:"+id, 0, nil) // redisx 自己加前缀 → 不报
	// _ = "site:capi:index:" 注释不算
}
`)
	uidWrite(t, filepath.Join(root, "handler", "x_test.go"), "package handler_test\n\nvar _ = \"site:capi:index\"\n")
	r := loadKeyRegistry(reg, "idl/redis-keys.txt")
	if len(r.errs) != 1 || r.errs[0].Line != 7 {
		t.Fatalf("bad line not reported: %+v", r.errs)
	}
	fs := ownersRun(t, root, func(goFiles []string) []Finding { return checkRedisKeyOwner(root, "ser-member", goFiles, r) })
	want := []int{5, 9, 11, 12}
	if got := findingLines(fs, redisKeyRule); fmtInts(got) != fmtInts(want) {
		t.Fatalf("ser-member findings at %v, want %v: %+v", got, want, fs)
	}
	// ser-api 在 auth 的允许名单里；ser-admin 是 site:capi 的所有者。
	for svc, wantN := range map[string]int{"ser-api": 3, "ser-admin": 2} {
		fs := ownersRun(t, root, func(goFiles []string) []Finding { return checkRedisKeyOwner(root, svc, goFiles, r) })
		if n := len(findingLines(fs, redisKeyRule)); n != wantN {
			t.Fatalf("%s: %d findings, want %d: %+v", svc, n, wantN, fs)
		}
	}
	if fs := ownersRun(t, root, func(goFiles []string) []Finding {
		return checkRedisKeyOwner(root, "ser-member", goFiles, keyRegistry{})
	}); len(fs) != 0 {
		t.Fatalf("empty registry must be silent: %+v", fs)
	}
}

func fmtInts(xs []int) string { return strings.Trim(fmt.Sprint(xs), "[]") }

func TestRedisPubSub(t *testing.T) {
	root := t.TempDir()
	uidWrite(t, filepath.Join(root, "handler", "redis.go"), `package handler

import (
	"context"

	"github.com/redis/go-redis/v9"
)

func a(ctx context.Context, rdb *redis.Client, s *Impl) {
	sub := rdb.Subscribe(ctx, "game:venue:public")      // 报
	_ = sub
	_ = s.rdb.Publish(ctx, "game:venue:connector", "1")  // 报
	_ = s.push.Publish(ctx, "user:1", nil)              // 推送，不报
	_ = rt.Kafka.Publish(ctx, "events.x", "k", "e", nil) // Kafka，不报
	_ = rdb.PSubscribe(ctx, "x*") //devkit:lint-ignore redis-pubsub —— 本服务内部
}
`)
	uidWrite(t, filepath.Join(root, "handler", "plain.go"), `package handler

func b(bus Bus) { bus.Publish("x") } // 文件没 import go-redis，不报
`)
	uidWrite(t, filepath.Join(root, "handler", "x_test.go"), "package handler_test\n")
	fs := ownersRun(t, root, func(goFiles []string) []Finding { return checkRedisPubSub(root, goFiles) })
	if got := findingLines(fs, redisPubSubRul); fmtInts(got) != "10 12" {
		t.Fatalf("findings at %v: %+v", got, fs)
	}
}

func TestEventOwner(t *testing.T) {
	root := t.TempDir()
	uidWrite(t, filepath.Join(root, "handler", "events.go"), `package handler

const memberEventsTopic = "events.member"

const (
	VenueEventsTopic = "events.game"
)

func a(s *Impl) {
	_ = s.events.PublishTx(ctx, tx, memberEventsTopic, key, "member.updated", d) // 别人的主题（除非 producers 放行）
	_ = rt.Kafka.Publish(ctx, "events.admin", key, "admin.x", d)                  // 自己的
	_ = rt.Kafka.PublishTx(ctx, tx,
		handler.VenueEventsTopic, key, "venue.updated", d) // 跨行、带包名的常量 → 报
	_ = rt.Kafka.Publish(ctx, "callbacks.cr", key, "callback.received", d) // 只有厂商服务能发 → 报
	_ = s.push.Publish(ctx, fmt.Sprintf("user:%d", uid), map[string]any{}) // 不是主题，跳过
	_ = rt.Kafka.Publish(ctx, topic, key, "x", d)                           // 解不出，跳过
	_ = rt.Kafka.Subscribe("events.member", h.OnMemberEvent)                // 消费不限
	_ = s.events.PublishTx(ctx, tx, memberEventsTopic, key, "member.updated", d) //devkit:lint-ignore event-owner
}
`)
	uidWrite(t, filepath.Join(root, "handler", "x_test.go"), "package handler_test\n")
	run := func(svc, vendor string, producers map[string][]string) []int {
		fs := ownersRun(t, root, func(goFiles []string) []Finding {
			return checkEventOwner(root, svc, vendor, goFiles, producers)
		})
		return findingLines(fs, eventOwnerRule)
	}
	if got := run("ser-admin", "ser-vendor", nil); fmtInts(got) != "10 12 14" {
		t.Fatalf("ser-admin: %v", got)
	}
	if got := run("ser-admin", "ser-vendor", map[string][]string{"events.member": {"ser-admin"}}); fmtInts(got) != "12 14" {
		t.Fatalf("ser-admin with producers: %v", got)
	}
	if got := run("ser-vendor", "ser-vendor", nil); fmtInts(got) != "10 11 12" {
		t.Fatalf("ser-vendor: %v", got)
	}
	if got := run("ser-game", "ser-vendor", nil); fmtInts(got) != "10 11 14" {
		t.Fatalf("ser-game: %v", got)
	}
	if got := run("ser-finance-admin", "ser-vendor", nil); len(got) != 4 {
		t.Fatalf("ser-finance-admin: %v", got)
	}
}

func TestModelOwner(t *testing.T) {
	root := t.TempDir()
	uidWrite(t, filepath.Join(root, "repo", "member.go"), `package repo

func a(db *gorm.DB) {
	var m model.Member        // 自己的
	var ag model.Agent        // 别人的 → 报（第一处）
	_ = []model.Agent{}       // 同文件同模型第二处不再报
	_ = mysql.AdminUser{}     // 别人的 → 报
	_ = model.WalletKindMain  // 没登记（常量），不报
	_ = model.Agent{} //devkit:lint-ignore model-owner
	_, _ = m, ag
}
`)
	uidWrite(t, filepath.Join(root, "app", "app.go"), `package app

func setup(rt *kitexx.Runtime) error {
	return rt.AutoMigrate(model.Member{}, model.VerifyCode{},
		model.Agent{}) // 迁移别人的表 → 报，不看例外
}
`)
	uidWrite(t, filepath.Join(root, "handler", "x_test.go"), "package handler_test\n")
	owners := map[string][]string{
		"ser-member": {"Member", "VerifyCode"},
		"ser-admin":  {"Agent", "AdminUser"},
	}
	run := func(svc string, exc map[string][]string) []Finding {
		return ownersRun(t, root, func(goFiles []string) []Finding { return checkModelOwner(root, svc, goFiles, owners, exc) })
	}
	fs := run("ser-member", nil)
	files := map[string][]int{}
	for _, f := range fs {
		if f.Rule != modelOwnerRule {
			t.Fatalf("rule: %+v", f)
		}
		files[f.File] = append(files[f.File], f.Line)
	}
	if fmtInts(files["repo/member.go"]) != "5 7" || fmtInts(files["app/app.go"]) != "5" {
		t.Fatalf("findings: %v (%+v)", files, fs)
	}
	// 例外放过引用，但 AutoMigrate 别人的表照样报。
	fs = run("ser-member", map[string][]string{"ser-member": {"Agent", "AdminUser"}})
	if len(fs) != 1 || fs[0].File != "app/app.go" {
		t.Fatalf("exceptions: %+v", fs)
	}
	if fs := run("ser-member", nil); len(ownersRun(t, root, func(goFiles []string) []Finding {
		return checkModelOwner(root, "ser-member", goFiles, nil, nil)
	})) != 0 || len(fs) == 0 {
		t.Fatalf("no owners must be silent")
	}
	if fs := run("ser-admin", nil); len(fs) != 3 { // app.go 的 AutoMigrate 两处（Member、VerifyCode）+ repo 里引用 Member 一处
		t.Fatalf("ser-admin: %+v", fs)
	}
}
