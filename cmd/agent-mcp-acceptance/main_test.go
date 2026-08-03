package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/mcp/acceptance"
	"twitter-clone/internal/module/agent/mcp/remote"
)

const commandTestToken = "0123456789abcdef0123456789abcdef"

func TestRunRequiresExplicitLivePermission(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "explicit --allow-live") {
		t.Fatalf("unexpected result: code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunProducesAndVerifiesSignedConformanceReport(t *testing.T) {
	handler, err := acceptance.NewConformanceHandler(commandTestToken)
	if err != nil {
		t.Fatalf("NewConformanceHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("TEST_MCP_COMMAND_TOKEN", commandTestToken)
	t.Setenv("TEST_MCP_COMMAND_INTEGRITY_KEY", commandTestToken)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	reportPath := filepath.Join(tempDir, "report.json")
	config := acceptance.Config{
		SchemaVersion: acceptance.ConfigSchemaVersion, Target: "command-test",
		Transport: remote.TransportStreamableHTTP, Endpoint: server.URL,
		AllowedHosts: []string{"127.0.0.1"},
		Auth:         acceptance.AuthConfig{Type: remote.AuthBearer, BearerTokenEnv: "TEST_MCP_COMMAND_TOKEN"},
		ReadProbe: acceptance.ToolProbe{
			Tool: acceptance.ConformanceReadTool, Arguments: map[string]any{"query": "acceptance"},
		},
		IdempotencyProbe: &acceptance.IdempotencyProbe{
			Tool: acceptance.ConformanceWriteTool, Arguments: map[string]any{"value": "acceptance"},
			KeyArgument: acceptance.ConformanceKeyArgument, ReceiptJSONPointer: "/receipt",
			StateVerificationProbe: &acceptance.StateVerification{
				Tool: acceptance.ConformanceWriteStatusTool, Arguments: map[string]any{},
				KeyArgument:            acceptance.ConformanceKeyArgument,
				EffectCountJSONPointer: "/effect_count", ExpectedEffectCount: 1,
			},
		},
	}
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("json.Marshal(config) error = %v", err)
	}
	if err := os.WriteFile(configPath, payload, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"--allow-live", "--allow-write", "--require-complete", "--require-signed",
		"--config", configPath, "--out", reportPath,
		"--integrity-key-env", "TEST_MCP_COMMAND_INTEGRITY_KEY",
		"--integrity-key-id", "command-test-v1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run failed: code=%d stderr=%q", code, stderr.String())
	}
	report, err := loadReport(reportPath)
	if err != nil {
		t.Fatalf("loadReport() error = %v", err)
	}
	if report.Status != acceptance.StatusPassed || report.Integrity == nil {
		t.Fatalf("unexpected report = %#v", report)
	}
	if err := acceptance.VerifyReport(report, []byte(commandTestToken), "command-test-v1"); err != nil {
		t.Fatalf("VerifyReport() error = %v", err)
	}
	reportPayload, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for _, forbidden := range []string{commandTestToken, server.URL} {
		if bytes.Contains(reportPayload, []byte(forbidden)) {
			t.Fatalf("report leaked %q: %s", forbidden, reportPayload)
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"--verify-report", reportPath,
		"--integrity-key-env", "TEST_MCP_COMMAND_INTEGRITY_KEY",
		"--integrity-key-id", "command-test-v1",
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "report verified") {
		t.Fatalf("verify failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunRefusesWriteFlagWithoutConfiguredProbe(t *testing.T) {
	t.Setenv("TEST_MCP_COMMAND_TOKEN", commandTestToken)
	config := acceptance.Config{
		SchemaVersion: acceptance.ConfigSchemaVersion, Target: "command-test",
		Transport: remote.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
		Auth:      acceptance.AuthConfig{Type: remote.AuthNone},
		ReadProbe: acceptance.ToolProbe{Tool: "lookup", Arguments: map[string]any{}},
	}
	payload, _ := json.Marshal(config)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--allow-live", "--allow-write", "--config", path}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "requires idempotency_probe") {
		t.Fatalf("unexpected result: code=%d stderr=%q", code, stderr.String())
	}
}

func TestWriteReportRefusesToReplaceExistingEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	original := acceptance.Report{
		SchemaVersion: acceptance.ReportSchemaVersion,
		Target:        "original",
		Status:        acceptance.StatusPassed,
	}
	var stdout bytes.Buffer
	if err := writeReport(&stdout, path, original); err != nil {
		t.Fatalf("writeReport(original) error = %v", err)
	}
	replacement := original
	replacement.Target = "replacement"
	if err := writeReport(&stdout, path, replacement); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("writeReport(replacement) error = %v", err)
	}
	stored, err := loadReport(path)
	if err != nil {
		t.Fatalf("loadReport() error = %v", err)
	}
	if stored.Target != original.Target {
		t.Fatalf("existing evidence was replaced: %#v", stored)
	}
}
