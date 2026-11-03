package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrySigned(t *testing.T) {
	idl := t.TempDir()
	// No devkit.yaml: nothing is registered, nothing to protect.
	if fs := checkRegistrySigned(idl); len(fs) != 0 {
		t.Fatalf("no rules file: %v", fs)
	}
	uidWrite(t, filepath.Join(idl, "devkit.yaml"), "lint:\n  redis_keys: redis-keys.txt\n")
	uidWrite(t, filepath.Join(idl, "redis-keys.txt"), "site:capi: ser-admin\n")
	fs := checkRegistrySigned(idl)
	if len(fs) != 1 || fs[0].Rule != registrySignedRule || fs[0].File != "idl/registry.sig" || !strings.Contains(fs[0].Message, "没有签名") || !strings.Contains(fs[0].Message, "turtle") {
		t.Fatalf("unsigned registries: %+v", fs)
	}
	// A signature made with some other key is refused by the compiled-in public key.
	if err := os.WriteFile(filepath.Join(idl, "registry.sig"), []byte("devkit-registry-v1\ndevkit.yaml sha256:00\nredis-keys.txt sha256:00\nsig ed25519:AAAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fs := checkRegistrySigned(idl); len(fs) != 1 || !strings.Contains(fs[0].Message, "被改过") {
		t.Fatalf("stale hashes: %+v", fs)
	}
}
