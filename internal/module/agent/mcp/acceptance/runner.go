package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/mcp/remote"
	agentModel "twitter-clone/internal/module/agent/model"

	"github.com/mark3labs/mcp-go/mcp"
)

var ErrAcceptanceFailed = errors.New("MCP acceptance failed")

const (
	maxAcceptedToolCount       = 256
	maxAcceptedToolSchemaBytes = 128 * 1024
	maxAcceptedCatalogBytes    = 2 * 1024 * 1024
	maxAcceptedResultBytes     = 2 * 1024 * 1024
)

type Client interface {
	remote.Discoverer
	remote.Caller
	remote.HealthProber
}

type RunOptions struct {
	Environment              string
	AllowWrite               bool
	ExpectCredentialRotation bool
	RotationTimeout          time.Duration
	RotationPollInterval     time.Duration
}

type Runner struct {
	client      Client
	credentials CredentialSource
	now         func() time.Time
}

func NewRunner(client Client, credentials CredentialSource) (*Runner, error) {
	if client == nil {
		return nil, errors.New("MCP acceptance client is required")
	}
	if credentials == nil {
		return nil, errors.New("MCP acceptance credential source is required")
	}
	return &Runner{client: client, credentials: credentials, now: time.Now}, nil
}

func (runner *Runner) Run(ctx context.Context, config Config, options RunOptions) (Report, error) {
	if runner == nil || runner.client == nil || runner.credentials == nil {
		return Report{}, errors.New("MCP acceptance runner is unavailable")
	}
	if err := config.Validate(); err != nil {
		return Report{}, err
	}
	configHash, err := config.Hash()
	if err != nil {
		return Report{}, err
	}
	startedAt := runner.now().UTC()
	report := Report{
		SchemaVersion: ReportSchemaVersion,
		Target:        strings.TrimSpace(config.Target), Environment: normalizedEnvironment(options.Environment),
		StartedAt: startedAt, ConfigSHA256: configHash,
		EndpointSHA256: hashBytes([]byte(strings.TrimSpace(config.Endpoint))),
		Transport:      config.Transport, CredentialSource: runner.credentials.Kind(),
		Steps: make([]StepResult, 0, 6),
		Limitations: []string{
			"Remote MCP annotations and metadata are server claims; acceptance evidence does not prove strict exactly-once behavior.",
		},
	}

	var initialCredential Credential
	if !runner.step(&report, "credential_load", func() (string, string, error) {
		loaded, loadErr := runner.credentials.Load(ctx)
		if loadErr != nil {
			return "", "", loadErr
		}
		initialCredential = loaded
		return hashBytes([]byte(runner.credentials.Kind())), "credential_reference", nil
	}) {
		return runner.finish(report), ErrAcceptanceFailed
	}
	request := discoveryRequest(config, configHash, initialCredential)

	if !runner.step(&report, "protocol_ping", func() (string, string, error) {
		if pingErr := runner.client.Ping(ctx, request); pingErr != nil {
			return "", "", pingErr
		}
		return hashBytes([]byte("ping:ok")), "protocol", nil
	}) {
		return runner.finish(report), ErrAcceptanceFailed
	}

	var tools []mcp.Tool
	if !runner.step(&report, "tool_discovery", func() (string, string, error) {
		discovered, discoverErr := runner.client.Discover(ctx, request)
		if discoverErr != nil {
			return "", "", discoverErr
		}
		contracts, catalogHash, summarizeErr := summarizeTools(discovered)
		if summarizeErr != nil {
			return "", "", summarizeErr
		}
		tools = discovered
		report.ToolCatalogSHA256 = catalogHash
		report.DiscoveredToolCount = len(discovered)
		report.ToolContracts = contracts
		return catalogHash, "schema_catalog", nil
	}) {
		return runner.finish(report), ErrAcceptanceFailed
	}

	if !runner.step(&report, "read_probe", func() (string, string, error) {
		tool, findErr := findTool(tools, config.ReadProbe.Tool)
		if findErr != nil {
			return "", "", findErr
		}
		if !declaredReadOnly(tool) {
			return "", "", errors.New("configured read probe is not declared read-only")
		}
		result, callErr := runner.client.Call(ctx, request, tool.Name, cloneArguments(config.ReadProbe.Arguments))
		if callErr != nil {
			return "", "", callErr
		}
		digest, resultErr := successfulResultDigest(result)
		if resultErr != nil {
			return "", "", resultErr
		}
		return digest, "read_result_digest", nil
	}) {
		return runner.finish(report), ErrAcceptanceFailed
	}

	if config.IdempotencyProbe != nil {
		if !options.AllowWrite {
			report.Steps = append(report.Steps, StepResult{
				Name: "idempotency_probe", Status: StepSkipped,
				ErrorCode: "write_probe_not_authorized",
			})
			report.Limitations = append(report.Limitations,
				"The configured idempotency probe was skipped because --allow-write was not supplied.")
		} else if !runner.step(&report, "idempotency_probe", func() (string, string, error) {
			return runner.runIdempotencyProbe(ctx, request, configHash, tools, *config.IdempotencyProbe)
		}) {
			return runner.finish(report), ErrAcceptanceFailed
		}
	}

	if options.ExpectCredentialRotation {
		if !runner.step(&report, "credential_rotation", func() (string, string, error) {
			return runner.waitForCredentialRotation(ctx, config, configHash, initialCredential, options)
		}) {
			return runner.finish(report), ErrAcceptanceFailed
		}
	}
	return runner.finish(report), nil
}

