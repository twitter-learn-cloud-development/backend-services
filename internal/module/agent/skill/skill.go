package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	ContractVersionV1 = "skill.v1"
	SourceWorkflow    = "workflow"

	maxSkillIDBytes           = 128
	maxSkillVersionBytes      = 96
	maxSkillDisplayNameRunes  = 120
	maxSkillDescriptionRunes  = 512
	maxSkillInstructionsRunes = 4_000
	maxSkillSchemaBytes       = 16 << 10
	maxSkillTools             = 64
	maxSkillKnowledgeBindings = 32
	maxSkillTimeout           = 10 * time.Minute
)

var (
	ErrCatalogDisabled = errors.New("agent skill catalog is disabled")
	ErrSkillNotFound   = errors.New("agent skill not found")
	ErrVersionNotFound = errors.New("agent skill version not found")

	skillIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// Catalog exposes immutable, tenant-scoped Skill versions. Implementations
// may project versions from another authoritative control plane, but must not
// grant tools beyond the current policy intersection.
type Catalog interface {
	List(context.Context, uint64, int) ([]Version, error)
	Resolve(context.Context, uint64, string, string) (Version, error)
}

type ProfileBinding struct {
	ID            string
	Version       string
	PromptID      string
	PromptVersion string
}

type KnowledgeBinding struct {
	Kind      string
	Reference string
	Version   string
}

type OutputContract struct {
	SchemaID    string
	ContentType string
	SchemaJSON  json.RawMessage
}

type WorkflowBinding struct {
	PublicationID          string
	PublicationRevision    int64
	WorkflowID             string
	WorkflowRevisionID     string
	WorkflowRevisionNumber int64
	WorkflowDSLHash        string
	ToolName               string
	InputSchemaJSON        json.RawMessage
}

// Version is the immutable, executable Skill contract. Instructions can only
// narrow behavior; AllowedTools, Profile, Budget and the source binding remain
// authoritative enforcement metadata.
type Version struct {
	ContractVersion string
	ID              string
	Version         string
	DisplayName     string
	Description     string
	Instructions    string
	Source          string
	AllowedTools    []string
	Knowledge       []KnowledgeBinding
	Profile         ProfileBinding
	Budget          agentRuntime.Budget
	Output          OutputContract
	Workflow        *WorkflowBinding
}

func ValidateVersion(candidate Version) error {
	if candidate.ContractVersion != ContractVersionV1 {
		return fmt.Errorf("unsupported skill contract version %q", candidate.ContractVersion)
	}
	if err := validateIdentifier("skill id", candidate.ID, maxSkillIDBytes); err != nil {
		return err
	}
	if err := validateIdentifier("skill version", candidate.Version, maxSkillVersionBytes); err != nil {
		return err
	}
	if err := validateBoundedText("skill display name", candidate.DisplayName, maxSkillDisplayNameRunes); err != nil {
		return err
	}
	if err := validateBoundedText("skill description", candidate.Description, maxSkillDescriptionRunes); err != nil {
		return err
	}
	if err := validateBoundedText("skill instructions", candidate.Instructions, maxSkillInstructionsRunes); err != nil {
		return err
	}
	if strings.TrimSpace(candidate.Source) == "" {
		return errors.New("skill source is required")
	}
	if err := validateAllowedTools(candidate.AllowedTools); err != nil {
		return err
	}
	if err := validateProfileBinding(candidate.Profile); err != nil {
		return err
	}
	if err := validateKnowledgeBindings(candidate.Knowledge); err != nil {
		return err
	}
	if err := validateBudget(candidate.Budget); err != nil {
		return err
	}
	if err := ValidateOutputContract(candidate.Output); err != nil {
		return err
	}
	if candidate.Source == SourceWorkflow {
		if err := validateWorkflowBinding(candidate.Workflow, candidate.AllowedTools); err != nil {
			return err
		}
	}
	return nil
}

func ValidateOutput(contract OutputContract, raw json.RawMessage) error {
	compiled, err := compileOutputContract(contract)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return errors.New("skill output is empty")
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decode skill output: %w", err)
	}
	if err := compiled.Validate(value); err != nil {
		return fmt.Errorf("skill output contract validation failed: %w", err)
	}
	return nil
}

func ValidateOutputContract(contract OutputContract) error {
	_, err := compileOutputContract(contract)
	return err
}

func CloneVersion(source Version) Version {
	clone := source
	clone.AllowedTools = append([]string(nil), source.AllowedTools...)
	clone.Knowledge = append([]KnowledgeBinding(nil), source.Knowledge...)
	clone.Output.SchemaJSON = append(json.RawMessage(nil), source.Output.SchemaJSON...)
	if source.Workflow != nil {
		binding := *source.Workflow
		binding.InputSchemaJSON = append(json.RawMessage(nil), source.Workflow.InputSchemaJSON...)
		clone.Workflow = &binding
	}
	return clone
}

