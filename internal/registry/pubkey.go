package registry

import (
	"crypto/ed25519"
	"encoding/base64"
)

// teamPublicKey is the public half of the key `devkit registry keygen` made
// on turtle's machine (2026-11-03). Only that private key can sign the
// registries; rotating it means a new devkit release with the new value.
const teamPublicKey = "9gR9d73d0H/twb9FGO2ZIMoAaUX0f/iZYH53Gsz1nIQ="

// TeamPublicKey is the compiled-in verification key.
func TeamPublicKey() ed25519.PublicKey {
	b, err := base64.StdEncoding.DecodeString(teamPublicKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("devkit: teamPublicKey is not a base64 Ed25519 public key")
	}
	return ed25519.PublicKey(b)
}