func (runner *Runner) runIdempotencyProbe(
	ctx context.Context,
	request remote.DiscoveryRequest,
	configHash string,
	tools []mcp.Tool,
	probe IdempotencyProbe,
) (string, string, error) {
	tool, err := findTool(tools, probe.Tool)
	if err != nil {
		return "", "", err
	}
	if err := validateIdempotencyContract(tool, probe.KeyArgument); err != nil {
		return "", "", err
	}
	executionKey := fmt.Sprintf("%s:%d:%s", configHash, runner.now().UnixNano(), tool.Name)
	remoteKey, err := remote.DeriveRemoteIdempotencyKey(executionKey)
	if err != nil {
		return "", "", err
	}
	arguments := cloneArguments(probe.Arguments)
	arguments[probe.KeyArgument] = remoteKey

	first, err := runner.client.Call(ctx, request, tool.Name, arguments)
	if err != nil {
		return "", "", err
	}
	firstDigest, err := successfulResultDigest(first)
	if err != nil {
		return "", "", err
	}
	second, err := runner.client.Call(ctx, request, tool.Name, arguments)
	if err != nil {
		return "", "", err
	}
	secondDigest, err := successfulResultDigest(second)
	if err != nil {
		return "", "", err
	}
	consistent := firstDigest == secondDigest
	if probe.ReceiptJSONPointer != "" {
		firstReceipt, extractErr := extractResultPointer(first, probe.ReceiptJSONPointer)
		if extractErr != nil {
			return "", "", extractErr
		}
		secondReceipt, extractErr := extractResultPointer(second, probe.ReceiptJSONPointer)
		if extractErr != nil {
			return "", "", extractErr
		}
		firstReceiptJSON, _ := json.Marshal(firstReceipt)
		secondReceiptJSON, _ := json.Marshal(secondReceipt)
		consistent = string(firstReceiptJSON) == string(secondReceiptJSON)
	}
	if !consistent {
		return "", "", errors.New("remote idempotency replay returned inconsistent evidence")
	}

	evidenceMaterial := firstDigest + ":" + secondDigest
	evidenceLevel := "response_consistency"
	if verification := probe.StateVerificationProbe; verification != nil {
		verificationTool, findErr := findTool(tools, verification.Tool)
		if findErr != nil {
			return "", "", findErr
		}
		if !declaredReadOnly(verificationTool) {
			return "", "", errors.New("idempotency state verification tool is not declared read-only")
		}
		verificationArguments := cloneArguments(verification.Arguments)
		verificationArguments[verification.KeyArgument] = remoteKey
		result, callErr := runner.client.Call(ctx, request, verificationTool.Name, verificationArguments)
		if callErr != nil {
			return "", "", callErr
		}
		resultDigest, resultErr := successfulResultDigest(result)
		if resultErr != nil {
			return "", "", resultErr
		}
		value, pointerErr := extractResultPointer(result, verification.EffectCountJSONPointer)
		if pointerErr != nil {
			return "", "", pointerErr
		}
		effectCount, convertErr := integerValue(value)
		if convertErr != nil {
			return "", "", convertErr
		}
		if effectCount != verification.ExpectedEffectCount {
			return "", "", fmt.Errorf("remote state verification returned unexpected effect count")
		}
		evidenceMaterial += ":" + resultDigest + ":" + strconv.FormatInt(effectCount, 10)
		evidenceLevel = "observable_state"
	}
	return hashBytes([]byte(evidenceMaterial)), evidenceLevel, nil
}

