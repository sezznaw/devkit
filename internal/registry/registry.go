// Package registry signs and verifies the IDL repository's ownership
// registries (devkit.yaml and the Redis key registry it names). The public
// key is compiled into devkit, the private key lives only on the machine of
// the person who grants ownership, so a lint finding, not a code review,
// catches a service that registered itself as a reader of another service's
// keys (that is how a NOPERM reached dev on 2026-11-03).
//
// 登记表签名：idl/devkit.yaml 与它指向的 Redis 键登记表由持有私钥的人（turtle）签名（devkit registry sign），
// 签名放在 idl/registry.sig；devkit lint 的规则 registry-signed 用编译进二进制的公钥验签。改了这两个文件不重新签名，
// 所有服务的 make lint 都会红——这就是目的：登记表是授权，只有一个人能开口子。
package registry

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SigFile is the signature file inside the IDL directory.
const SigFile = "registry.sig"

// RulesFile is the project rules file inside the IDL directory (idl/devkit.yaml).
const RulesFile = "devkit.yaml"

const (
	formatLine = "devkit-registry-v1"
	keyPrefix  = "ed25519-seed:"
)

// DefaultKeyPath is where `devkit registry keygen` puts the private key and
// `devkit registry sign` reads it: ~/.config/devkit/registry.key (mode 0600).
func DefaultKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "devkit", "registry.key"), nil
}

// Files lists the registries that are signed, relative to idlDir: devkit.yaml
// and, when it names one, lint.redis_keys. Missing files are skipped so a
// project without a key registry still signs its rules file.
func Files(idlDir string) ([]string, error) {
	var files []string
	rules := filepath.Join(idlDir, RulesFile)
	if _, err := os.Stat(rules); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	files = append(files, RulesFile)
	data, err := os.ReadFile(rules)
	if err != nil {
		return nil, err
	}
	var r struct {
		Lint struct {
			RedisKeys string `yaml:"redis_keys"`
		} `yaml:"lint"`
	}
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", rules, err)
	}
	if k := strings.TrimSpace(r.Lint.RedisKeys); k != "" {
		if _, err := os.Stat(filepath.Join(idlDir, k)); err == nil {
			files = append(files, filepath.ToSlash(k))
		}
	}
	return files, nil
}

// Manifest is the signed text: the format line, then "<file> sha256:<hex>"
// per file in the given order. Comments and the signature line are not part
// of it, so the file can carry an explanation for the reader.
func Manifest(idlDir string, files []string) (string, error) {
	var b strings.Builder
	b.WriteString(formatLine + "\n")
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(idlDir, filepath.FromSlash(f)))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s sha256:%s\n", f, hex.EncodeToString(sum[:]))
	}
	return b.String(), nil
}

// Keygen writes a new private key to keyPath (0600, refusing to overwrite)
// and returns the public key to compile into devkit (base64).
func Keygen(keyPath string) (string, error) {
	if _, err := os.Stat(keyPath); err == nil {
		return "", fmt.Errorf("%s already exists; refusing to overwrite a signing key", keyPath)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return "", err
	}
	line := keyPrefix + base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"
	if err := os.WriteFile(keyPath, []byte(line), 0o600); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

// LoadPrivateKey reads the key written by Keygen.
func LoadPrivateKey(keyPath string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w (run `devkit registry keygen` on the machine that grants ownership)", err)
	}
	s := strings.TrimSpace(string(data))
	if !strings.HasPrefix(s, keyPrefix) {
		return nil, fmt.Errorf("%s is not a devkit registry key", keyPath)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, keyPrefix))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s: bad key", keyPath)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Sign writes idlDir/registry.sig for the current registries with the
// private key at keyPath. It returns the signed files.
func Sign(idlDir, keyPath string) ([]string, error) {
	priv, err := LoadPrivateKey(keyPath)
	if err != nil {
		return nil, err
	}
	files, err := Files(idlDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s has no %s: nothing to sign", idlDir, RulesFile)
	}
	manifest, err := Manifest(idlDir, files)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(priv, []byte(manifest))
	var b bytes.Buffer
	b.WriteString("# 登记表签名（devkit registry sign）：devkit lint 的规则 registry-signed 用编译进 devkit 的公钥验签。\n")
	b.WriteString("# 下面列的文件是授权（谁拥有哪些 Redis 键 / 表 / 事件），只有持有私钥的人（turtle）改完重新签名；\n")
	b.WriteString("# 别人改了这些文件，所有服务的 make lint 都会红。要加一行：把要加的内容发给 turtle。\n")
	b.WriteString(manifest)
	b.WriteString("sig ed25519:" + base64.StdEncoding.EncodeToString(sig) + "\n")
	if err := os.WriteFile(filepath.Join(idlDir, SigFile), b.Bytes(), 0o644); err != nil {
		return nil, err
	}
	return files, nil
}

// Verify checks idlDir/registry.sig against the current registries with pub.
// A nil error means every signed file is unchanged and the signature is by
// the holder of the private key. The error text is written for the lint
// finding: it says which file changed, or that the signature is missing.
func Verify(idlDir string, pub ed25519.PublicKey) error {
	files, err := Files(idlDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil // no devkit.yaml: nothing is registered, nothing to protect
	}
	data, err := os.ReadFile(filepath.Join(idlDir, SigFile))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("idl/%s 不存在：登记表（%s）没有签名", SigFile, strings.Join(files, "、"))
	}
	if err != nil {
		return err
	}
	var signed []string
	recorded := map[string]string{}
	var sig []byte
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#") || line == formatLine:
		case strings.HasPrefix(line, "sig ed25519:"):
			sig, err = base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "sig ed25519:"))
			if err != nil {
				return fmt.Errorf("idl/%s 的签名行不是 base64", SigFile)
			}
		default:
			f, h, ok := strings.Cut(line, " ")
			if !ok || !strings.HasPrefix(h, "sha256:") {
				return fmt.Errorf("idl/%s 第 %q 行看不懂", SigFile, line)
			}
			signed = append(signed, f)
			recorded[f] = strings.TrimPrefix(h, "sha256:")
		}
	}
	if sig == nil {
		return fmt.Errorf("idl/%s 没有签名行", SigFile)
	}
	// The signed set must be exactly the current set: a registry added since
	// the last signing (or one dropped from devkit.yaml) also needs a signature.
	for _, f := range files {
		if _, ok := recorded[f]; !ok {
			return fmt.Errorf("idl/%s 没有登记 %s：登记表变了但没有重新签名", SigFile, f)
		}
	}
	if len(signed) != len(files) {
		return fmt.Errorf("idl/%s 列的文件和现在的登记表对不上：登记表变了但没有重新签名", SigFile)
	}
	manifest, err := Manifest(idlDir, signed)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(manifest), "\n")[1:] {
		f, h, _ := strings.Cut(line, " ")
		if strings.TrimPrefix(h, "sha256:") != recorded[f] {
			return fmt.Errorf("idl/%s 签名后被改过，没有重新签名", f)
		}
	}
	if !ed25519.Verify(pub, []byte(manifest), sig) {
		return fmt.Errorf("idl/%s 的签名不是用团队的私钥签的", SigFile)
	}
	return nil
}
