package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/mcp/remote"
	agentModel "twitter-clone/internal/module/agent/model"

	"github.com/mark3labs/mcp-go/mcp"
)

const acceptanceTestToken = "0123456789abcdef0123456789abcdef"

func TestRunnerExercisesConformanceServerWithoutLeakingSecrets(t *testing.T) {
	t.Setenv("TEST_MCP_ACCEPTANCE_TOKEN", acceptanceTestToken)
	handler, err := NewConformanceHandler(acceptanceTestToken)
	if err != nil {
		t.Fatalf("NewConformanceHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	config := conformanceTestConfig(server.URL)
	source, err := NewCredentialSource(config.Auth)
	if err != nil {
		t.Fatalf("NewCredentialSource() error = %v", err)
	}
	discoverer := remote.NewSDKDiscoverer(
		agentModel.NewEndpointPolicy("127.0.0.1"),
		3*time.Second,
		remote.WithClientPool(remote.ClientPoolConfig{
			Enabled: true, MaxSessions: 2, MaxSessionsPerConnection: 1,
			IdleTimeout: time.Minute, AcquireTimeout: time.Second,
		}),
	)
	defer discoverer.Close()
	runner, err := NewRunner(discoverer, source)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background(), config, RunOptions{
		Environment: "test", AllowWrite: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v, report = %#v", err, report)
	}
	if report.Status != StatusPassed || report.Summary.Failed != 0 || report.DiscoveredToolCount != 4 {
		t.Fatalf("unexpected report = %#v", report)
	}
	step := findStep(report, "idempotency_probe")
	if step.Status != StepPassed || step.EvidenceLevel != "observable_state" || step.EvidenceSHA256 == "" {
		t.Fatalf("unexpected idempotency step = %#v", step)
	}
	payload, err := MarshalReport(report)
	if err != nil {
		t.Fatalf("MarshalReport() error = %v", err)
	}
	for _, forbidden := range []string{acceptanceTestToken, server.URL, `"endpoint"`} {
		if bytes.Contains(payload, []byte(forbidden)) {
			t.Fatalf("report leaked %q: %s", forbidden, payload)
		}
	}
}

func TestRunnerMarksWriteProbePartialWithoutExplicitPermission(t *testing.T) {
	client := &fakeAcceptanceClient{tools: conformanceToolsForFake()}
	runner, err := NewRunner(client, staticCredentialSource{})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	config := conformanceTestConfig("https://mcp.example.com/mcp")
	config.Auth = AuthConfig{Type: remote.AuthNone}
	report, err := runner.Run(context.Background(), config, RunOptions{Environment: "test"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Status != StatusPartial || findStep(report, "idempotency_probe").ErrorCode != "write_probe_not_authorized" {
		t.Fatalf("unexpected partial report = %#v", report)
	}
	if client.writeCalls != 0 {
		t.Fatalf("write probe called %d times without permission", client.writeCalls)
	}
}

func TestRunnerObservesCredentialRotationWithFreshIdentity(t *testing.T) {
	client := &fakeAcceptanceClient{tools: conformanceToolsForFake()}
	source := &rotatingCredentialSource{}
	runner, err := NewRunner(client, source)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	config := conformanceTestConfig("https://mcp.example.com/mcp")
	report, err := runner.Run(context.Background(), config, RunOptions{
		Environment: "test", ExpectCredentialRotation: true,
		RotationTimeout: 100 * time.Millisecond, RotationPollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run() error = %v, report = %#v", err, report)
	}
	step := findStep(report, "credential_rotation")
	if step.Status != StepPassed || step.EvidenceLevel != "projected_secret_rotation" {
		t.Fatalf("unexpected rotation step = %#v", step)
	}
	if len(client.pingIdentities) < 2 ||
		client.pingIdentities[0] == client.pingIdentities[len(client.pingIdentities)-1] {
		t.Fatalf("rotation did not use a fresh session identity: %#v", client)
	}
}

func TestRunnerRetriesRotatedCredentialUntilRemoteAcceptsIt(t *testing.T) {
	pingCalls := 0
	client := &fakeAcceptanceClient{
		tools: conformanceToolsForFake(),
		ping: func(context.Context) error {
			pingCalls++
			if pingCalls == 2 {
				return errors.New("rotated credential has not propagated")
			}
			return nil
		},
	}
	runner, err := NewRunner(client, &rotatingCredentialSource{})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	config := conformanceTestConfig("https://mcp.example.com/mcp")
	report, err := runner.Run(context.Background(), config, RunOptions{
		Environment: "test", ExpectCredentialRotation: true,
		RotationTimeout: 100 * time.Millisecond, RotationPollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run() error = %v, report = %#v", err, report)
	}
	if step := findStep(report, "credential_rotation"); step.Status != StepPassed {
		t.Fatalf("unexpected rotation step = %#v", step)
	}
	if pingCalls < 3 {
		t.Fatalf("rotated credential was not retried, ping calls = %d", pingCalls)
	}
}

func TestRunnerRecordsTimeoutWithoutErrorText(t *testing.T) {
	client := &fakeAcceptanceClient{ping: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	runner, err := NewRunner(client, staticCredentialSource{})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	config := conformanceTestConfig("https://mcp.example.com/mcp")
	config.Auth = AuthConfig{Type: remote.AuthNone}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	report, err := runner.Run(ctx, config, RunOptions{Environment: "test"})
	if !errors.Is(err, ErrAcceptanceFailed) {
		t.Fatalf("Run() error = %v", err)
	}
	step := findStep(report, "protocol_ping")
	if report.Status != StatusFailed || step.ErrorCode != "timeout" {
		t.Fatalf("unexpected timeout report = %#v", report)
	}
	payload, _ := json.Marshal(report)
	if bytes.Contains(payload, []byte("context deadline exceeded")) {
		t.Fatalf("report leaked raw error text: %s", payload)
	}
}

func TestDecodeConfigRejectsUnknownAndSensitiveFields(t *testing.T) {
	valid := conformanceTestConfig("https://mcp.example.com/mcp")
	payload, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	payload = bytes.Replace(payload, []byte(`"auth":{`), []byte(`"auth":{"api_key":"secret",`), 1)
	if _, err := DecodeConfig(bytes.NewReader(payload)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown credential field error = %v", err)
	}

	valid.ReadProbe.Arguments["token"] = "plaintext"
	payload, _ = json.Marshal(valid)
	if _, err := DecodeConfig(bytes.NewReader(payload)); err == nil || !strings.Contains(err.Error(), "forbidden credential-like field") {
		t.Fatalf("sensitive probe argument error = %v", err)
	}
}

func TestDecodeConfigRejectsPlainHTTPForNonLoopbackTarget(t *testing.T) {
	config := conformanceTestConfig("http://mcp.example.com/mcp")
	config.Auth = AuthConfig{Type: remote.AuthNone}
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if _, err := DecodeConfig(bytes.NewReader(payload)); err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("plain HTTP endpoint error = %v", err)
	}
}

func TestReportIntegrityRejectsTampering(t *testing.T) {
	report := Report{
		SchemaVersion: ReportSchemaVersion, Target: "target", Environment: "test",
		Status: StatusPassed, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
		ConfigSHA256: strings.Repeat("a", 64), EndpointSHA256: strings.Repeat("b", 64),
		Transport: remote.TransportStreamableHTTP, CredentialSource: "bearer_env",
		Steps: []StepResult{{Name: "protocol_ping", Status: StepPassed}},
	}
	key := []byte(acceptanceTestToken)
	if err := SignReport(&report, key, "test-key-v1", time.Now()); err != nil {
		t.Fatalf("SignReport() error = %v", err)
	}
	if err := VerifyReport(report, key, "test-key-v1"); err != nil {
		t.Fatalf("VerifyReport() error = %v", err)
	}
	report.Status = StatusFailed
	if err := VerifyReport(report, key, "test-key-v1"); err == nil || !strings.Contains(err.Error(), "payload hash mismatch") {
		t.Fatalf("tampered VerifyReport() error = %v", err)
	}
}

func TestAcceptanceEvidenceRejectsOversizedRemotePayloads(t *testing.T) {
	t.Parallel()

	tools := make([]mcp.Tool, maxAcceptedToolCount+1)
	if _, _, err := summarizeTools(tools); err == nil {
		t.Fatal("summarizeTools() accepted an oversized catalog")
	}
	result := mcp.NewToolResultStructured(
		map[string]any{"payload": strings.Repeat("x", maxAcceptedResultBytes+1)},
		"oversized",
	)
	if _, err := successfulResultDigest(result); err == nil {
		t.Fatal("successfulResultDigest() accepted an oversized result")
	}
	if _, err := DecodeReport(strings.NewReader(strings.Repeat(" ", maxReportBytes+1))); err == nil {
		t.Fatal("DecodeReport() accepted an oversized report")
	}
}

func conformanceTestConfig(endpoint string) Config {
	return Config{
		SchemaVersion: ConfigSchemaVersion, Target: "conformance-test",
		Transport: remote.TransportStreamableHTTP, Endpoint: endpoint,
		AllowedHosts: []string{"127.0.0.1"},
		Auth:         AuthConfig{Type: remote.AuthBearer, BearerTokenEnv: "TEST_MCP_ACCEPTANCE_TOKEN"},
		ReadProbe: ToolProbe{
			Tool: ConformanceReadTool, Arguments: map[string]any{"query": "acceptance"},
		},
		IdempotencyProbe: &IdempotencyProbe{
			Tool: ConformanceWriteTool, Arguments: map[string]any{"value": "acceptance"},
			KeyArgument: ConformanceKeyArgument, ReceiptJSONPointer: "/receipt",
			StateVerificationProbe: &StateVerification{
				Tool: ConformanceWriteStatusTool, Arguments: map[string]any{},
				KeyArgument: ConformanceKeyArgument, EffectCountJSONPointer: "/effect_count",
				ExpectedEffectCount: 1,
			},
		},
	}
}

func findStep(report Report, name string) StepResult {
	for _, step := range report.Steps {
		if step.Name == name {
			return step
		}
	}
	return StepResult{}
}

type staticCredentialSource struct{}

func (staticCredentialSource) Load(context.Context) (Credential, error) {
	return Credential{BearerToken: "static", Identity: "identity-static"}, nil
}
func (staticCredentialSource) Kind() string    { return "test" }
func (staticCredentialSource) Rotatable() bool { return false }

type rotatingCredentialSource struct {
	mu    sync.Mutex
	loads int
}

func (source *rotatingCredentialSource) Load(context.Context) (Credential, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.loads++
	if source.loads == 1 {
		return Credential{BearerToken: "old", Identity: "identity-old"}, nil
	}
	return Credential{BearerToken: "new", Identity: "identity-new"}, nil
}
func (*rotatingCredentialSource) Kind() string    { return "bearer_file" }
func (*rotatingCredentialSource) Rotatable() bool { return true }

type fakeAcceptanceClient struct {
	tools          []mcp.Tool
	ping           func(context.Context) error
	pingIdentities []string
	writeCalls     int
}

func (client *fakeAcceptanceClient) Ping(ctx context.Context, request remote.DiscoveryRequest) error {
	client.pingIdentities = append(client.pingIdentities, request.CredentialIdentity)
	if client.ping != nil {
		return client.ping(ctx)
	}
	return nil
}

func (client *fakeAcceptanceClient) Discover(context.Context, remote.DiscoveryRequest) ([]mcp.Tool, error) {
	return append([]mcp.Tool(nil), client.tools...), nil
}

func (client *fakeAcceptanceClient) Call(
	_ context.Context,
	_ remote.DiscoveryRequest,
	toolName string,
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	if toolName == ConformanceWriteTool {
		client.writeCalls++
	}
	if toolName == ConformanceWriteStatusTool {
		return mcp.NewToolResultStructured(map[string]any{
			"effect_count": float64(1), "receipt": "receipt",
		}, "status"), nil
	}
	if toolName == ConformanceWriteTool {
		return mcp.NewToolResultStructured(map[string]any{
			"effect_count": float64(1), "receipt": "receipt",
		}, "write"), nil
	}
	return mcp.NewToolResultStructured(map[string]any{"echo": arguments["query"]}, "read"), nil
}

func conformanceToolsForFake() []mcp.Tool {
	read := mcp.NewTool(
		ConformanceReadTool, mcp.WithString("query", mcp.Required()),
		mcp.WithReadOnlyHintAnnotation(true), mcp.WithDestructiveHintAnnotation(false),
	)
	write := mcp.NewTool(
		ConformanceWriteTool,
		mcp.WithString("value", mcp.Required()),
		mcp.WithString(ConformanceKeyArgument, mcp.Required()),
		mcp.WithIdempotentHintAnnotation(true),
	)
	write.Meta = mcp.NewMetaFromMap(map[string]any{
		remote.IdempotencyKeyArgumentMetaField: ConformanceKeyArgument,
	})
	status := mcp.NewTool(
		ConformanceWriteStatusTool, mcp.WithString(ConformanceKeyArgument, mcp.Required()),
		mcp.WithReadOnlyHintAnnotation(true), mcp.WithDestructiveHintAnnotation(false),
	)
	return []mcp.Tool{read, write, status}
}
