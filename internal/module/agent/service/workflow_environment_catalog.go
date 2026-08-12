package service

import (
	"context"
	"fmt"
	"log/slog"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
)

type workflowEnvironmentCatalog struct {
	service *AgentService
}

// ListWorkflowTools projects only active publications whose ownership,
// immutable revision and current DSL policy still validate. Execution performs
// the same checks again and remains behind the governed Runtime executor.
func (catalog *workflowEnvironmentCatalog) ListWorkflowTools(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]agentEnvironment.WorkflowToolBinding, error) {
	if catalog == nil || catalog.service == nil {
		return nil, fmt.Errorf("workflow environment catalog is not configured")
	}
	service := catalog.service
	if !service.workflowAsToolEnabled {
		return nil, ErrWorkflowAsToolDisabled
	}
	if userID == 0 {
		return nil, fmt.Errorf("workflow environment catalog user is required")
	}
	store, err := service.workflowToolPublications()
	if err != nil {
		return nil, err
	}
	publications, err := store.ListActiveWorkflowToolPublications(
		ctx,
		userID,
		normalizeWorkflowEnvironmentCatalogLimit(service.workflowToolCatalogLimit, limit),
	)
	if err != nil {
		return nil, err
	}

	bindings := make([]agentEnvironment.WorkflowToolBinding, 0, len(publications))
	for _, publication := range publications {
		revision, _, validationErr := service.validateWorkflowToolPublicationBinding(ctx, userID, publication)
		if validationErr != nil {
			slog.WarnContext(
				ctx,
				"published workflow tool excluded from environment catalog",
				"tool", workflowToolPublicationName(publication),
				"error", validationErr,
			)
			continue
		}
		if publication.ID.IsZero() {
			slog.WarnContext(
				ctx,
				"published workflow tool excluded from environment catalog",
				"tool", workflowToolPublicationName(publication),
				"error", "publication identity is missing",
			)
			continue
		}
		bindings = append(bindings, agentEnvironment.WorkflowToolBinding{
			Tool:                   workflowSkillToolDefinition(publication),
			PublicationID:          publication.ID.Hex(),
			PublicationRevision:    publication.Revision,
			WorkflowID:             publication.WorkflowID.Hex(),
			WorkflowRevisionID:     revision.ID.Hex(),
			WorkflowRevisionNumber: revision.RevisionNumber,
			WorkflowDSLHash:        revision.DSLHash,
		})
	}
	return bindings, nil
}

func (s *AgentService) newWorkflowToolEnvironment(
	userID uint64,
) (*agentEnvironment.WorkflowToolEnvironment, error) {
	if s == nil {
		return nil, fmt.Errorf("agent service is not configured")
	}
	limit := normalizeWorkflowEnvironmentCatalogLimit(s.workflowToolCatalogLimit, 0)
	return agentEnvironment.NewWorkflowToolEnvironment(
		&workflowEnvironmentCatalog{service: s},
		userID,
		agentEnvironment.WithWorkflowToolLimit(limit),
	)
}

func normalizeWorkflowEnvironmentCatalogLimit(configured int, requested int) int {
	if configured < 1 || configured > maxAgentSkillCatalogLimit {
		configured = defaultWorkflowToolCatalogLimit
	}
	if requested < 1 || requested > configured {
		return configured
	}
	return requested
}

var _ agentEnvironment.WorkflowToolCatalog = (*workflowEnvironmentCatalog)(nil)
