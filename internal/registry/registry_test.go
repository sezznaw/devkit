package registry

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) (idl, key string, pub ed25519.PublicKey) {
	t.Helper()
	root := t.TempDir()
	idl = filepath.Join(root, "idl")
	write(t, filepath.Join(idl, "devkit.yaml"), "lint:\n  redis_keys: redis-keys.txt\n  model_owners:\n    ser-admin: [AdminUser]\n")
	write(t, filepath.Join(idl, "redis-keys.txt"), "site:capi: ser-admin\nauth: ser-auth ser-api\n")
	key = filepath.Join(root, "registry.key")
	pubB64, err := Keygen(key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		t.Fatalf("public key: %v %d", err, len(b))
	}
	if st, _ := os.Stat(key); st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v", st.Mode().Perm())
	}
	if _, err := Keygen(key); err == nil {
		t.Fatal("keygen must refuse to overwrite")
	}
	return idl, key, ed25519.PublicKey(b)
}

func TestSignAndVerify(t *testing.T) {
	idl, key, pub := setup(t)
	if err := Verify(idl, pub); err == nil || !strings.Contains(err.Error(), "没有签名") {
		t.Fatalf("unsigned registries are a finding: %v", err)
	}
	files, err := Sign(idl, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "devkit.yaml,redis-keys.txt" {
		t.Fatalf("signed files %v", files)
	}
	if err := Verify(idl, pub); err != nil {
		t.Fatalf("fresh signature verifies: %v", err)
	}
	sig, _ := os.ReadFile(filepath.Join(idl, SigFile))
	if !strings.Contains(string(sig), "#") || !strings.Contains(string(sig), "sig ed25519:") {
		t.Fatalf("signature file keeps its explanation and signature:\n%s", sig)
	}

	// A colleague grants themselves a read: the hash no longer matches.
	write(t, filepath.Join(idl, "redis-keys.txt"), "site:capi: ser-admin ser-member\nauth: ser-auth ser-api\n")
	if err := Verify(idl, pub); err == nil || !strings.Contains(err.Error(), "redis-keys.txt 签名后被改过") {
		t.Fatalf("edited registry is a finding: %v", err)
	}
	if _, err := Sign(idl, key); err != nil {
		t.Fatal(err)
	}
	if err := Verify(idl, pub); err != nil {
		t.Fatalf("re-signed: %v", err)
	}

	// devkit.yaml itself (owners, exceptions, which registry file) is signed too.
	write(t, filepath.Join(idl, "devkit.yaml"), "lint:\n  redis_keys: redis-keys.txt\n  model_owners:\n    ser-member: [AdminUser]\n")
	if err := Verify(idl, pub); err == nil || !strings.Contains(err.Error(), "devkit.yaml 签名后被改过") {
		t.Fatalf("edited devkit.yaml is a finding: %v", err)
	}

	// A signature by another key is not the team's.
	if _, err := Sign(idl, key); err != nil {
		t.Fatal(err)
	}
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := Verify(idl, otherPub); err == nil || !strings.Contains(err.Error(), "不是用团队的私钥") {
		t.Fatalf("foreign key is a finding: %v", err)
	}

	// Dropping the key registry from devkit.yaml after signing: the signed set differs.
	write(t, filepath.Join(idl, "devkit.yaml"), "lint:\n  model_owners:\n    ser-admin: [AdminUser]\n")
	if _, err := Sign(idl, key); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(idl, "devkit.yaml"), "lint:\n  redis_keys: redis-keys.txt\n  model_owners:\n    ser-admin: [AdminUser]\n")
	if err := Verify(idl, pub); err == nil || !strings.Contains(err.Error(), "没有登记 redis-keys.txt") {
		t.Fatalf("registry added after signing is a finding: %v", err)
	}
}

func TestVerifyWithoutRules(t *testing.T) {
	idl := t.TempDir()
	write(t, filepath.Join(idl, "a.thrift"), "")
	if err := Verify(idl, ed25519.PublicKey(make([]byte, ed25519.PublicKeySize))); err != nil {
		t.Fatalf("a project without devkit.yaml has nothing to protect: %v", err)
	}
	if _, err := Sign(idl, "/nonexistent"); err == nil {
		t.Fatal("signing needs a key")
	}
}

func TestTeamPublicKeyIsSet(t *testing.T) {
	if len(TeamPublicKey()) != ed25519.PublicKeySize {
		t.Fatal("compiled-in public key")
	}
}