func compileOutputContract(contract OutputContract) (*jsonschema.Schema, error) {
	if strings.TrimSpace(contract.SchemaID) == "" {
		return nil, errors.New("skill output schema_id is required")
	}
	if strings.TrimSpace(contract.ContentType) == "" {
		return nil, errors.New("skill output content_type is required")
	}
	if len(contract.SchemaJSON) == 0 || len(contract.SchemaJSON) > maxSkillSchemaBytes {
		return nil, fmt.Errorf("skill output schema must contain 1..%d bytes", maxSkillSchemaBytes)
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(contract.SchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("decode skill output schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	location := "mem://agent-skill/" + strings.TrimSpace(contract.SchemaID) + ".json"
	if err := compiler.AddResource(location, value); err != nil {
		return nil, fmt.Errorf("add skill output schema resource: %w", err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("compile skill output schema: %w", err)
	}
	return compiled, nil
}

func validateAllowedTools(tools []string) error {
	if len(tools) == 0 || len(tools) > maxSkillTools {
		return fmt.Errorf("skill allowed_tools must contain 1..%d entries", maxSkillTools)
	}
	seen := make(map[string]struct{}, len(tools))
	for _, toolName := range tools {
		toolName = strings.TrimSpace(toolName)
		if err := validateIdentifier("skill allowed tool", toolName, maxSkillIDBytes); err != nil {
			return err
		}
		if _, exists := seen[toolName]; exists {
			return fmt.Errorf("duplicate skill allowed tool %q", toolName)
		}
		seen[toolName] = struct{}{}
	}
	return nil
}

func validateProfileBinding(binding ProfileBinding) error {
	if err := validateIdentifier("skill profile id", binding.ID, maxSkillIDBytes); err != nil {
		return err
	}
	if err := validateIdentifier("skill profile version", binding.Version, maxSkillVersionBytes); err != nil {
		return err
	}
	if err := validateIdentifier("skill prompt id", binding.PromptID, maxSkillIDBytes); err != nil {
		return err
	}
	return validateIdentifier("skill prompt version", binding.PromptVersion, maxSkillVersionBytes)
}

func validateKnowledgeBindings(bindings []KnowledgeBinding) error {
	if len(bindings) > maxSkillKnowledgeBindings {
		return fmt.Errorf("skill knowledge bindings exceed %d entries", maxSkillKnowledgeBindings)
	}
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		kind := strings.TrimSpace(binding.Kind)
		reference := strings.TrimSpace(binding.Reference)
		version := strings.TrimSpace(binding.Version)
		if kind == "" || reference == "" {
			return errors.New("skill knowledge kind and reference are required")
		}
		key := kind + "\x00" + reference + "\x00" + version
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate skill knowledge binding %q", reference)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateBudget(budget agentRuntime.Budget) error {
	if budget.MaxSteps < 1 || budget.MaxSteps > agentRuntime.MaxAllowedSteps {
		return fmt.Errorf("skill max_steps must be within 1..%d", agentRuntime.MaxAllowedSteps)
	}
	if budget.MaxInputTokens < 1 || budget.MaxOutputTokens < 1 || budget.MaxTotalTokens < 1 {
		return errors.New("skill token budgets must be positive")
	}
	if budget.MaxTotalTokens < budget.MaxInputTokens ||
		budget.MaxTotalTokens < budget.MaxOutputTokens {
		return errors.New("skill max_total_tokens cannot be smaller than per-call token limits")
	}
	if budget.MaxEstimatedCostMicros < 0 {
		return errors.New("skill cost budget cannot be negative")
	}
	if budget.Timeout <= 0 || budget.Timeout > maxSkillTimeout {
		return fmt.Errorf("skill timeout must be within 1ns..%s", maxSkillTimeout)
	}
	if !budget.Deadline.IsZero() {
		return errors.New("skill version cannot contain a request-specific deadline")
	}
	return nil
}

func validateWorkflowBinding(binding *WorkflowBinding, allowedTools []string) error {
	if binding == nil {
		return errors.New("workflow skill binding is required")
	}
	if strings.TrimSpace(binding.PublicationID) == "" ||
		binding.PublicationRevision < 1 ||
		strings.TrimSpace(binding.WorkflowID) == "" ||
		strings.TrimSpace(binding.WorkflowRevisionID) == "" ||
		binding.WorkflowRevisionNumber < 1 ||
		strings.TrimSpace(binding.WorkflowDSLHash) == "" ||
		strings.TrimSpace(binding.ToolName) == "" ||
		len(binding.InputSchemaJSON) == 0 {
		return errors.New("workflow skill binding is incomplete")
	}
	if len(allowedTools) != 1 || strings.TrimSpace(allowedTools[0]) != strings.TrimSpace(binding.ToolName) {
		return errors.New("workflow skill must allow exactly its bound workflow tool")
	}
	return nil
}

func validateIdentifier(label, value string, maxBytes int) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxBytes || !skillIdentifierPattern.MatchString(value) {
		return fmt.Errorf("%s must match %s and contain at most %d bytes", label, skillIdentifierPattern, maxBytes)
	}
	return nil
}

func validateBoundedText(label, value string, maxRunes int) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s exceeds %d characters", label, maxRunes)
	}
	return nil
}
