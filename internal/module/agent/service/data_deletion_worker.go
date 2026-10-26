package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
	"twitter-clone/internal/module/agent/repository"
)

const (
	dataDeletionVerifierAdapterVersion  = "v1"
	dataDeletionFinalizerAdapterVersion = "v1"
)

var destructiveDeletionStages = []struct {
	capability string
	stageID    string
}{
	{agentdeletion.CapabilityRelationshipMutation, agentdeletion.StageDetachRelationships},
	{agentdeletion.CapabilityMinIOSubjectDelete, agentdeletion.StageDeleteMinIO},
	{agentdeletion.CapabilityQdrantSubjectDelete, agentdeletion.StageDeleteQdrant},
	{agentdeletion.CapabilityTweetServiceDelete, agentdeletion.StageDeleteTweets},
	{agentdeletion.CapabilityMongoOwnedDelete, agentdeletion.StageDeleteMongo},
}

type DataDeletionGovernanceIssuer interface {
	IssueGovernanceDecision(context.Context, uint64, time.Time, time.Time) (agentdeletion.GovernanceDecision, error)
}

// DataDeletionSubjectWriteFence must stop every write path capable of
// recreating subject-owned data. No production implementation is registered
// until every participating service honors this fence.
type DataDeletionSubjectWriteFence interface {
	agentdeletion.CapabilityProvider
	EnforceDataDeletionFence(context.Context, agentdeletion.WriteFenceRequest) (agentdeletion.WriteFenceReceipt, error)
}

type DataDeletionAdapterBinding struct {
	Adapter     agentdeletion.DeletionAdapter
	ScopeDigest string
}

type DataDeletionExecutionWorker struct {
	service    *DataDeletionRequestService
	governance DataDeletionGovernanceIssuer
	writeFence DataDeletionSubjectWriteFence
	adapters   map[string]DataDeletionAdapterBinding
	now        func() time.Time
	snapshot   agentdeletion.CapabilitySnapshot
}

func NewDataDeletionExecutionWorker(
	service *DataDeletionRequestService,
	governance DataDeletionGovernanceIssuer,
	writeFence DataDeletionSubjectWriteFence,
	bindings []DataDeletionAdapterBinding,
	now func() time.Time,
) (*DataDeletionExecutionWorker, error) {
	if service == nil || governance == nil || writeFence == nil {
		return nil, errors.New("data deletion worker dependencies are incomplete")
	}
	if now == nil {
		now = time.Now
	}
	if descriptor := writeFence.DeletionCapability(); descriptor.ID != agentdeletion.CapabilitySubjectWriteFence {
		return nil, errors.New("data deletion write fence capability is invalid")
	}
	expected := make(map[string]string, len(destructiveDeletionStages))
	for _, definition := range destructiveDeletionStages {
		expected[definition.capability] = definition.stageID
	}
	adapters := make(map[string]DataDeletionAdapterBinding, len(bindings))
	providers := make([]agentdeletion.CapabilityProvider, 0, len(bindings)+3)
	providers = append(providers, writeFence)
	for _, binding := range bindings {
		if binding.Adapter == nil || !isSHA256Digest(binding.ScopeDigest) {
			return nil, errors.New("data deletion adapter binding is invalid")
		}
		descriptor := binding.Adapter.DeletionCapability()
		if _, ok := expected[descriptor.ID]; !ok {
			return nil, fmt.Errorf("unexpected data deletion adapter capability %q", descriptor.ID)
		}
		if _, duplicate := adapters[descriptor.ID]; duplicate {
			return nil, fmt.Errorf("duplicate data deletion adapter capability %q", descriptor.ID)
		}
		adapters[descriptor.ID] = binding
		providers = append(providers, binding.Adapter)
	}
	if len(adapters) != len(expected) {
		return nil, errors.New("data deletion worker requires every destructive storage adapter")
	}
	providers = append(providers,
		dataDeletionStaticCapability{agentdeletion.CapabilityDescriptor{
			ID: agentdeletion.CapabilityCrossStoreVerify, Adapter: "service.cross-store-verifier",
			Version: dataDeletionVerifierAdapterVersion,
		}},
		dataDeletionStaticCapability{agentdeletion.CapabilityDescriptor{
			ID: agentdeletion.CapabilityRequestFinalize, Adapter: "service.request-finalizer",
			Version: dataDeletionFinalizerAdapterVersion,
		}},
	)
	registry, err := agentdeletion.NewCapabilityRegistry(providers...)
	if err != nil {
		return nil, fmt.Errorf("build data deletion worker capability registry: %w", err)
	}
	worker := &DataDeletionExecutionWorker{
		service: service, governance: governance, writeFence: writeFence,
		adapters: adapters, now: now, snapshot: registry.Snapshot(),
	}
	// Construction is the only place that installs a complete immutable
	// registry. It does not enable an existing or future deletion request.
	service.capabilityRegistry = registry
	return worker, nil
}

