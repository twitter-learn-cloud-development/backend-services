package credential

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestAESGCMCipherRoundTripAndAADBinding(t *testing.T) {
	cipher, err := NewAESGCMCipher("v2", map[string][]byte{
		"v1": bytes.Repeat([]byte{1}, 32),
		"v2": bytes.Repeat([]byte{2}, 32),
	})
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}
	secret, err := cipher.Encrypt([]byte("api-key-value"), []byte("user:config:provider"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if secret.KeyID != "v2" || secret.Ciphertext == "api-key-value" || secret.Nonce == "" {
		t.Fatalf("Encrypt() secret = %+v", secret)
	}
	plaintext, err := cipher.Decrypt(secret, []byte("user:config:provider"))
	if err != nil || string(plaintext) != "api-key-value" {
		t.Fatalf("Decrypt() plaintext/error = %q/%v", plaintext, err)
	}
	if _, err := cipher.Decrypt(secret, []byte("different-user")); err == nil {
		t.Fatal("Decrypt() error = nil for different AAD")
	}
}

func TestAESGCMCipherCanDecryptPreviousRotationKey(t *testing.T) {
	oldCipher, _ := NewAESGCMCipher("v1", map[string][]byte{"v1": bytes.Repeat([]byte{1}, 32)})
	secret, err := oldCipher.Encrypt([]byte("old-secret"), nil)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	rotatedCipher, _ := NewAESGCMCipher("v2", map[string][]byte{
		"v1": bytes.Repeat([]byte{1}, 32),
		"v2": bytes.Repeat([]byte{2}, 32),
	})
	plaintext, err := rotatedCipher.Decrypt(secret, nil)
	if err != nil || string(plaintext) != "old-secret" {
		t.Fatalf("Decrypt() plaintext/error = %q/%v", plaintext, err)
	}
}

func TestRunCheckpointCipherUsesIndependentEnvironment(t *testing.T) {
	t.Setenv(ProviderConfigMasterKeyEnv, "")
	t.Setenv(ProviderConfigMasterKeysEnv, "")
	t.Setenv(RunCheckpointActiveKeyEnv, "checkpoint-v1")
	t.Setenv(RunCheckpointMasterKeyEnv, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	t.Setenv(RunCheckpointMasterKeysEnv, "")

	cipher, err := NewRunCheckpointCipherFromEnv()
	if err != nil {
		t.Fatalf("NewRunCheckpointCipherFromEnv() error = %v", err)
	}
	secret, err := cipher.Encrypt([]byte("checkpoint"), []byte("user:run"))
	if err != nil || secret.KeyID != "checkpoint-v1" {
		t.Fatalf("Encrypt() secret/error = %+v/%v", secret, err)
	}
}
