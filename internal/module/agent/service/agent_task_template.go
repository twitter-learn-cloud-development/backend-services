package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"twitter-clone/internal/module/agent/repository"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	AgentTaskTemplateInputPlaceholder = "{{input}}"

	MaxAgentTaskTemplateInputBytes    = 12 * 1024
	MaxAgentTaskTemplateRenderedBytes = 20 * 1024
	defaultAgentTaskTemplateListLimit = 20
)

var (
	ErrAgentTaskTemplatesDisabled        = errors.New("agent task templates are disabled")
	ErrAgentTaskTemplateStoreUnavailable = errors.New("agent task template store is unavailable")
	ErrInvalidAgentTaskTemplate          = errors.New("invalid agent task template")
	ErrAgentTaskTemplateSourceIncomplete = errors.New("agent task template source run is not completed")
	ErrAgentTaskTemplateRouteDrift       = errors.New("agent task template execution route changed")
	ErrAgentTaskTemplateIdempotency      = errors.New("agent task template idempotency conflict")
)

type CreateAgentTaskTemplateRequest struct {
	UserID                    uint64
	SourceRunID               string
	ExpectedSourceRunRevision int64
	Name                      string
	Description               string
	InstructionTemplate       string
	IdempotencyKey            string
}

type RunAgentTaskTemplateRequest struct {
	UserID                    uint64
	TemplateID                string
	ExpectedTemplateRevision  int64
	DialogueID                uint64
	DialogueKey               string
	Input                     string
	WebSearchProviderConfigID string
}

// AgentTaskTemplateView excludes the idempotency key and exposes only reusable
// configuration plus immutable source-run evidence.
type AgentTaskTemplateView struct {
	TemplateID             string
	ContractVersion        string
	Name                   string
	Description            string
	InstructionTemplate    string
	Status                 string
	Revision               int64
	SourceRunID            string
	SourceRunRevision      int64
	SourceResultDigest     string
	SourceExecutionProfile string
	CapabilityIDs          []string
	SkillID                string
	SkillVersion           string
	SourceModel            string
	AgentProfileID         string
	AgentProfileVersion    string
	PromptTemplateID       string
	PromptTemplateVersion  string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	ArchivedAt             time.Time
}

func (s *AgentService) AgentTaskTemplatesEnabled() bool {
	return s != nil && s.agentTaskTemplatesEnabled
}

func (s *AgentService) CreateAgentTaskTemplate(
	ctx context.Context,
	request CreateAgentTaskTemplateRequest,
) (*AgentTaskTemplateView, error) {
	if s == nil || !s.agentTaskTemplatesEnabled {
		return nil, ErrAgentTaskTemplatesDisabled
	}
	if !s.recoverableAgentRuns || s.agentExecutionRunStore == nil {
		return nil, ErrAgentExecutionRunStoreUnavailable
	}
	if s.agentTaskTemplateStore == nil {
		return nil, ErrAgentTaskTemplateStoreUnavailable
	}
	request.SourceRunID = strings.TrimSpace(request.SourceRunID)
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	request.InstructionTemplate = strings.TrimSpace(request.InstructionTemplate)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if err := validateCreateAgentTaskTemplateRequest(request); err != nil {
		return nil, err
	}

	source, err := s.agentExecutionRunStore.GetAgentExecutionRun(
		ctx,
		request.SourceRunID,
		request.UserID,
	)
	if err != nil {
		return nil, err
	}
	if source.Revision != request.ExpectedSourceRunRevision {
		return nil, repository.ErrAgentExecutionRunConflict
	}
	if source.Status != repository.AgentExecutionRunCompleted ||
		strings.TrimSpace(source.ResultDigest) == "" {
		return nil, ErrAgentTaskTemplateSourceIncomplete
	}
	if err := s.validateAgentTaskTemplateSourceRoute(ctx, source); err != nil {
		return nil, err
	}

	candidate := &repository.AgentTaskTemplate{
		ContractVersion:        repository.AgentTaskTemplateContractVersion,
		UserID:                 request.UserID,
		Name:                   request.Name,
		Description:            request.Description,
		InstructionTemplate:    request.InstructionTemplate,
		Status:                 repository.AgentTaskTemplateActive,
		Revision:               1,
		IdempotencyKey:         request.IdempotencyKey,
		SourceRunID:            source.ID,
		SourceRunRevision:      source.Revision,
		SourceResultDigest:     source.ResultDigest,
		SourceExecutionProfile: source.ExecutionProfile,
		CapabilityIDs:          append([]string(nil), source.CapabilityIDs...),
		SkillID:                source.SkillID,
		SkillVersion:           source.SkillVersion,
		SourceModel:            source.Model,
		AgentProfileID:         source.AgentProfileID,
		AgentProfileVersion:    source.AgentProfileVersion,
		PromptTemplateID:       source.PromptTemplateID,
		PromptTemplateVersion:  source.PromptTemplateVersion,
	}

	existing, err := s.agentTaskTemplateStore.GetAgentTaskTemplateByIdempotencyKey(
		ctx,
		request.UserID,
		request.IdempotencyKey,
	)
	switch {
	case err == nil:
		if !equivalentAgentTaskTemplate(existing, candidate) {
			return nil, ErrAgentTaskTemplateIdempotency
		}
		return agentTaskTemplateView(existing), nil
	case !errors.Is(err, repository.ErrAgentTaskTemplateNotFound):
		return nil, err
	}

	if err := s.agentTaskTemplateStore.CreateAgentTaskTemplate(ctx, candidate); err != nil {
		if !errors.Is(err, repository.ErrAgentTaskTemplateConflict) {
			return nil, err
		}
		existing, getErr := s.agentTaskTemplateStore.GetAgentTaskTemplateByIdempotencyKey(
			ctx,
			request.UserID,
			request.IdempotencyKey,
		)
		if getErr != nil {
			return nil, errors.Join(err, getErr)
		}
		if !equivalentAgentTaskTemplate(existing, candidate) {
			return nil, ErrAgentTaskTemplateIdempotency
		}
		return agentTaskTemplateView(existing), nil
	}
	return agentTaskTemplateView(candidate), nil
}

