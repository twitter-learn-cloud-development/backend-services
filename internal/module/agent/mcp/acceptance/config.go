package acceptance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"twitter-clone/internal/module/agent/mcp/remote"
)

const (
	ConfigSchemaVersion = "agent-mcp-acceptance-config/v1"

	maxConfigBytes    = 256 * 1024
	maxArgumentsBytes = 64 * 1024
	maxAllowedHosts   = 32
)

var (
	targetNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	toolNamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	environmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

	sensitiveArgumentNames = map[string]struct{}{
		"api_key": {}, "apikey": {}, "authorization": {}, "cookie": {},
		"credential": {}, "password": {}, "secret": {}, "token": {},
		"access_token": {}, "refresh_token": {},
	}
)

type Config struct {
	SchemaVersion    string            `json:"schema_version"`
	Target           string            `json:"target"`
	Transport        string            `json:"transport"`
	Endpoint         string            `json:"endpoint"`
	AllowedHosts     []string          `json:"allowed_hosts,omitempty"`
	Auth             AuthConfig        `json:"auth"`
	ReadProbe        ToolProbe         `json:"read_probe"`
	IdempotencyProbe *IdempotencyProbe `json:"idempotency_probe,omitempty"`
}

type AuthConfig struct {
	Type            string `json:"type"`
	BearerTokenEnv  string `json:"bearer_token_env,omitempty"`
	BearerTokenFile string `json:"bearer_token_file,omitempty"`
}

type ToolProbe struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type IdempotencyProbe struct {
	Tool                   string             `json:"tool"`
	Arguments              map[string]any     `json:"arguments"`
	KeyArgument            string             `json:"key_argument"`
	ReceiptJSONPointer     string             `json:"receipt_json_pointer,omitempty"`
	StateVerificationProbe *StateVerification `json:"state_verification,omitempty"`
}

type StateVerification struct {
	Tool                   string         `json:"tool"`
	Arguments              map[string]any `json:"arguments"`
	KeyArgument            string         `json:"key_argument"`
	EffectCountJSONPointer string         `json:"effect_count_json_pointer"`
	ExpectedEffectCount    int64          `json:"expected_effect_count"`
}

func DecodeConfig(reader io.Reader) (Config, error) {
	if reader == nil {
		return Config{}, errors.New("MCP acceptance config reader is nil")
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read MCP acceptance config: %w", err)
	}
	if len(payload) > maxConfigBytes {
		return Config{}, errors.New("MCP acceptance config is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode MCP acceptance config: %w", err)
	}
	if err := ensureEOF(decoder, "MCP acceptance config"); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.SchemaVersion) != ConfigSchemaVersion {
		return fmt.Errorf("unsupported MCP acceptance config schema version %q", config.SchemaVersion)
	}
	if !targetNamePattern.MatchString(strings.TrimSpace(config.Target)) {
		return errors.New("MCP acceptance target must be a 1-64 byte stable identifier")
	}
	if config.Transport != remote.TransportStreamableHTTP && config.Transport != remote.TransportSSE {
		return fmt.Errorf("unsupported MCP acceptance transport %q", config.Transport)
	}
	if err := validateEndpoint(config.Endpoint); err != nil {
		return err
	}
	if len(config.AllowedHosts) > maxAllowedHosts {
		return fmt.Errorf("MCP acceptance allowed_hosts exceeds %d entries", maxAllowedHosts)
	}
	for _, host := range config.AllowedHosts {
		host = strings.TrimSpace(host)
		if host == "" || strings.ContainsAny(host, "/\\?#@ \t\r\n") {
			return fmt.Errorf("MCP acceptance allowed host %q is invalid", host)
		}
	}
	if err := config.Auth.validate(); err != nil {
		return err
	}
	parsed, _ := url.Parse(strings.TrimSpace(config.Endpoint))
	if parsed.Scheme != "https" && !explicitLoopbackTarget(parsed.Hostname(), config.AllowedHosts) {
		return errors.New("MCP acceptance endpoint must use https unless an explicit loopback host is allowlisted")
	}
	if err := config.ReadProbe.validate("read_probe"); err != nil {
		return err
	}
	if config.IdempotencyProbe != nil {
		if err := config.IdempotencyProbe.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (config Config) Hash() (string, error) {
	payload, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("encode MCP acceptance config: %w", err)
	}
	return hashBytes(payload), nil
}

func (config AuthConfig) validate() error {
	authType := strings.ToLower(strings.TrimSpace(config.Type))
	switch authType {
	case remote.AuthNone:
		if strings.TrimSpace(config.BearerTokenEnv) != "" || strings.TrimSpace(config.BearerTokenFile) != "" {
			return errors.New("MCP acceptance auth type none cannot reference a bearer token")
		}
	case remote.AuthBearer:
		envName := strings.TrimSpace(config.BearerTokenEnv)
		fileName := strings.TrimSpace(config.BearerTokenFile)
		if (envName == "") == (fileName == "") {
			return errors.New("MCP acceptance bearer auth requires exactly one token environment variable or file")
		}
		if envName != "" && !environmentPattern.MatchString(envName) {
			return errors.New("MCP acceptance bearer token environment variable is invalid")
		}
	default:
		return fmt.Errorf("unsupported MCP acceptance auth type %q", config.Type)
	}
	return nil
}

