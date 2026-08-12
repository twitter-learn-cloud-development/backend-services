package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	externalMCPConnectionID = "mcpconn_0123456789abcdef0123456789abcdef"
	externalMCPSnapshotID   = "mcpsnap_0123456789abcdef0123456789abcdef"
	externalMCPSchemaHash   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

type staticExternalMCPToolCatalog struct {
	bindings []ExternalMCPToolBinding
	err      error
	userID   uint64
	calls    int
}

func (catalog *staticExternalMCPToolCatalog) ListExternalMCPTools(
	_ context.Context,
	userID uint64,
) ([]ExternalMCPToolBinding, error) {
	catalog.calls++
	catalog.userID = userID
	return append([]ExternalMCPToolBinding(nil), catalog.bindings...), catalog.err
}

func TestExternalMCPEnvironmentFiltersTenantCatalogAndTask(t *testing.T) {
	read := externalMCPBinding("crm.lookup", "lookup", agentRuntime.ToolCategoryRead, false)
	write := externalMCPBinding("docs.create", "create", agentRuntime.ToolCategoryWrite, true)
	write.ServerID = "docs"
	write.ConnectionID = "mcpconn_fedcba9876543210fedcba9876543210"
	write.SnapshotID = "mcpsnap_fedcba9876543210fedcba9876543210"
	write.PolicySnapshotID = write.SnapshotID
	catalog := &staticExternalMCPToolCatalog{bindings: []ExternalMCPToolBinding{write, read}}
	environment, err := NewExternalMCPEnvironment(catalog, 42)
	if err != nil {
		t.Fatalf("NewExternalMCPEnvironment() error = %v", err)
	}

	tools, err := environment.Tools(context.Background(), externalMCPTask(read.Tool.Name))
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if catalog.userID != 42 {
		t.Fatalf("catalog user = %d, want 42", catalog.userID)
	}
	if len(tools) != 1 || tools[0].Name != read.Tool.Name || tools[0].RequiresApproval {
		t.Fatalf("Tools() = %+v, want exact read task intersection", tools)
	}
	tools[0].InputSchema[0] = '['
	if catalog.bindings[1].Tool.InputSchema[0] != '{' {
		t.Fatal("Tools() exposed mutable MCP input schema")
	}
}

func TestExternalMCPEnvironmentSnapshotTracksBindingWithoutLeakingSensitiveCatalog(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	secret := "schema-secret-default"
	binding := externalMCPBinding("crm.lookup", "lookup", agentRuntime.ToolCategoryRead, false)
	binding.Tool.Description = "private connector description " + secret
	binding.Tool.InputSchema = json.RawMessage(`{"type":"object","properties":{"token":{"default":"` + secret + `"}}}`)
	catalog := &staticExternalMCPToolCatalog{bindings: []ExternalMCPToolBinding{binding}}
	environment, err := NewExternalMCPEnvironment(catalog, 42, WithExternalMCPClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewExternalMCPEnvironment() error = %v", err)
	}
	task := externalMCPTask(binding.Tool.Name)

	before, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatalf("Snapshot(before) error = %v", err)
	}
	catalog.bindings[0].Tool.Description = "changed presentation only"
	after, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseAfter,
	})
	if err != nil {
		t.Fatalf("Snapshot(after) error = %v", err)
	}
	if before.ID != after.ID || before.Digest != after.Digest || before.Reference != after.Reference {
		t.Fatalf("presentation metadata changed binding identity: before=%+v after=%+v", before, after)
	}
	if before.Environment != ExternalMCPEnvironmentName || !before.CapturedAt.Equal(now.UTC()) {
		t.Fatalf("Snapshot() identity/time = %+v", before)
	}
	serialized := string(before.Metadata) + before.Reference + before.Digest + before.ID
	for _, forbidden := range []string{
		secret, externalMCPConnectionID, externalMCPSnapshotID, externalMCPSchemaHash, "properties",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("Snapshot() leaked MCP content or raw binding %q: %s", forbidden, serialized)
		}
	}
	if !strings.Contains(string(before.Metadata), `"binding_digest":"sha256:`) ||
		!strings.Contains(string(before.Metadata), `"phase":"before"`) ||
		!strings.Contains(string(after.Metadata), `"phase":"after"`) {
		t.Fatalf("Snapshot() metadata is incomplete: before=%s after=%s", before.Metadata, after.Metadata)
	}

	catalog.bindings[0].SnapshotVersion++
	changed, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseAfter,
	})
	if err != nil {
		t.Fatalf("Snapshot(changed binding) error = %v", err)
	}
	if changed.Digest == before.Digest {
		t.Fatal("Snapshot() did not detect MCP snapshot version change")
	}
}

