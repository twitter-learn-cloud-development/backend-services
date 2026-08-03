package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"twitter-clone/internal/module/agent/mcp/remote"
)

const maxBearerTokenBytes = 64 * 1024

type Credential struct {
	BearerToken string
	Identity    string
}

type CredentialSource interface {
	Load(context.Context) (Credential, error)
	Kind() string
	Rotatable() bool
}

type noCredentialSource struct{}

func (noCredentialSource) Load(ctx context.Context) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	return Credential{}, nil
}

func (noCredentialSource) Kind() string    { return remote.AuthNone }
func (noCredentialSource) Rotatable() bool { return false }

type environmentCredentialSource struct {
	name string
}

func (source environmentCredentialSource) Load(ctx context.Context) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	return credentialFromToken(os.Getenv(source.name))
}

func (environmentCredentialSource) Kind() string    { return "bearer_env" }
func (environmentCredentialSource) Rotatable() bool { return false }

type fileCredentialSource struct {
	path string
}

func (source fileCredentialSource) Load(ctx context.Context) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	file, err := os.Open(source.path)
	if err != nil {
		return Credential{}, errors.New("MCP acceptance bearer token file is unavailable")
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxBearerTokenBytes+1))
	if err != nil {
		return Credential{}, errors.New("MCP acceptance bearer token file cannot be read")
	}
	if len(payload) > maxBearerTokenBytes {
		return Credential{}, errors.New("MCP acceptance bearer token file is too large")
	}
	return credentialFromToken(string(payload))
}

func (fileCredentialSource) Kind() string    { return "bearer_file" }
func (fileCredentialSource) Rotatable() bool { return true }

func NewCredentialSource(config AuthConfig) (CredentialSource, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(config.Type)) {
	case remote.AuthNone:
		return noCredentialSource{}, nil
	case remote.AuthBearer:
		if name := strings.TrimSpace(config.BearerTokenEnv); name != "" {
			return environmentCredentialSource{name: name}, nil
		}
		return fileCredentialSource{path: strings.TrimSpace(config.BearerTokenFile)}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP acceptance auth type %q", config.Type)
	}
}

func credentialFromToken(raw string) (Credential, error) {
	token := strings.TrimSpace(raw)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return Credential{}, errors.New("MCP acceptance bearer token is empty or malformed")
	}
	if len(token) > maxBearerTokenBytes {
		return Credential{}, errors.New("MCP acceptance bearer token is too large")
	}
	digest := sha256.Sum256([]byte("agent-mcp-acceptance-credential:v1\x00" + token))
	return Credential{BearerToken: token, Identity: hex.EncodeToString(digest[:])}, nil
}