func (s *AgentService) ListAgentTaskTemplates(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]*AgentTaskTemplateView, error) {
	if s == nil || s.agentTaskTemplateStore == nil {
		return nil, ErrAgentTaskTemplateStoreUnavailable
	}
	if userID == 0 {
		return nil, fmt.Errorf("%w: user_id is required", ErrInvalidAgentTaskTemplate)
	}
	if limit < 1 || limit > 100 {
		limit = s.agentTaskTemplateListLimit
		if limit < 1 {
			limit = defaultAgentTaskTemplateListLimit
		}
	}
	templates, err := s.agentTaskTemplateStore.ListActiveAgentTaskTemplates(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	views := make([]*AgentTaskTemplateView, 0, len(templates))
	for _, template := range templates {
		views = append(views, agentTaskTemplateView(template))
	}
	return views, nil
}

func (s *AgentService) ArchiveAgentTaskTemplate(
	ctx context.Context,
	userID uint64,
	templateID string,
	expectedRevision int64,
) (*AgentTaskTemplateView, error) {
	if s == nil || s.agentTaskTemplateStore == nil {
		return nil, ErrAgentTaskTemplateStoreUnavailable
	}
	templateOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(templateID))
	if err != nil || userID == 0 || expectedRevision <= 0 {
		return nil, fmt.Errorf("%w: template identity is invalid", ErrInvalidAgentTaskTemplate)
	}
	template, err := s.agentTaskTemplateStore.ArchiveAgentTaskTemplate(
		ctx,
		templateOID,
		userID,
		expectedRevision,
		time.Now(),
	)
	if err != nil {
		return nil, err
	}
	return agentTaskTemplateView(template), nil
}