func (runner *Runner) waitForCredentialRotation(
	ctx context.Context,
	config Config,
	configHash string,
	initial Credential,
	options RunOptions,
) (string, string, error) {
	if !runner.credentials.Rotatable() {
		return "", "", errors.New("configured MCP acceptance credential source cannot be observed for rotation")
	}
	timeout := options.RotationTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	pollInterval := options.RotationPollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	rotationCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-rotationCtx.Done():
			return "", "", rotationCtx.Err()
		case <-ticker.C:
			current, err := runner.credentials.Load(rotationCtx)
			if err != nil {
				continue
			}
			if current.Identity == initial.Identity {
				continue
			}
			rotatedRequest := discoveryRequest(config, configHash, current)
			if err := runner.client.Ping(rotationCtx, rotatedRequest); err != nil {
				continue
			}
			return hashBytes([]byte(initial.Identity + ":" + current.Identity)), "projected_secret_rotation", nil
		}
	}
}

func (runner *Runner) step(
	report *Report,
	name string,
	run func() (evidenceSHA256 string, evidenceLevel string, err error),
) bool {
	startedAt := runner.now()
	evidence, level, err := run()
	duration := runner.now().Sub(startedAt)
	if duration < 0 {
		duration = 0
	}
	result := StepResult{
		Name: name, Status: StepPassed, DurationMillis: duration.Milliseconds(),
		EvidenceSHA256: evidence, EvidenceLevel: level,
	}
	if err != nil {
		result.Status = StepFailed
		result.ErrorCode = classifyError(err)
		result.EvidenceSHA256 = ""
		result.EvidenceLevel = ""
	}
	report.Steps = append(report.Steps, result)
	return err == nil
}

func (runner *Runner) finish(report Report) Report {
	report.FinishedAt = runner.now().UTC()
	report.Summary = StepSummary{}
	for _, step := range report.Steps {
		switch step.Status {
		case StepPassed:
			report.Summary.Passed++
		case StepSkipped:
			report.Summary.Skipped++
		case StepFailed:
			report.Summary.Failed++
		}
	}
	switch {
	case report.Summary.Failed > 0:
		report.Status = StatusFailed
	case report.Summary.Skipped > 0:
		report.Status = StatusPartial
	default:
		report.Status = StatusPassed
	}
	return report
}

func summarizeTools(tools []mcp.Tool) ([]ToolContractEvidence, string, error) {
	if len(tools) == 0 {
		return nil, "", errors.New("external MCP server returned no tools")
	}
	if len(tools) > maxAcceptedToolCount {
		return nil, "", fmt.Errorf("external MCP server returned more than %d tools", maxAcceptedToolCount)
	}
	sorted := append([]mcp.Tool(nil), tools...)
	for index := 1; index < len(sorted); index++ {
		for current := index; current > 0 && sorted[current].Name < sorted[current-1].Name; current-- {
			sorted[current], sorted[current-1] = sorted[current-1], sorted[current]
		}
	}
	contracts := make([]ToolContractEvidence, 0, len(sorted))
	seen := make(map[string]struct{}, len(sorted))
	totalBytes := 0
	for _, tool := range sorted {
		if !toolNamePattern.MatchString(strings.TrimSpace(tool.Name)) {
			return nil, "", fmt.Errorf("external MCP tool name %q is invalid", tool.Name)
		}
		if _, exists := seen[tool.Name]; exists {
			return nil, "", fmt.Errorf("external MCP tool name %q is duplicated", tool.Name)
		}
		seen[tool.Name] = struct{}{}
		payload, err := json.Marshal(tool)
		if err != nil {
			return nil, "", fmt.Errorf("encode external MCP tool %q: %w", tool.Name, err)
		}
		if len(payload) > maxAcceptedToolSchemaBytes {
			return nil, "", fmt.Errorf("external MCP tool %q schema exceeds the acceptance limit", tool.Name)
		}
		totalBytes += len(payload)
		if totalBytes > maxAcceptedCatalogBytes {
			return nil, "", errors.New("external MCP tool catalog exceeds the acceptance limit")
		}
		argument, _ := idempotencyArgument(tool)
		contracts = append(contracts, ToolContractEvidence{
			Name: tool.Name, SchemaSHA256: hashBytes(payload),
			DeclaredReadOnly:       declaredReadOnly(tool),
			DeclaredIdempotent:     tool.Annotations.IdempotentHint != nil && *tool.Annotations.IdempotentHint,
			HasIdempotencyArgument: argument != "",
		})
	}
	payload, err := json.Marshal(sorted)
	if err != nil {
		return nil, "", fmt.Errorf("encode external MCP tool catalog: %w", err)
	}
	if len(payload) > maxAcceptedCatalogBytes {
		return nil, "", errors.New("external MCP tool catalog exceeds the acceptance limit")
	}
	return contracts, hashBytes(payload), nil
}

func findTool(tools []mcp.Tool, name string) (mcp.Tool, error) {
	for _, tool := range tools {
		if tool.Name == strings.TrimSpace(name) {
			return tool, nil
		}
	}
	return mcp.Tool{}, fmt.Errorf("configured MCP acceptance tool %q was not discovered", name)
}