func (probe ToolProbe) validate(field string) error {
	if !toolNamePattern.MatchString(strings.TrimSpace(probe.Tool)) {
		return fmt.Errorf("%s tool name is invalid", field)
	}
	if probe.Arguments == nil {
		return fmt.Errorf("%s arguments are required", field)
	}
	return validateArguments(field, probe.Arguments)
}

func (probe IdempotencyProbe) validate() error {
	if err := (ToolProbe{Tool: probe.Tool, Arguments: probe.Arguments}).validate("idempotency_probe"); err != nil {
		return err
	}
	if !toolNamePattern.MatchString(strings.TrimSpace(probe.KeyArgument)) {
		return errors.New("idempotency_probe key_argument is invalid")
	}
	if _, exists := probe.Arguments[probe.KeyArgument]; exists {
		return errors.New("idempotency_probe arguments cannot supply the platform-owned idempotency key")
	}
	if probe.ReceiptJSONPointer != "" {
		if err := validateJSONPointer(probe.ReceiptJSONPointer); err != nil {
			return fmt.Errorf("idempotency_probe receipt_json_pointer: %w", err)
		}
	}
	if probe.StateVerificationProbe == nil {
		return nil
	}
	verification := probe.StateVerificationProbe
	if err := (ToolProbe{Tool: verification.Tool, Arguments: verification.Arguments}).validate("idempotency_probe.state_verification"); err != nil {
		return err
	}
	if !toolNamePattern.MatchString(strings.TrimSpace(verification.KeyArgument)) {
		return errors.New("idempotency_probe state verification key_argument is invalid")
	}
	if _, exists := verification.Arguments[verification.KeyArgument]; exists {
		return errors.New("idempotency_probe state verification arguments cannot supply the platform-owned idempotency key")
	}
	if err := validateJSONPointer(verification.EffectCountJSONPointer); err != nil {
		return fmt.Errorf("idempotency_probe state verification effect_count_json_pointer: %w", err)
	}
	if verification.ExpectedEffectCount < 0 {
		return errors.New("idempotency_probe state verification expected_effect_count cannot be negative")
	}
	return nil
}

func validateEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("MCP acceptance endpoint is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("MCP acceptance endpoint scheme must be http or https")
	}
	if parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return errors.New("MCP acceptance endpoint must contain a host and cannot contain credentials, query, or fragment")
	}
	return nil
}

func explicitLoopbackTarget(host string, allowedHosts []string) bool {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return false
	}
	for _, allowed := range allowedHosts {
		if strings.EqualFold(strings.Trim(strings.TrimSpace(allowed), "[]"), host) {
			return true
		}
	}
	return false
}

func validateArguments(field string, arguments map[string]any) error {
	payload, err := json.Marshal(arguments)
	if err != nil {
		return fmt.Errorf("%s arguments are not valid JSON: %w", field, err)
	}
	if len(payload) > maxArgumentsBytes {
		return fmt.Errorf("%s arguments exceed %d bytes", field, maxArgumentsBytes)
	}
	if sensitivePath := findSensitiveArgument(arguments, ""); sensitivePath != "" {
		return fmt.Errorf("%s contains forbidden credential-like field %q", field, sensitivePath)
	}
	return nil
}

func findSensitiveArgument(value any, path string) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			current := key
			if path != "" {
				current = path + "." + key
			}
			if _, sensitive := sensitiveArgumentNames[normalized]; sensitive {
				return current
			}
			if found := findSensitiveArgument(child, current); found != "" {
				return found
			}
		}
	case []any:
		for index, child := range typed {
			current := fmt.Sprintf("%s[%d]", path, index)
			if found := findSensitiveArgument(child, current); found != "" {
				return found
			}
		}
	}
	return ""
}

func validateJSONPointer(pointer string) error {
	if pointer == "" || pointer[0] != '/' {
		return errors.New("must be a non-empty RFC 6901 pointer")
	}
	for index := 0; index < len(pointer); index++ {
		if pointer[index] != '~' {
			continue
		}
		if index+1 >= len(pointer) || (pointer[index+1] != '0' && pointer[index+1] != '1') {
			return errors.New("contains an invalid RFC 6901 escape")
		}
		index++
	}
	return nil
}

func ensureEOF(decoder *json.Decoder, artifact string) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%s contains multiple JSON values", artifact)
		}
		return fmt.Errorf("decode %s trailer: %w", artifact, err)
	}
	return nil
}

func hashBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
