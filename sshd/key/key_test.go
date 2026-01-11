package key_test

import (
	"testing"

	"github.com/jpillora/sshd-lite/sshd"
	"github.com/jpillora/sshd-lite/sshd/key"
	"golang.org/x/crypto/ssh"
)

func TestGenerateKey(t *testing.T) {
	t.Parallel()

	k1, err := key.GenerateKey("", false)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	if len(k1) == 0 {
		t.Fatal("generated key is empty")
	}

	k2, err := key.GenerateKey("", false)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	if string(k1) == string(k2) {
		t.Fatal("keys should be different when using random seed")
	}

	k3, err := key.GenerateKey("seed1", false)
	if err != nil {
		t.Fatalf("failed to generate key with seed: %v", err)
	}

	k4, err := key.GenerateKey("seed1", false)
	if err != nil {
		t.Fatalf("failed to generate key with same seed: %v", err)
	}
	if string(k3) != string(k4) {
		t.Fatal("keys with same seed should be identical")
	}

	k5, err := key.GenerateKey("seed2", false)
	if err != nil {
		t.Fatalf("failed to generate key with different seed: %v", err)
	}
	if string(k3) == string(k5) {
		t.Fatal("keys with different seeds should be different")
	}
}

func TestGenerateKeyEd25519(t *testing.T) {
	t.Parallel()

	k1, err := key.GenerateKey("", true)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}
	if len(k1) == 0 {
		t.Fatal("generated ed25519 key is empty")
	}

	k2, err := key.GenerateKey("seed", true)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key with seed: %v", err)
	}
	if len(k2) == 0 {
		t.Fatal("generated ed25519 key with seed is empty")
	}
}

func TestFingerprint(t *testing.T) {
	t.Parallel()

	k, err := key.GenerateKey("test", false)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	pri, err := ssh.ParsePrivateKey(k)
	if err != nil {
		t.Fatalf("failed to parse key: %v", err)
	}

	fp := key.Fingerprint(pri.PublicKey())
	if len(fp) == 0 {
		t.Fatal("fingerprint is empty")
	}
	if len(fp) < 7 || fp[:6] != "SHA256" {
		t.Fatalf("fingerprint should start with SHA256:, got %s", fp)
	}
}

