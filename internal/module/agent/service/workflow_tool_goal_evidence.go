package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	"twitter-clone/internal/module/agent/repository"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type workflowToolRunEvidenceStore interface {
	GetWorkflowRun(
		ctx context.Context,
		runID primitive.ObjectID,
		userID uint64,
	) (*repository.WorkflowRunRecord, error)
}

type workflowToolRunEvidenceResolver struct {
	store workflowToolRunEvidenceStore
}

func (resolver workflowToolRunEvidenceResolver) ResolveWorkflowToolRunEvidence(
	ctx context.Context,
	userID uint64,
	workflowRunID string,
) (agentEvidence.WorkflowToolRunEvidence, error) {
	if resolver.store == nil || userID == 0 {
		return agentEvidence.WorkflowToolRunEvidence{}, fmt.Errorf("workflow tool run evidence store is unavailable")
	}
	runID, err := primitive.ObjectIDFromHex(strings.TrimSpace(workflowRunID))
	if err != nil {
		return agentEvidence.WorkflowToolRunEvidence{}, fmt.Errorf("workflow tool child run identity is invalid")
	}
	run, err := resolver.store.GetWorkflowRun(ctx, runID, userID)
	if err != nil {
		return agentEvidence.WorkflowToolRunEvidence{}, err
	}
	if run == nil || run.ID != runID || run.UserID != userID {
		return agentEvidence.WorkflowToolRunEvidence{}, fmt.Errorf("workflow tool child run ownership is invalid")
	}
	outputDigest := sha256.Sum256([]byte(strings.TrimSpace(run.OutputJSON)))
	return agentEvidence.WorkflowToolRunEvidence{
		WorkflowRunID: run.ID.Hex(), WorkflowID: run.WorkflowID.Hex(),
		WorkflowRevisionID:     run.WorkflowRevisionID.Hex(),
		WorkflowRevisionNumber: run.WorkflowRevisionNumber,
		InvocationSource:       run.InvocationSource,
		ParentRunID:            run.ParentRunID, ParentActionID: run.ParentActionID,
		Status:          run.Status,
		RunOutputDigest: "sha256:" + hex.EncodeToString(outputDigest[:]),
		FinishedAt:      run.FinishedAt,
	}, nil
}

var _ agentEvidence.WorkflowToolRunEvidenceResolver = workflowToolRunEvidenceResolver{}
