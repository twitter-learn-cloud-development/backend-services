package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	ProviderConfigMasterKeyEnv  = "AGENT_PROVIDER_CONFIG_MASTER_KEY"
	ProviderConfigMasterKeysEnv = "AGENT_PROVIDER_CONFIG_MASTER_KEYS"
	ProviderConfigActiveKeyEnv  = "AGENT_PROVIDER_CONFIG_ACTIVE_KEY_ID"
	RunCheckpointMasterKeyEnv   = "AGENT_RUN_CHECKPOINT_MASTER_KEY"
	RunCheckpointMasterKeysEnv  = "AGENT_RUN_CHECKPOINT_MASTER_KEYS"
	RunCheckpointActiveKeyEnv   = "AGENT_RUN_CHECKPOINT_ACTIVE_KEY_ID"
)

var ErrSecretCipherUnavailable = errors.New("provider config secret cipher is unavailable")

type EncryptedSecret struct {
	KeyID      string
	Nonce      string
	Ciphertext string
}

type SecretCipher interface {
	Encrypt(plaintext []byte, additionalData []byte) (EncryptedSecret, error)
	Decrypt(secret EncryptedSecret, additionalData []byte) ([]byte, error)
}

type AESGCMCipher struct {
	activeKeyID string
	keys        map[string][]byte
	random      io.Reader
}

func NewAESGCMCipher(activeKeyID string, keys map[string][]byte) (*AESGCMCipher, error) {
	activeKeyID = strings.TrimSpace(activeKeyID)
	if activeKeyID == "" {
		return nil, errors.New("active encryption key id is required")
	}
	cloned := make(map[string][]byte, len(keys))
	for keyID, key := range keys {
		keyID = strings.TrimSpace(keyID)
		if keyID == "" || len(key) != 32 {
			return nil, fmt.Errorf("encryption key %q must contain exactly 32 bytes", keyID)
		}
		cloned[keyID] = append([]byte(nil), key...)
	}
	if _, ok := cloned[activeKeyID]; !ok {
		return nil, fmt.Errorf("active encryption key %q is not in keyring", activeKeyID)
	}
	return &AESGCMCipher{activeKeyID: activeKeyID, keys: cloned, random: rand.Reader}, nil
}

// NewAESGCMCipherFromEnv accepts either a single base64 key or a rotation
// keyring formatted as "key-id:base64,key-id-2:base64".
func NewAESGCMCipherFromEnv() (*AESGCMCipher, error) {
	return newAESGCMCipherFromEnv(
		ProviderConfigMasterKeyEnv,
		ProviderConfigMasterKeysEnv,
		ProviderConfigActiveKeyEnv,
		"provider config",
	)
}

// NewRunCheckpointCipherFromEnv uses an independent key namespace so a
// provider credential key can be rotated or revoked without coupling it to
// durable Runtime recovery state.
func NewRunCheckpointCipherFromEnv() (*AESGCMCipher, error) {
	return newAESGCMCipherFromEnv(
		RunCheckpointMasterKeyEnv,
		RunCheckpointMasterKeysEnv,
		RunCheckpointActiveKeyEnv,
		"agent run checkpoint",
	)
}

func newAESGCMCipherFromEnv(masterKeyEnv, masterKeysEnv, activeKeyEnv, purpose string) (*AESGCMCipher, error) {
	activeKeyID := strings.TrimSpace(os.Getenv(activeKeyEnv))
	if activeKeyID == "" {
		activeKeyID = "v1"
	}
	keys := make(map[string][]byte)
	for _, entry := range strings.Split(os.Getenv(masterKeysEnv), ",") {
		keyID, encoded, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || strings.TrimSpace(keyID) == "" || strings.TrimSpace(encoded) == "" {
			continue
		}
		decoded, err := decodeKey(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode %s key %q: %w", purpose, keyID, err)
		}
		keys[strings.TrimSpace(keyID)] = decoded
	}
	if encoded := strings.TrimSpace(os.Getenv(masterKeyEnv)); encoded != "" {
		decoded, err := decodeKey(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode %s active key: %w", purpose, err)
		}
		keys[activeKeyID] = decoded
	}
	if len(keys) == 0 {
		return nil, ErrSecretCipherUnavailable
	}
	return NewAESGCMCipher(activeKeyID, keys)
}

func (c *AESGCMCipher) Encrypt(plaintext []byte, additionalData []byte) (EncryptedSecret, error) {
	if c == nil {
		return EncryptedSecret{}, ErrSecretCipherUnavailable
	}
	aead, err := c.aead(c.activeKeyID)
	if err != nil {
		return EncryptedSecret{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(c.random, nonce); err != nil {
		return EncryptedSecret{}, fmt.Errorf("generate encryption nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, additionalData)
	return EncryptedSecret{
		KeyID:      c.activeKeyID,
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext),
	}, nil
}

func (c *AESGCMCipher) Decrypt(secret EncryptedSecret, additionalData []byte) ([]byte, error) {
	if c == nil {
		return nil, ErrSecretCipherUnavailable
	}
	aead, err := c.aead(secret.KeyID)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.RawURLEncoding.DecodeString(secret.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid encrypted secret nonce")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(secret.Ciphertext)
	if err != nil {
		return nil, errors.New("invalid encrypted secret ciphertext")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return nil, errors.New("decrypt provider config secret failed")
	}
	return plaintext, nil
}

func (c *AESGCMCipher) aead(keyID string) (cipher.AEAD, error) {
	key, ok := c.keys[strings.TrimSpace(keyID)]
	if !ok {
		return nil, fmt.Errorf("encryption key %q is unavailable", keyID)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func decodeKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(encoded)
		if err == nil {
			if len(decoded) != 32 {
				return nil, fmt.Errorf("decoded key length is %d, want 32", len(decoded))
			}
			return decoded, nil
		}
	}
	return nil, errors.New("key is not valid base64")
}