func TestParseKeys(t *testing.T) {
	t.Parallel()

	pubkeyData := []byte(`ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCQnvq54YOsRVYpwaBJpNywAj9X4Aw895y+OQXEBb+xVGfgBeFET9ExWJMJCfk70snYpvRWQl3oNGLrKvalvmbiLOTafbw0pqIhFOSBdtri26pp4IWh1SucZfeLTxcjs0E4t0miTJN4W4V9GsznDgC5nca29ytxl8CkQBoCXFFWLJmqVjQ+YBjQFhITdMraqlaAluVhiKK8H7zKuLSnTosP66C4ypuqflwA+xEfb0sqxgdb4g6eEzMkFR2Hgdl+ka5yTq8E1W/0vv5QSJ4NHTHeCsYNUji1M13V7yly+UGPPsqZFGtSsuRscC7+ch9KPlQJi0bCYltsjSoFG2rFNi3Z test-key-1
ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC8E2fspjR0kZyYJOjudsI/nMeKgm0w7sFV5WwK/XyKDn+k0zSHvjeO4tnQaRrKFxXluBHWbl0MdRQWqhGrjt+d3pHXspTrSf4CEHO2mAC77R80K8zc5WlxaXZsTF0JaYMwHmZSXRGPaYd5IE6VD0cp/sl6WoeysOR7i6jhaFJU4zBu5i84CA1PbLkWj9S0A4nHcQlebPW1Rb4QuWIMjrkGGgbA53/RkR1un7q+93D1HQF97HKBFsrT/3L7pkFBiYcRMOGPpTXPyQz2F05N1/3aBZUuNNpqzfCar3PFRy9uzsDg33cjXByNuGV9IwTl9Xvlv5Eg4KVqUgmBdFBqSb6V test-key-2`)

	keys, err := key.ParseKeys(pubkeyData)
	if err != nil {
		t.Fatalf("failed to parse keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestParseKeysEmpty(t *testing.T) {
	t.Parallel()

	_, err := key.ParseKeys([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty keys")
	}
}

func TestDetermRand(t *testing.T) {
	t.Parallel()

	r := key.NewDetermRand([]byte("testseed"))
	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if n != 100 {
		t.Fatalf("expected 100 bytes, got %d", n)
	}

	r2 := key.NewDetermRand([]byte("testseed"))
	buf2 := make([]byte, 100)
	_, err = r2.Read(buf2)
	if err != nil {
		t.Fatalf("failed to read from second reader: %v", err)
	}

	if string(buf) != string(buf2) {
		t.Fatal("deterministic rand should produce same output for same seed")
	}
}

func TestConfigKeyBytes(t *testing.T) {
	t.Parallel()

	k, err := key.GenerateKey("test-seed", false)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cfg := &sshd.Config{
		KeyBytes: k,
	}

	if len(cfg.KeyBytes) == 0 {
		t.Fatal("KeyBytes should be set")
	}

	cfg2 := &sshd.Config{}
	if len(cfg2.KeyBytes) > 0 {
		t.Fatal("KeyBytes should be empty when not set")
	}
}

func TestConfigAuthKeys(t *testing.T) {
	t.Parallel()

	pubkeyData := []byte(`ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCQnvq54YOsRVYpwaBJpNywAj9X4Aw895y+OQXEBb+xVGfgBeFET9ExWJMJCfk70snYpvRWQl3oNGLrKvalvmbiLOTafbw0pqIhFOSBdtri26pp4IWh1SucZfeLTxcjs0E4t0miTJN4W4V9GsznDgC5nca29ytxl8CkQBoCXFFWLJmqVjQ+YBjQFhITdMraqlaAluVhiKK8H7zKuLSnTosP66C4ypuqflwA+xEfb0sqxgdb4g6eEzMkFR2Hgdl+ka5yTq8E1W/0vv5QSJ4NHTHeCsYNUji1M13V7yly+UGPPsqZFGtSsuRscC7+ch9KPlQJi0bCYltsjSoFG2rFNi3Z test-key`)

	keys, err := key.ParseKeys(pubkeyData)
	if err != nil {
		t.Fatalf("failed to parse keys: %v", err)
	}

	var authKeys []ssh.PublicKey
	for k := range keys {
		pub, err := ssh.ParsePublicKey([]byte(k))
		if err != nil {
			t.Fatalf("failed to parse public key: %v", err)
		}
		authKeys = append(authKeys, pub)
	}

	cfg := &sshd.Config{
		AuthKeys: authKeys,
	}

	if len(cfg.AuthKeys) != 1 {
		t.Fatalf("expected 1 auth key, got %d", len(cfg.AuthKeys))
	}

	cfg2 := &sshd.Config{}
	if len(cfg2.AuthKeys) > 0 {
		t.Fatal("AuthKeys should be empty when not set")
	}
}

func TestSignerFromSeed(t *testing.T) {
	t.Parallel()

	// Same seed should produce same signer
	signer1, err := key.SignerFromSeed("test-seed")
	if err != nil {
		t.Fatalf("failed to generate signer: %v", err)
	}

	signer2, err := key.SignerFromSeed("test-seed")
	if err != nil {
		t.Fatalf("failed to generate signer: %v", err)
	}

	// Compare public keys
	pub1 := signer1.PublicKey().Marshal()
	pub2 := signer2.PublicKey().Marshal()

	if string(pub1) != string(pub2) {
		t.Error("same seed should produce same signer")
	}

	// Different seed should produce different signer
	signer3, err := key.SignerFromSeed("different-seed")
	if err != nil {
		t.Fatalf("failed to generate signer: %v", err)
	}

	pub3 := signer3.PublicKey().Marshal()
	if string(pub1) == string(pub3) {
		t.Error("different seeds should produce different signers")
	}
}

func TestPublicKeyFromSeed(t *testing.T) {
	t.Parallel()

	pubKey, err := key.PublicKeyFromSeed("test-seed")
	if err != nil {
		t.Fatalf("failed to generate public key: %v", err)
	}

	if pubKey == nil {
		t.Fatal("public key should not be nil")
	}

	// Verify it matches signer's public key
	signer, _ := key.SignerFromSeed("test-seed")
	if string(pubKey.Marshal()) != string(signer.PublicKey().Marshal()) {
		t.Error("public key should match signer's public key")
	}
}

func TestAuthorizedKeyEntry(t *testing.T) {
	t.Parallel()

	entry, err := key.AuthorizedKeyEntry("test-seed")
	if err != nil {
		t.Fatalf("failed to generate authorized key entry: %v", err)
	}

	if entry == "" {
		t.Fatal("entry should not be empty")
	}

	// Should be parseable
	_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(entry))
	if err != nil {
		t.Fatalf("entry should be parseable: %v", err)
	}
}