func declaredReadOnly(tool mcp.Tool) bool {
	return tool.Annotations.ReadOnlyHint != nil && *tool.Annotations.ReadOnlyHint &&
		(tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint)
}

func validateIdempotencyContract(tool mcp.Tool, expectedArgument string) error {
	if tool.Annotations.IdempotentHint == nil || !*tool.Annotations.IdempotentHint {
		return errors.New("configured write probe does not declare idempotentHint=true")
	}
	argument, err := idempotencyArgument(tool)
	if err != nil {
		return err
	}
	if argument != expectedArgument {
		return errors.New("configured write probe idempotency key metadata does not match the acceptance config")
	}
	payload, err := json.Marshal(tool)
	if err != nil {
		return err
	}
	var wire struct {
		InputSchema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"inputSchema"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	property, exists := wire.InputSchema.Properties[argument]
	if !exists || property.Type != "string" {
		return errors.New("configured write probe idempotency key must be a string input property")
	}
	for _, required := range wire.InputSchema.Required {
		if required == argument {
			return nil
		}
	}
	return errors.New("configured write probe idempotency key input must be required")
}

func idempotencyArgument(tool mcp.Tool) (string, error) {
	if tool.Meta == nil || tool.Meta.AdditionalFields == nil {
		return "", nil
	}
	value, exists := tool.Meta.AdditionalFields[remote.IdempotencyKeyArgumentMetaField]
	if !exists {
		return "", nil
	}
	argument, ok := value.(string)
	argument = strings.TrimSpace(argument)
	if !ok || !toolNamePattern.MatchString(argument) {
		return "", errors.New("external MCP idempotency key metadata is invalid")
	}
	return argument, nil
}

func successfulResultDigest(result *mcp.CallToolResult) (string, error) {
	if result == nil {
		return "", errors.New("external MCP tool returned an empty result")
	}
	if result.IsError {
		return "", errors.New("external MCP tool returned an error result")
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode external MCP result evidence: %w", err)
	}
	if len(payload) > maxAcceptedResultBytes {
		return "", errors.New("external MCP tool result exceeds the acceptance limit")
	}
	return hashBytes(payload), nil
}

func extractResultPointer(result *mcp.CallToolResult, pointer string) (any, error) {
	if result == nil || result.StructuredContent == nil {
		return nil, errors.New("external MCP result does not contain structured content")
	}
	return extractJSONPointer(result.StructuredContent, pointer)
}

func extractJSONPointer(value any, pointer string) (any, error) {
	if err := validateJSONPointer(pointer); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode structured result pointer target: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var current any
	if err := decoder.Decode(&current); err != nil {
		return nil, fmt.Errorf("normalize structured result pointer target: %w", err)
	}
	for _, rawPart := range strings.Split(pointer[1:], "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(rawPart, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			next, exists := typed[part]
			if !exists {
				return nil, fmt.Errorf("structured result pointer %q was not found", pointer)
			}
			current = next
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, fmt.Errorf("structured result pointer %q contains an invalid array index", pointer)
			}
			current = typed[index]
		default:
			return nil, fmt.Errorf("structured result pointer %q traverses a scalar", pointer)
		}
	}
	return current, nil
}

func integerValue(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		converted := int64(typed)
		if float64(converted) != typed {
			return 0, errors.New("state verification effect count is not an integer")
		}
		return converted, nil
	case json.Number:
		return typed.Int64()
	default:
		return 0, errors.New("state verification effect count is not numeric")
	}
}

func discoveryRequest(config Config, configHash string, credential Credential) remote.DiscoveryRequest {
	connectionID := "acceptance_" + configHash[:16]
	return remote.DiscoveryRequest{
		ConnectionID: connectionID, CredentialVersion: 1,
		CredentialIdentity: credential.Identity,
		Transport:          config.Transport, Endpoint: strings.TrimSpace(config.Endpoint),
		BearerToken: credential.BearerToken,
	}
}

func cloneArguments(arguments map[string]any) map[string]any {
	cloned := make(map[string]any, len(arguments)+1)
	for key, value := range arguments {
		cloned[key] = value
	}
	return cloned
}

func normalizedEnvironment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "local"
	}
	runes := []rune(value)
	if len(runes) > 64 {
		return string(runes[:64])
	}
	return value
}

func classifyError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, agentModel.ErrEndpointNotAllowed):
		return "endpoint_not_allowed"
	case errors.Is(err, remote.ErrClientPoolSaturated):
		return "client_pool_saturated"
	case errors.Is(err, remote.ErrClientPoolClosed):
		return "client_pool_closed"
	case errors.Is(err, remote.ErrConnectionInvalidated):
		return "connection_invalidated"
	default:
		return "acceptance_check_failed"
	}
}
