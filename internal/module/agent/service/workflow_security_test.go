package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

func TestValidateWorkflowSecurityRejectsNestedPlaintextAPIKeys(t *testing.T) {
	workflow := &dsl.WorkflowDSL{Nodes: []dsl.NodeDSL{{
		ID: "llm", Type: "llm",
		Properties: json.RawMessage(`{"provider":"custom","auth":{"apiKey":"secret"}}`),
	}}}
	if err := validateWorkflowSecurity(workflow); !errors.Is(err, ErrPlaintextWorkflowCredential) {
		t.Fatalf("validateWorkflowSecurity() error = %v", err)
	}

	workflow.Nodes[0].Properties = json.RawMessage(`{"credential_ref":"tenant.default"}`)
	if err := validateWorkflowSecurity(workflow); err != nil {
		t.Fatalf("credential_ref should be accepted: %v", err)
	}
}

func TestRedactWorkflowDSLSecretsRemovesLegacyAPIKeys(t *testing.T) {
	raw := `{"name":"legacy","nodes":[{"id":"llm","type":"llm","properties":{"api_key":"secret","credential_ref":"tenant.default"}}],"edges":[]}`
	redacted, err := redactWorkflowDSLSecrets(raw)
	if err != nil {
		t.Fatalf("redactWorkflowDSLSecrets() error = %v", err)
	}
	if strings.Contains(redacted, "secret") || strings.Contains(redacted, "api_key") || !strings.Contains(redacted, "credential_ref") {
		t.Fatalf("redacted DSL = %s", redacted)
	}
}

func TestParseWorkflowInputRejectsPlaintextAPIKeys(t *testing.T) {
	_, err := parseWorkflowInput(`{"user_input":"hello","api_key":"secret"}`)
	if !errors.Is(err, ErrPlaintextWorkflowCredential) {
		t.Fatalf("parseWorkflowInput() error = %v", err)
	}
}