func (w *DataDeletionExecutionWorker) CapabilitySnapshot() agentdeletion.CapabilitySnapshot {
	if w == nil {
		return agentdeletion.CapabilitySnapshot{}
	}
	result := w.snapshot
	result.Capabilities = append([]agentdeletion.CapabilityDescriptor(nil), w.snapshot.Capabilities...)
	return result
}

func (w *DataDeletionExecutionWorker) RunRequest(
	ctx context.Context,
	requestID primitive.ObjectID,
) (*repository.DataDeletionRequest, error) {
	if w == nil || requestID.IsZero() {
		return nil, errors.New("data deletion worker request is invalid")
	}
	for stageCount := 0; stageCount < len(w.snapshot.Capabilities); stageCount++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		request, err := w.service.Get(ctx, requestID)
		if err != nil {
			return nil, err
		}
		if request.Status == repository.DataDeletionCompleted {
			return request, nil
		}
		if request.ExecutionProtocol == nil {
			return nil, ErrDataDeletionExecutionUnavailable
		}
		claim, err := w.service.ClaimExecutionStage(
			ctx, requestID, request.Revision, request.ExecutionProtocol.Checkpoint.Revision, w.now().UTC(),
		)
		if err != nil {
			return nil, err
		}
		result, executionErr := w.executeStage(ctx, claim)
		if executionErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failed, completionErr := w.service.CompleteExecutionStage(
				ctx, requestID, claim.Request.Revision, claim.Request.ExecutionProtocol.Checkpoint.Revision,
				claim.LeaseToken, agentdeletion.StageFailed, "", dataDeletionStageFailureCode(claim.Definition.ID),
				w.now().UTC(),
			)
			if completionErr != nil {
				return failed, errors.Join(executionErr, completionErr)
			}
			return failed, executionErr
		}
		completed, err := w.service.CompleteExecutionStage(
			ctx, requestID, claim.Request.Revision, claim.Request.ExecutionProtocol.Checkpoint.Revision,
			claim.LeaseToken, agentdeletion.StageSucceeded, result.receiptDigest, "", result.completedAt,
		)
		if err != nil {
			return completed, err
		}
		if completed.Status == repository.DataDeletionCompleted {
			return completed, nil
		}
	}
	return nil, errors.New("data deletion worker exhausted the bounded stage count")
}

type dataDeletionStageResult struct {
	receiptDigest string
	completedAt   time.Time
}

func (w *DataDeletionExecutionWorker) executeStage(
	ctx context.Context,
	claim *DataDeletionStageClaim,
) (dataDeletionStageResult, error) {
	if claim == nil || claim.Request == nil || claim.Request.ExecutionProtocol == nil {
		return dataDeletionStageResult{}, errors.New("data deletion stage claim is incomplete")
	}
	switch claim.Definition.ID {
	case agentdeletion.StageEnforceWriteFence:
		return w.executeWriteFence(ctx, claim)
	case agentdeletion.StageDetachRelationships, agentdeletion.StageDeleteMinIO,
		agentdeletion.StageDeleteQdrant, agentdeletion.StageDeleteTweets, agentdeletion.StageDeleteMongo:
		return w.executeDestructiveAdapter(ctx, claim)
	case agentdeletion.StageVerify:
		return w.executeCrossStoreVerification(ctx, claim)
	case agentdeletion.StageFinalize:
		completedAt := w.now().UTC()
		receipt, err := agentdeletion.BuildFinalizationReceipt(
			claim.Request.ID.Hex(), claim.Request.UserID, *claim.Request.ExecutionProtocol, w.snapshot, completedAt,
		)
		if err != nil {
			return dataDeletionStageResult{}, err
		}
		return dataDeletionStageResult{receiptDigest: receipt.ReceiptDigest, completedAt: completedAt}, nil
	default:
		return dataDeletionStageResult{}, errors.New("data deletion stage is unsupported")
	}
}

func (w *DataDeletionExecutionWorker) executeWriteFence(
	ctx context.Context,
	claim *DataDeletionStageClaim,
) (dataDeletionStageResult, error) {
	now := w.now().UTC()
	decision, err := w.issueGovernance(ctx, claim.Request.UserID, now, claim.Checkpoint.LeaseUntil)
	if err != nil {
		return dataDeletionStageResult{}, err
	}
	request := agentdeletion.WriteFenceRequest{
		RequestID: claim.Request.ID.Hex(), UserID: claim.Request.UserID,
		SubjectDigest:    agentdeletion.DeletionSubjectDigest(claim.Request.UserID),
		PlanDigest:       claim.Request.ExecutionProtocol.Plan.Digest,
		CapabilityDigest: claim.Request.ExecutionProtocol.Plan.CapabilityDigest,
		Governance:       decision, Deadline: claim.Checkpoint.LeaseUntil,
	}
	if err := agentdeletion.ValidateWriteFenceRequest(request, now); err != nil {
		return dataDeletionStageResult{}, err
	}
	receipt, err := w.writeFence.EnforceDataDeletionFence(ctx, request)
	if err != nil {
		return dataDeletionStageResult{}, err
	}
	completedAt := w.now().UTC()
	if err := agentdeletion.VerifyWriteFenceReceipt(request, receipt, completedAt); err != nil {
		return dataDeletionStageResult{}, err
	}
	return dataDeletionStageResult{receiptDigest: receipt.ReceiptDigest, completedAt: completedAt}, nil
}