func (s *AgentService) RunAgentTaskTemplate(
	ctx context.Context,
	request RunAgentTaskTemplateRequest,
) (*UnifiedAgentResult, error) {
	if s == nil || !s.agentTaskTemplatesEnabled {
		return nil, ErrAgentTaskTemplatesDisabled
	}
	if s.agentTaskTemplateStore == nil {
		return nil, ErrAgentTaskTemplateStoreUnavailable
	}
	if !s.recoverableAgentRuns || s.agentExecutionRunStore == nil {
		return nil, ErrAgentExecutionRunStoreUnavailable
	}
	templateOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(request.TemplateID))
	request.Input = strings.TrimSpace(request.Input)
	if err != nil || request.UserID == 0 || request.ExpectedTemplateRevision <= 0 ||
		request.Input == "" || len([]byte(request.Input)) > MaxAgentTaskTemplateInputBytes {
		return nil, fmt.Errorf("%w: template run request is invalid", ErrInvalidAgentTaskTemplate)
	}
	template, err := s.agentTaskTemplateStore.GetAgentTaskTemplate(
		ctx,
		templateOID,
		request.UserID,
	)
	if err != nil {
		return nil, err
	}
	if template.Status != repository.AgentTaskTemplateActive ||
		template.Revision != request.ExpectedTemplateRevision {
		return nil, repository.ErrAgentTaskTemplateConflict
	}
	source, err := s.agentExecutionRunStore.GetAgentExecutionRun(
		ctx,
		template.SourceRunID,
		request.UserID,
	)
	if err != nil {
		return nil, err
	}
	if source.Status != repository.AgentExecutionRunCompleted ||
		source.Revision != template.SourceRunRevision ||
		source.ResultDigest != template.SourceResultDigest ||
		source.ExecutionProfile != template.SourceExecutionProfile ||
		!sameCapabilityIDs(source.CapabilityIDs, template.CapabilityIDs) ||
		source.SkillID != template.SkillID ||
		source.SkillVersion != template.SkillVersion ||
		source.Model != template.SourceModel ||
		source.AgentProfileID != template.AgentProfileID ||
		source.AgentProfileVersion != template.AgentProfileVersion ||
		source.PromptTemplateID != template.PromptTemplateID ||
		source.PromptTemplateVersion != template.PromptTemplateVersion {
		return nil, ErrAgentTaskTemplateSourceIncomplete
	}
	content, err := renderAgentTaskTemplate(template.InstructionTemplate, request.Input)
	if err != nil {
		return nil, err
	}
	return s.RunAgent(ctx, UnifiedAgentRequest{
		UserID:                    request.UserID,
		DialogueID:                request.DialogueID,
		DialogueKey:               request.DialogueKey,
		Content:                   content,
		PreferredCapabilityIDs:    append([]string(nil), template.CapabilityIDs...),
		WebSearchProviderConfigID: request.WebSearchProviderConfigID,
		SkillID:                   template.SkillID,
		SkillVersion:              template.SkillVersion,
		TaskTemplateID:            template.ID.Hex(),
		TaskTemplateRevision:      template.Revision,
		ExpectedExecutionProfile:  template.SourceExecutionProfile,
	})
}

func (s *AgentService) validateAgentTaskTemplateSourceRoute(
	ctx context.Context,
	source *repository.AgentExecutionRun,
) error {
	if source == nil || strings.TrimSpace(source.ExecutionProfile) == "" ||
		len(uniqueCapabilityIDs(source.CapabilityIDs)) == 0 {
		return ErrAgentTaskTemplateSourceIncomplete
	}
	if source.SkillID != "" {
		if source.SkillVersion == "" ||
			len(source.CapabilityIDs) != 1 ||
			source.CapabilityIDs[0] != CapabilitySkillRun {
			return ErrAgentTaskTemplateSourceIncomplete
		}
		if _, err := s.resolveWorkflowSkill(
			ctx,
			source.UserID,
			source.SkillID,
			source.SkillVersion,
		); err != nil {
			return err
		}
	}
	plan, err := s.capabilityPlanner.Plan(ctx, AgentCapabilityPlanRequest{
		Query:                  AgentTaskTemplateInputPlaceholder,
		PreferredCapabilityIDs: source.CapabilityIDs,
	})
	if err != nil {
		return err
	}
	if plan.ExecutionProfile != source.ExecutionProfile ||
		!sameCapabilityIDs(plan.CapabilityIDs, source.CapabilityIDs) {
		return ErrAgentTaskTemplateRouteDrift
	}
	return nil
}

func validateCreateAgentTaskTemplateRequest(request CreateAgentTaskTemplateRequest) error {
	switch {
	case request.UserID == 0 || request.SourceRunID == "" ||
		request.ExpectedSourceRunRevision <= 0:
		return fmt.Errorf("%w: source run identity is required", ErrInvalidAgentTaskTemplate)
	case request.Name == "" ||
		utf8.RuneCountInString(request.Name) > repository.MaxAgentTaskTemplateNameRunes:
		return fmt.Errorf("%w: name is invalid", ErrInvalidAgentTaskTemplate)
	case utf8.RuneCountInString(request.Description) >
		repository.MaxAgentTaskTemplateDescriptionRunes:
		return fmt.Errorf("%w: description is too long", ErrInvalidAgentTaskTemplate)
	case request.IdempotencyKey == "" ||
		len([]byte(request.IdempotencyKey)) > repository.MaxAgentTaskTemplateIdempotencyBytes:
		return fmt.Errorf("%w: idempotency_key is invalid", ErrInvalidAgentTaskTemplate)
	}
	if _, err := renderAgentTaskTemplate(request.InstructionTemplate, "validation"); err != nil {
		return err
	}
	return nil
}

