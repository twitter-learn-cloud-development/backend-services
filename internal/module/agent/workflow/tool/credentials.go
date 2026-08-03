package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

var ErrCredentialReferenceNotFound = errors.New("credential reference not found")

type CredentialResolver interface {
	Resolve(ctx context.Context, reference string) (string, error)
}

type ResolvedProviderConfig struct {
	Provider string
	BaseURL  string
	Model    string
	APIKey   string
}

type ProviderConfigResolver interface {
	ResolveWorkflowProviderConfig(ctx context.Context, userID uint64, configID string) (ResolvedProviderConfig, error)
}

// EnvironmentCredentialResolver maps public reference names to an explicit
// allowlist of environment variables. Workflow authors cannot read arbitrary
// process environment values.
type EnvironmentCredentialResolver struct {
	references map[string]string
	lookup     func(string) (string, bool)
}

func NewEnvironmentCredentialResolver(references map[string]string) *EnvironmentCredentialResolver {
	cloned := make(map[string]string, len(references))
	for reference, environmentName := range references {
		reference = strings.TrimSpace(reference)
		environmentName = strings.TrimSpace(environmentName)
		if reference != "" && environmentName != "" {
			cloned[reference] = environmentName
		}
	}
	return &EnvironmentCredentialResolver{references: cloned, lookup: os.LookupEnv}
}

func (resolver *EnvironmentCredentialResolver) Resolve(_ context.Context, reference string) (string, error) {
	if resolver == nil {
		return "", ErrCredentialReferenceNotFound
	}
	environmentName, ok := resolver.references[strings.TrimSpace(reference)]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrCredentialReferenceNotFound, reference)
	}
	value, ok := resolver.lookup(environmentName)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: %s is unavailable", ErrCredentialReferenceNotFound, reference)
	}
	return value, nil
}
