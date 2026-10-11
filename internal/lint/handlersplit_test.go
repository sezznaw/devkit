package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerSplit(t *testing.T) {
	root := t.TempDir()
	w := func(name, body string) {
		if err := os.MkdirAll(filepath.Join(root, "handler"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "handler", name), []byte("package handler\n\n"+body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	method := func(name string) string {
		return "func (s *Impl) " + name + "(ctx context.Context, req *api." + name + "Req) (*api." + name + "Resp, error) { return nil, nil }\n"
	}
	w("handler.go", method("Ping")+"func New() *Impl { return nil }\n")
	w("profile.go", method("GetProfile")+method("UpdateUsername")+"func (s *Impl) OnEvent(ctx context.Context, ev *kafkax.Event) error { return nil }\n")
	if fs := checkHandlerSplit(root, 0); len(fs) != 0 {
		t.Fatalf("good layout flagged: %v", fs)
	}
	w("handler.go", method("Ping")+method("GetProfile"))
	fs := checkHandlerSplit(root, 0)
	if len(fs) != 1 || fs[0].Rule != "handler-split" || fs[0].Line != 4 {
		t.Fatalf("method in handler.go: %+v", fs)
	}
	big := ""
	for i := 0; i < 9; i++ {
		big += method("M" + string(rune('A'+i)))
	}
	w("handler.go", method("Ping"))
	w("big.go", big)
	if fs := checkHandlerSplit(root, 0); len(fs) != 1 {
		t.Fatalf("9 methods: %v", fs)
	}
	if fs := checkHandlerSplit(root, 10); len(fs) != 0 {
		t.Fatalf("limit 10: %v", fs)
	}
}