func TestExternalMCPEnvironmentFailsClosedForInvalidBindings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ExternalMCPToolBinding)
	}{
		{name: "user owner", mutate: func(binding *ExternalMCPToolBinding) { binding.ConnectionOwnerID = 99 }},
		{name: "connection revision", mutate: func(binding *ExternalMCPToolBinding) { binding.ConnectionRevision = 0 }},
		{name: "scope", mutate: func(binding *ExternalMCPToolBinding) { binding.ConnectionScope = "global" }},
		{name: "snapshot version", mutate: func(binding *ExternalMCPToolBinding) { binding.SnapshotVersion = 0 }},
		{name: "schema hash", mutate: func(binding *ExternalMCPToolBinding) { binding.SchemaHash = "invalid" }},
		{name: "policy snapshot", mutate: func(binding *ExternalMCPToolBinding) { binding.PolicySnapshotID = "other" }},
		{name: "disabled policy", mutate: func(binding *ExternalMCPToolBinding) { binding.PolicyEnabled = false }},
		{name: "qualified name", mutate: func(binding *ExternalMCPToolBinding) { binding.PolicyQualifiedName = "other.lookup" }},
		{name: "approval", mutate: func(binding *ExternalMCPToolBinding) { binding.Tool.RequiresApproval = true }},
		{name: "policy category", mutate: func(binding *ExternalMCPToolBinding) { binding.PolicyCategory = "unknown" }},
		{name: "category", mutate: func(binding *ExternalMCPToolBinding) { binding.Tool.Category = "unknown" }},
		{name: "schema", mutate: func(binding *ExternalMCPToolBinding) { binding.Tool.InputSchema = json.RawMessage(`{"type":`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding := externalMCPBinding("crm.lookup", "lookup", agentRuntime.ToolCategoryRead, false)
			test.mutate(&binding)
			environment, err := NewExternalMCPEnvironment(
				&staticExternalMCPToolCatalog{bindings: []ExternalMCPToolBinding{binding}},
				42,
			)
			if err != nil {
				t.Fatalf("NewExternalMCPEnvironment() error = %v", err)
			}
			if _, err = environment.Tools(context.Background(), externalMCPTask(binding.Tool.Name)); err == nil {
				t.Fatal("Tools() error = nil, want invalid binding rejection")
			}
		})
	}
}

func TestExternalMCPEnvironmentRejectsDuplicatesOversizeCanceledAndInvalidConstruction(t *testing.T) {
	if _, err := NewExternalMCPEnvironment(nil, 42); err == nil {
		t.Fatal("NewExternalMCPEnvironment(nil catalog) error = nil")
	}
	if _, err := NewExternalMCPEnvironment(&staticExternalMCPToolCatalog{}, 0); err == nil {
		t.Fatal("NewExternalMCPEnvironment(zero user) error = nil")
	}
	if _, err := NewExternalMCPEnvironment(&staticExternalMCPToolCatalog{}, 42, WithExternalMCPClock(nil)); err == nil {
		t.Fatal("NewExternalMCPEnvironment(nil clock) error = nil")
	}

	binding := externalMCPBinding("crm.lookup", "lookup", agentRuntime.ToolCategoryRead, false)
	catalog := &staticExternalMCPToolCatalog{bindings: []ExternalMCPToolBinding{binding, binding}}
	environment, err := NewExternalMCPEnvironment(catalog, 42)
	if err != nil {
		t.Fatalf("NewExternalMCPEnvironment() error = %v", err)
	}
	if _, err = environment.Tools(context.Background(), externalMCPTask(binding.Tool.Name)); err == nil ||
		!strings.Contains(err.Error(), "duplicate external MCP catalog tool") {
		t.Fatalf("Tools(duplicate) error = %v", err)
	}

	oversize := make([]ExternalMCPToolBinding, 0, maxExternalMCPTools+1)
	allowed := make([]string, 0, maxExternalMCPTools+1)
	for index := 0; index <= maxExternalMCPTools; index++ {
		server := fmt.Sprintf("server%d", index)
		candidate := externalMCPBinding(server+".lookup", "lookup", agentRuntime.ToolCategoryRead, false)
		candidate.ServerID = server
		candidate.ConnectionID = fmt.Sprintf("mcpconn_%032x", index+1)
		candidate.SnapshotID = fmt.Sprintf("mcpsnap_%032x", index+1)
		candidate.PolicySnapshotID = candidate.SnapshotID
		oversize = append(oversize, candidate)
		allowed = append(allowed, candidate.Tool.Name)
	}
	overEnvironment, err := NewExternalMCPEnvironment(&staticExternalMCPToolCatalog{bindings: oversize}, 42)
	if err != nil {
		t.Fatalf("NewExternalMCPEnvironment(oversize catalog) error = %v", err)
	}
	if _, err = overEnvironment.Tools(context.Background(), externalMCPTask(allowed...)); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Tools(oversize) error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	beforeCalls := catalog.calls
	if _, err = environment.Tools(canceled, externalMCPTask(binding.Tool.Name)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Tools(canceled) error = %v", err)
	}
	if catalog.calls != beforeCalls {
		t.Fatal("Tools(canceled) called catalog")
	}
	if _, err = environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: externalMCPTask(binding.Tool.Name), Phase: agentRuntime.SnapshotPhaseBefore,
		Scope: []string{"mcp:connection"},
	}); err == nil || !strings.Contains(err.Error(), "does not support resource scope") {
		t.Fatalf("Snapshot(scope) error = %v", err)
	}
}

func externalMCPBinding(
	qualifiedName string,
	toolName string,
	category agentRuntime.ToolCategory,
	requiresApproval bool,
) ExternalMCPToolBinding {
	serverID := strings.SplitN(qualifiedName, ".", 2)[0]
	return ExternalMCPToolBinding{
		Tool: agentRuntime.ToolDefinition{
			Name: qualifiedName, Description: "remote tool",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Category:    category, RequiresApproval: requiresApproval,
		},
		ConnectionID: externalMCPConnectionID, ConnectionOwnerID: 42,
		ConnectionScope: externalMCPScopeUser, ConnectionRevision: 4, ServerID: serverID,
		SnapshotID: externalMCPSnapshotID, SnapshotVersion: 3, SchemaHash: externalMCPSchemaHash,
		PolicySnapshotID: externalMCPSnapshotID, PolicyToolName: toolName,
		PolicyCategory:      string(category),
		PolicyQualifiedName: qualifiedName, PolicyEnabled: true,
	}
}

func externalMCPTask(tools ...string) agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "task-external-mcp", Goal: "Use only the selected governed MCP tools.", AllowedTools: tools,
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "mcp-result", Description: "The governed remote tool returned evidence.", Required: true,
		}},
	}
}