func (w *DataDeletionExecutionWorker) executeDestructiveAdapter(
	ctx context.Context,
	claim *DataDeletionStageClaim,
) (dataDeletionStageResult, error) {
	binding, ok := w.adapters[claim.Definition.RequiredCapability]
	if !ok {
		return dataDeletionStageResult{}, errors.New("data deletion stage adapter is unavailable")
	}
	request, err := w.newAdapterRequest(ctx, claim, binding, claim.Definition.ID)
	if err != nil {
		return dataDeletionStageResult{}, err
	}
	result, err := binding.Adapter.Delete(ctx, request)
	if err != nil {
		return dataDeletionStageResult{}, err
	}
	postState, err := binding.Adapter.Observe(ctx, request.ObservationRequest())
	if err != nil {
		return dataDeletionStageResult{}, err
	}
	completedAt := w.now().UTC()
	if err := agentdeletion.VerifyAdapterResult(binding.Adapter, request, result, postState, completedAt); err != nil {
		return dataDeletionStageResult{}, err
	}
	return dataDeletionStageResult{receiptDigest: result.Receipt.ReceiptDigest, completedAt: completedAt}, nil
}

func (w *DataDeletionExecutionWorker) executeCrossStoreVerification(
	ctx context.Context,
	claim *DataDeletionStageClaim,
) (dataDeletionStageResult, error) {
	states := make([]agentdeletion.PostState, 0, len(destructiveDeletionStages))
	for _, definition := range destructiveDeletionStages {
		binding := w.adapters[definition.capability]
		request, err := w.newAdapterRequest(ctx, claim, binding, definition.stageID)
		if err != nil {
			return dataDeletionStageResult{}, err
		}
		state, err := binding.Adapter.Observe(ctx, request.ObservationRequest())
		if err != nil {
			return dataDeletionStageResult{}, err
		}
		states = append(states, state)
	}
	completedAt := w.now().UTC()
	report, err := agentdeletion.BuildCrossStoreVerificationReport(
		claim.Request.ID.Hex(), claim.Request.UserID, claim.Request.ExecutionProtocol.Plan.Digest,
		w.snapshot, states, completedAt,
	)
	if err != nil {
		return dataDeletionStageResult{}, err
	}
	return dataDeletionStageResult{receiptDigest: report.ReportDigest, completedAt: completedAt}, nil
}

func (w *DataDeletionExecutionWorker) newAdapterRequest(
	ctx context.Context,
	claim *DataDeletionStageClaim,
	binding DataDeletionAdapterBinding,
	stageID string,
) (agentdeletion.AdapterRequest, error) {
	now := w.now().UTC()
	decision, err := w.issueGovernance(ctx, claim.Request.UserID, now, claim.Checkpoint.LeaseUntil)
	if err != nil {
		return agentdeletion.AdapterRequest{}, err
	}
	return agentdeletion.NewAdapterRequest(agentdeletion.AdapterRequestInput{
		RequestID: claim.Request.ID.Hex(), StageID: stageID, UserID: claim.Request.UserID,
		PlanDigest:       claim.Request.ExecutionProtocol.Plan.Digest,
		CapabilityDigest: claim.Request.ExecutionProtocol.Plan.CapabilityDigest,
		Capability:       binding.Adapter.DeletionCapability(), ScopeDigest: binding.ScopeDigest,
		Governance: decision, Now: now, Deadline: claim.Checkpoint.LeaseUntil,
	})
}

func (w *DataDeletionExecutionWorker) issueGovernance(
	ctx context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	decision, err := w.governance.IssueGovernanceDecision(ctx, userID, now, validUntil)
	if err != nil {
		return agentdeletion.GovernanceDecision{}, err
	}
	if err := agentdeletion.AuthorizeGovernanceDecision(decision, userID, now); err != nil {
		return agentdeletion.GovernanceDecision{}, err
	}
	return decision, nil
}

type dataDeletionStaticCapability struct {
	descriptor agentdeletion.CapabilityDescriptor
}

func (provider dataDeletionStaticCapability) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return provider.descriptor
}

func dataDeletionStageFailureCode(stageID string) string {
	switch stageID {
	case agentdeletion.StageEnforceWriteFence:
		return "write_fence_failed"
	case agentdeletion.StageDetachRelationships:
		return "relationship_mutation_failed"
	case agentdeletion.StageDeleteMinIO:
		return "minio_delete_failed"
	case agentdeletion.StageDeleteQdrant:
		return "qdrant_delete_failed"
	case agentdeletion.StageDeleteTweets:
		return "tweet_delete_failed"
	case agentdeletion.StageDeleteMongo:
		return "mongo_delete_failed"
	case agentdeletion.StageVerify:
		return "cross_store_verification_failed"
	case agentdeletion.StageFinalize:
		return "request_finalization_failed"
	default:
		return "stage_execution_failed"
	}
}

func isSHA256Digest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
