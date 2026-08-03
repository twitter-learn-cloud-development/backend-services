package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	agentModel "twitter-clone/internal/module/agent/model"
)

const (
	maxManagedCredentialRegistryBytes = 256 * 1024
	maxManagedCredentialDefinitions   = 128
)

var (
	ErrManagedCredentialsDisabled   = errors.New("managed external MCP credentials are disabled")
	ErrManagedCredentialNotFound    = errors.New("managed external MCP credential not found")
	ErrManagedCredentialUnavailable = errors.New("managed external MCP credential is unavailable")
	ErrManagedCredentialBinding     = errors.New("managed external MCP credential binding is invalid")

	managedCredentialReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,95}$`)
	managedCredentialSecretKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

// ManagedCredentialRequest binds one administrator-managed secret to an
// exact Agent project and MCP endpoint. Callers cannot use a project secret
// as a generic bearer-token lookup.
type ManagedCredentialRequest struct {
	Reference string
	ProjectID string
	Endpoint  string
	AuthType  string
}

// ResolvedManagedCredential exists only at the remote call boundary. Identity
// is a one-way digest used to isolate pooled sessions after secret rotation.
type ResolvedManagedCredential struct {
	BearerToken string
	Version     int64
	Identity    string
}

type ManagedCredentialResolver interface {
	Resolve(ctx context.Context, request ManagedCredentialRequest) (ResolvedManagedCredential, error)
}

type managedCredentialDefinition struct {
	Reference string `json:"reference"`
	ProjectID string `json:"project_id"`
	Endpoint  string `json:"endpoint"`
	AuthType  string `json:"auth_type"`
	SecretKey string `json:"secret_key"`
	Version   int64  `json:"version"`
}

// FileManagedCredentialResolver reads projected deployment secrets at call
// time. The registry contains only metadata and secret file keys, never token
// values. Reading on every call lets Kubernetes-style atomic secret projection
// rotate credentials without persisting them in Mongo or process-wide caches.
type FileManagedCredentialResolver struct {
	secretDirectory string
	resolvedRoot    string
	definitions     map[string]managedCredentialDefinition
}

func NewFileManagedCredentialResolver(
	registryJSON string,
	secretDirectory string,
	endpointPolicy *agentModel.EndpointPolicy,
) (*FileManagedCredentialResolver, error) {
	registryJSON = strings.TrimSpace(registryJSON)
	if registryJSON == "" {
		return nil, errors.New("managed external MCP credential registry is required")
	}
	if len(registryJSON) > maxManagedCredentialRegistryBytes {
		return nil, errors.New("managed external MCP credential registry is too large")
	}
	decoder := json.NewDecoder(strings.NewReader(registryJSON))
	decoder.DisallowUnknownFields()
	var definitions []managedCredentialDefinition
	if err := decoder.Decode(&definitions); err != nil {
		return nil, fmt.Errorf("decode managed external MCP credential registry: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if len(definitions) == 0 || len(definitions) > maxManagedCredentialDefinitions {
		return nil, fmt.Errorf("managed external MCP credential registry must contain 1-%d entries", maxManagedCredentialDefinitions)
	}

	secretDirectory = strings.TrimSpace(secretDirectory)
	if secretDirectory == "" {
		return nil, errors.New("managed external MCP credential secret directory is required")
	}
	absoluteRoot, err := filepath.Abs(secretDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve managed external MCP credential secret directory: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("open managed external MCP credential secret directory: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil || !info.IsDir() {
		return nil, errors.New("managed external MCP credential secret directory is unavailable")
	}
	if endpointPolicy == nil {
		endpointPolicy = agentModel.NewEndpointPolicy()
	}

	resolver := &FileManagedCredentialResolver{
		secretDirectory: absoluteRoot,
		resolvedRoot:    resolvedRoot,
		definitions:     make(map[string]managedCredentialDefinition, len(definitions)),
	}
	for index := range definitions {
		definition, validateErr := normalizeManagedCredentialDefinition(definitions[index], endpointPolicy)
		if validateErr != nil {
			return nil, fmt.Errorf("managed external MCP credential registry entry %d: %w", index, validateErr)
		}
		if _, exists := resolver.definitions[definition.Reference]; exists {
			return nil, fmt.Errorf("managed external MCP credential reference %q is duplicated", definition.Reference)
		}
		resolver.definitions[definition.Reference] = definition
		if _, readErr := resolver.readSecret(definition); readErr != nil {
			return nil, fmt.Errorf("managed external MCP credential %q: %w", definition.Reference, readErr)
		}
	}
	return resolver, nil
}

func (resolver *FileManagedCredentialResolver) Resolve(
	ctx context.Context,
	request ManagedCredentialRequest,
) (ResolvedManagedCredential, error) {
	if resolver == nil {
		return ResolvedManagedCredential{}, ErrManagedCredentialUnavailable
	}
	if err := ctx.Err(); err != nil {
		return ResolvedManagedCredential{}, err
	}
	reference := strings.TrimSpace(request.Reference)
	definition, ok := resolver.definitions[reference]
	if !ok {
		return ResolvedManagedCredential{}, ErrManagedCredentialNotFound
	}
	if definition.ProjectID != strings.TrimSpace(request.ProjectID) ||
		definition.Endpoint != strings.TrimSpace(request.Endpoint) ||
		definition.AuthType != strings.ToLower(strings.TrimSpace(request.AuthType)) {
		return ResolvedManagedCredential{}, ErrManagedCredentialBinding
	}
	token, err := resolver.readSecret(definition)
	if err != nil {
		return ResolvedManagedCredential{}, fmt.Errorf("%w: %v", ErrManagedCredentialUnavailable, err)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf(
		"external-mcp-managed:v1\x00%s\x00%d\x00%s",
		definition.Reference,
		definition.Version,
		token,
	)))
	return ResolvedManagedCredential{
		BearerToken: token,
		Version:     definition.Version,
		Identity:    hex.EncodeToString(digest[:]),
	}, nil
}

func (resolver *FileManagedCredentialResolver) readSecret(definition managedCredentialDefinition) (string, error) {
	path := filepath.Join(resolver.secretDirectory, definition.SecretKey)
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("secret file is unavailable")
	}
	relative, err := filepath.Rel(resolver.resolvedRoot, resolvedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("secret file escapes the configured directory")
	}
	file, err := os.Open(resolvedPath)
	if err != nil {
		return "", errors.New("secret file is unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBearerTokenBytes+1))
	if err != nil {
		return "", errors.New("secret file cannot be read")
	}
	if len(data) > maxBearerTokenBytes {
		return "", errors.New("secret file is too large")
	}
	token := strings.TrimSpace(string(data))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("secret file does not contain a valid bearer token")
	}
	return token, nil
}

func normalizeManagedCredentialDefinition(
	definition managedCredentialDefinition,
	endpointPolicy *agentModel.EndpointPolicy,
) (managedCredentialDefinition, error) {
	definition.Reference = strings.TrimSpace(definition.Reference)
	definition.ProjectID = strings.TrimSpace(definition.ProjectID)
	definition.Endpoint = strings.TrimSpace(definition.Endpoint)
	definition.AuthType = strings.ToLower(strings.TrimSpace(definition.AuthType))
	definition.SecretKey = strings.TrimSpace(definition.SecretKey)
	if !managedCredentialReferencePattern.MatchString(definition.Reference) {
		return definition, errors.New("reference is invalid")
	}
	if !projectIDPattern.MatchString(definition.ProjectID) {
		return definition, errors.New("project_id is invalid")
	}
	if definition.Endpoint == "" || len(definition.Endpoint) > maxEndpointBytes {
		return definition, errors.New("endpoint is invalid")
	}
	if err := endpointPolicy.Validate(definition.Endpoint, "external-mcp-managed-credential"); err != nil {
		return definition, err
	}
	parsedEndpoint, err := url.Parse(definition.Endpoint)
	if err != nil || parsedEndpoint.Scheme != "https" {
		return definition, errors.New("managed bearer credential endpoint must use https")
	}
	if definition.AuthType == "" {
		definition.AuthType = AuthBearer
	}
	if definition.AuthType != AuthBearer {
		return definition, errors.New("managed credential auth_type must be bearer")
	}
	if !managedCredentialSecretKeyPattern.MatchString(definition.SecretKey) {
		return definition, errors.New("secret_key is invalid")
	}
	if definition.Version <= 0 {
		return definition, errors.New("version must be positive")
	}
	return definition, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("managed external MCP credential registry contains trailing JSON")
		}
		return fmt.Errorf("decode managed external MCP credential registry trailer: %w", err)
	}
	return nil
}