func renderAgentTaskTemplate(instruction, input string) (string, error) {
	instruction = strings.TrimSpace(instruction)
	input = strings.TrimSpace(input)
	if instruction == "" ||
		len([]byte(instruction)) > repository.MaxAgentTaskTemplateInstructionBytes ||
		strings.ContainsRune(instruction, '\x00') {
		return "", fmt.Errorf("%w: instruction_template is invalid", ErrInvalidAgentTaskTemplate)
	}
	if strings.Count(instruction, AgentTaskTemplateInputPlaceholder) != 1 {
		return "", fmt.Errorf(
			"%w: instruction_template requires exactly one %s placeholder",
			ErrInvalidAgentTaskTemplate,
			AgentTaskTemplateInputPlaceholder,
		)
	}
	remaining := strings.Replace(instruction, AgentTaskTemplateInputPlaceholder, "", 1)
	if strings.Contains(remaining, "{{") || strings.Contains(remaining, "}}") {
		return "", fmt.Errorf("%w: unsupported instruction placeholder", ErrInvalidAgentTaskTemplate)
	}
	if input == "" || strings.ContainsRune(input, '\x00') ||
		len([]byte(input)) > MaxAgentTaskTemplateInputBytes {
		return "", fmt.Errorf("%w: template input is invalid", ErrInvalidAgentTaskTemplate)
	}
	rendered := strings.Replace(instruction, AgentTaskTemplateInputPlaceholder, input, 1)
	if len([]byte(rendered)) > MaxAgentTaskTemplateRenderedBytes {
		return "", fmt.Errorf("%w: rendered instruction exceeds the size limit", ErrInvalidAgentTaskTemplate)
	}
	return rendered, nil
}

func equivalentAgentTaskTemplate(
	existing *repository.AgentTaskTemplate,
	candidate *repository.AgentTaskTemplate,
) bool {
	if existing == nil || candidate == nil || existing.Status != repository.AgentTaskTemplateActive {
		return false
	}
	return existing.ContractVersion == candidate.ContractVersion &&
		existing.UserID == candidate.UserID &&
		existing.Name == candidate.Name &&
		existing.Description == candidate.Description &&
		existing.InstructionTemplate == candidate.InstructionTemplate &&
		existing.SourceRunID == candidate.SourceRunID &&
		existing.SourceRunRevision == candidate.SourceRunRevision &&
		existing.SourceResultDigest == candidate.SourceResultDigest &&
		existing.SourceExecutionProfile == candidate.SourceExecutionProfile &&
		sameCapabilityIDs(existing.CapabilityIDs, candidate.CapabilityIDs) &&
		existing.SkillID == candidate.SkillID &&
		existing.SkillVersion == candidate.SkillVersion &&
		existing.SourceModel == candidate.SourceModel &&
		existing.AgentProfileID == candidate.AgentProfileID &&
		existing.AgentProfileVersion == candidate.AgentProfileVersion &&
		existing.PromptTemplateID == candidate.PromptTemplateID &&
		existing.PromptTemplateVersion == candidate.PromptTemplateVersion
}

func agentTaskTemplateView(template *repository.AgentTaskTemplate) *AgentTaskTemplateView {
	if template == nil {
		return nil
	}
	return &AgentTaskTemplateView{
		TemplateID: template.ID.Hex(), ContractVersion: template.ContractVersion,
		Name: template.Name, Description: template.Description,
		InstructionTemplate: template.InstructionTemplate, Status: template.Status,
		Revision: template.Revision, SourceRunID: template.SourceRunID,
		SourceRunRevision:      template.SourceRunRevision,
		SourceResultDigest:     template.SourceResultDigest,
		SourceExecutionProfile: template.SourceExecutionProfile,
		CapabilityIDs:          append([]string(nil), template.CapabilityIDs...),
		SkillID:                template.SkillID, SkillVersion: template.SkillVersion,
		SourceModel: template.SourceModel, AgentProfileID: template.AgentProfileID,
		AgentProfileVersion:   template.AgentProfileVersion,
		PromptTemplateID:      template.PromptTemplateID,
		PromptTemplateVersion: template.PromptTemplateVersion,
		CreatedAt:             template.CreatedAt, UpdatedAt: template.UpdatedAt,
		ArchivedAt: template.ArchivedAt,
	}
}

func sameCapabilityIDs(left, right []string) bool {
	left = uniqueCapabilityIDs(left)
	right = uniqueCapabilityIDs(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
