package deletion

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	AdapterContractSchemaV1  = "agent-data-deletion-adapter/v1"
	GovernanceDecisionSchema = "agent-data-deletion-governance/v1"
	PostStateSchemaV1        = "agent-data-deletion-post-state/v1"
	ExecutionReceiptSchemaV1 = "agent-data-deletion-receipt/v1"
)

var (
	ErrDeletionLegalHold         = errors.New("data deletion adapter is blocked by legal hold")
	ErrDeletionGovernanceExpired = errors.New("data deletion governance decision has expired")
	ErrDeletionResidualsRemain   = errors.New("data deletion adapter left residual resources")
	ErrDeletionReceiptMismatch   = errors.New("data deletion adapter receipt does not match the request or post-state")
)

type GovernanceStatus string

const (
	GovernanceClear GovernanceStatus = "clear"
	GovernanceHeld  GovernanceStatus = "held"
)

// GovernanceDecision is a short-lived, digest-bound projection of the
// authoritative retention state. It contains no raw user identity.
type GovernanceDecision struct {
	SchemaVersion  string           `bson:"schema_version" json:"schema_version"`
	SubjectDigest  string           `bson:"subject_digest" json:"subject_digest"`
	Status         GovernanceStatus `bson:"status" json:"status"`
	Revision       int64            `bson:"revision" json:"revision"`
	EvidenceDigest string           `bson:"evidence_digest" json:"evidence_digest"`
	ObservedAt     time.Time        `bson:"observed_at" json:"observed_at"`
	ValidUntil     time.Time        `bson:"valid_until" json:"valid_until"`
	Digest         string           `bson:"digest" json:"digest"`
}

type AdapterRequest struct {
	SchemaVersion    string               `bson:"schema_version" json:"schema_version"`
	RequestID        string               `bson:"request_id" json:"request_id"`
	StageID          string               `bson:"stage_id" json:"stage_id"`
	UserID           uint64               `bson:"-" json:"-"`
	SubjectDigest    string               `bson:"subject_digest" json:"subject_digest"`
	PlanDigest       string               `bson:"plan_digest" json:"plan_digest"`
	CapabilityDigest string               `bson:"capability_digest" json:"capability_digest"`
	Capability       CapabilityDescriptor `bson:"capability" json:"capability"`
	ScopeDigest      string               `bson:"scope_digest" json:"scope_digest"`
	OperationKey     string               `bson:"operation_key" json:"operation_key"`
	Governance       GovernanceDecision   `bson:"governance" json:"governance"`
	Deadline         time.Time            `bson:"deadline" json:"deadline"`
}

type AdapterRequestInput struct {
	RequestID        string
	StageID          string
	UserID           uint64
	PlanDigest       string
	CapabilityDigest string
	Capability       CapabilityDescriptor
	ScopeDigest      string
	Governance       GovernanceDecision
	Now              time.Time
	Deadline         time.Time
}

type ObservationRequest struct {
	RequestID     string               `json:"request_id"`
	StageID       string               `json:"stage_id"`
	UserID        uint64               `json:"-"`
	SubjectDigest string               `json:"subject_digest"`
	Capability    CapabilityDescriptor `json:"capability"`
	ScopeDigest   string               `json:"scope_digest"`
	OperationKey  string               `json:"operation_key"`
}

type PostState struct {
	SchemaVersion string               `bson:"schema_version" json:"schema_version"`
	Capability    CapabilityDescriptor `bson:"capability" json:"capability"`
	SubjectDigest string               `bson:"subject_digest" json:"subject_digest"`
	ScopeDigest   string               `bson:"scope_digest" json:"scope_digest"`
	ResidualCount int64                `bson:"residual_count" json:"residual_count"`
	StateDigest   string               `bson:"state_digest" json:"state_digest"`
	ObservedAt    time.Time            `bson:"observed_at" json:"observed_at"`
}

type ExecutionReceipt struct {
	SchemaVersion     string               `bson:"schema_version" json:"schema_version"`
	ReceiptDigest     string               `bson:"receipt_digest" json:"receipt_digest"`
	RequestID         string               `bson:"request_id" json:"request_id"`
	StageID           string               `bson:"stage_id" json:"stage_id"`
	SubjectDigest     string               `bson:"subject_digest" json:"subject_digest"`
	PlanDigest        string               `bson:"plan_digest" json:"plan_digest"`
	CapabilityDigest  string               `bson:"capability_digest" json:"capability_digest"`
	Capability        CapabilityDescriptor `bson:"capability" json:"capability"`
	ScopeDigest       string               `bson:"scope_digest" json:"scope_digest"`
	OperationKey      string               `bson:"operation_key" json:"operation_key"`
	GovernanceDigest  string               `bson:"governance_digest" json:"governance_digest"`
	BeforeStateDigest string               `bson:"before_state_digest" json:"before_state_digest"`
	AfterStateDigest  string               `bson:"after_state_digest" json:"after_state_digest"`
	BeforeCount       int64                `bson:"before_count" json:"before_count"`
	AfterCount        int64                `bson:"after_count" json:"after_count"`
	AffectedCount     int64                `bson:"affected_count" json:"affected_count"`
	CompletedAt       time.Time            `bson:"completed_at" json:"completed_at"`
}

type AdapterResult struct {
	Receipt             ExecutionReceipt `json:"receipt"`
	Replayed            bool             `json:"replayed"`
	AuthorizationDigest string           `json:"authorization_digest"`
}

// DeletionAdapter is deliberately narrower than a worker. Implementations
// own their storage transaction and idempotency record, while orchestration
// remains outside this package.
type DeletionAdapter interface {
	CapabilityProvider
	Observe(context.Context, ObservationRequest) (PostState, error)
	Delete(context.Context, AdapterRequest) (AdapterResult, error)
}

func NewGovernanceDecision(
	userID uint64,
	status GovernanceStatus,
	revision int64,
	evidenceDigest string,
	observedAt time.Time,
	validUntil time.Time,
) (GovernanceDecision, error) {
	decision := GovernanceDecision{
		SchemaVersion: GovernanceDecisionSchema, SubjectDigest: DeletionSubjectDigest(userID),
		Status: status, Revision: revision, EvidenceDigest: strings.ToLower(strings.TrimSpace(evidenceDigest)),
		ObservedAt: observedAt.UTC(), ValidUntil: validUntil.UTC(),
	}
	decision.Digest = governanceDecisionDigest(decision)
	if err := ValidateGovernanceDecision(decision); err != nil {
		return GovernanceDecision{}, err
	}
	return decision, nil
}

func ValidateGovernanceDecision(decision GovernanceDecision) error {
	if decision.SchemaVersion != GovernanceDecisionSchema || !isSHA256(decision.SubjectDigest) ||
		(decision.Status != GovernanceClear && decision.Status != GovernanceHeld) || decision.Revision < 1 ||
		!isSHA256(decision.EvidenceDigest) || decision.ObservedAt.IsZero() ||
		!decision.ValidUntil.After(decision.ObservedAt) || decision.ValidUntil.Sub(decision.ObservedAt) > maxStageLease ||
		!isSHA256(decision.Digest) || decision.Digest != governanceDecisionDigest(decision) {
		return errors.New("data deletion governance decision is invalid")
	}
	return nil
}

func AuthorizeGovernanceDecision(decision GovernanceDecision, userID uint64, now time.Time) error {
	if err := ValidateGovernanceDecision(decision); err != nil {
		return err
	}
	if userID == 0 || now.IsZero() || decision.SubjectDigest != DeletionSubjectDigest(userID) || now.Before(decision.ObservedAt) {
		return errors.New("data deletion governance decision subject or time is invalid")
	}
	if !decision.ValidUntil.After(now) {
		return ErrDeletionGovernanceExpired
	}
	if decision.Status == GovernanceHeld {
		return ErrDeletionLegalHold
	}
	return nil
}

func NewAdapterRequest(input AdapterRequestInput) (AdapterRequest, error) {
	descriptor := normalizeCapabilityDescriptor(input.Capability)
	request := AdapterRequest{
		SchemaVersion: AdapterContractSchemaV1, RequestID: strings.TrimSpace(input.RequestID),
		StageID: strings.TrimSpace(input.StageID), UserID: input.UserID,
		SubjectDigest: DeletionSubjectDigest(input.UserID), PlanDigest: strings.ToLower(strings.TrimSpace(input.PlanDigest)),
		CapabilityDigest: strings.ToLower(strings.TrimSpace(input.CapabilityDigest)), Capability: descriptor,
		ScopeDigest: strings.ToLower(strings.TrimSpace(input.ScopeDigest)), Governance: input.Governance,
		Deadline: input.Deadline.UTC(),
	}
	request.OperationKey = AdapterOperationKey(request)
	if err := ValidateAdapterRequest(request, input.Now); err != nil {
		return AdapterRequest{}, err
	}
	return request, nil
}

func ValidateAdapterRequest(request AdapterRequest, now time.Time) error {
	if request.SchemaVersion != AdapterContractSchemaV1 || !isBoundedIdentity(request.RequestID, 64) ||
		request.UserID == 0 || request.SubjectDigest != DeletionSubjectDigest(request.UserID) ||
		!isSHA256(request.PlanDigest) || !isSHA256(request.CapabilityDigest) || !isSHA256(request.ScopeDigest) ||
		!isSHA256(request.OperationKey) || request.OperationKey != AdapterOperationKey(request) ||
		now.IsZero() || !request.Deadline.After(now) || request.Deadline.Sub(now) > maxStageLease {
		return errors.New("data deletion adapter request is invalid")
	}
	if err := validateCapabilityDescriptor(request.Capability); err != nil {
		return err
	}
	stage, ok := deletionStage(request.StageID)
	if !ok || !stage.Destructive || stage.RequiredCapability != request.Capability.ID {
		return errors.New("data deletion adapter request stage capability is invalid")
	}
	return AuthorizeGovernanceDecision(request.Governance, request.UserID, now)
}

func ValidateAdapterBinding(provider CapabilityProvider, request AdapterRequest) error {
	if provider == nil {
		return errors.New("data deletion adapter provider is required")
	}
	descriptor := normalizeCapabilityDescriptor(provider.DeletionCapability())
	if err := validateCapabilityDescriptor(descriptor); err != nil {
		return err
	}
	if descriptor != request.Capability {
		return errors.New("data deletion adapter implementation does not match the request capability")
	}
	return nil
}

func (request AdapterRequest) ObservationRequest() ObservationRequest {
	return ObservationRequest{
		RequestID: request.RequestID, StageID: request.StageID, UserID: request.UserID,
		SubjectDigest: request.SubjectDigest, Capability: request.Capability,
		ScopeDigest: request.ScopeDigest, OperationKey: request.OperationKey,
	}
}

func NewPostState(
	capability CapabilityDescriptor,
	subjectDigest string,
	scopeDigest string,
	residualCount int64,
	observedAt time.Time,
) (PostState, error) {
	state := PostState{
		SchemaVersion: PostStateSchemaV1, Capability: normalizeCapabilityDescriptor(capability),
		SubjectDigest: strings.ToLower(strings.TrimSpace(subjectDigest)),
		ScopeDigest:   strings.ToLower(strings.TrimSpace(scopeDigest)), ResidualCount: residualCount,
		ObservedAt: observedAt.UTC(),
	}
	state.StateDigest = postStateDigest(state)
	if err := ValidatePostState(state); err != nil {
		return PostState{}, err
	}
	return state, nil
}

func ValidatePostState(state PostState) error {
	if state.SchemaVersion != PostStateSchemaV1 || !isSHA256(state.SubjectDigest) || !isSHA256(state.ScopeDigest) ||
		state.ResidualCount < 0 || !isSHA256(state.StateDigest) || state.ObservedAt.IsZero() ||
		state.StateDigest != postStateDigest(state) {
		return errors.New("data deletion adapter post-state is invalid")
	}
	return validateCapabilityDescriptor(state.Capability)
}

func NewExecutionReceipt(request AdapterRequest, before, after PostState, completedAt time.Time) (ExecutionReceipt, error) {
	receipt := ExecutionReceipt{
		SchemaVersion: ExecutionReceiptSchemaV1, RequestID: request.RequestID, StageID: request.StageID,
		SubjectDigest: request.SubjectDigest, PlanDigest: request.PlanDigest,
		CapabilityDigest: request.CapabilityDigest, Capability: request.Capability,
		ScopeDigest: request.ScopeDigest, OperationKey: request.OperationKey,
		GovernanceDigest: request.Governance.Digest, BeforeStateDigest: before.StateDigest,
		AfterStateDigest: after.StateDigest, BeforeCount: before.ResidualCount,
		AfterCount: after.ResidualCount, AffectedCount: before.ResidualCount - after.ResidualCount,
		CompletedAt: completedAt.UTC(),
	}
	receipt.ReceiptDigest = executionReceiptDigest(receipt)
	if err := ValidateExecutionReceipt(receipt); err != nil {
		return ExecutionReceipt{}, err
	}
	if err := validateReceiptStates(receipt, before, after); err != nil {
		return ExecutionReceipt{}, err
	}
	return receipt, nil
}

func ValidateExecutionReceipt(receipt ExecutionReceipt) error {
	if receipt.SchemaVersion != ExecutionReceiptSchemaV1 || !isSHA256(receipt.ReceiptDigest) ||
		!isBoundedIdentity(receipt.RequestID, 64) || !isSHA256(receipt.SubjectDigest) ||
		!isSHA256(receipt.PlanDigest) || !isSHA256(receipt.CapabilityDigest) || !isSHA256(receipt.ScopeDigest) ||
		!isSHA256(receipt.OperationKey) || !isSHA256(receipt.GovernanceDigest) ||
		!isSHA256(receipt.BeforeStateDigest) || !isSHA256(receipt.AfterStateDigest) ||
		receipt.BeforeCount < 0 || receipt.AfterCount < 0 || receipt.AffectedCount < 0 ||
		receipt.BeforeCount-receipt.AfterCount != receipt.AffectedCount || receipt.AfterCount != 0 ||
		receipt.CompletedAt.IsZero() || receipt.ReceiptDigest != executionReceiptDigest(receipt) {
		return errors.New("data deletion execution receipt is invalid")
	}
	if err := validateCapabilityDescriptor(receipt.Capability); err != nil {
		return err
	}
	stage, ok := deletionStage(receipt.StageID)
	if !ok || !stage.Destructive || stage.RequiredCapability != receipt.Capability.ID {
		return errors.New("data deletion receipt stage capability is invalid")
	}
	return nil
}

func VerifyAdapterResult(provider CapabilityProvider, request AdapterRequest, result AdapterResult, postState PostState, now time.Time) error {
	if err := ValidateAdapterBinding(provider, request); err != nil {
		return err
	}
	if err := ValidateExecutionReceipt(result.Receipt); err != nil {
		return err
	}
	if err := ValidateAdapterRequest(request, result.Receipt.CompletedAt); err != nil {
		return err
	}
	if err := ValidatePostState(postState); err != nil {
		return err
	}
	if now.IsZero() || now.Before(result.Receipt.CompletedAt) || now.Before(postState.ObservedAt) {
		return ErrDeletionReceiptMismatch
	}
	receipt := result.Receipt
	if receipt.RequestID != request.RequestID || receipt.StageID != request.StageID ||
		receipt.SubjectDigest != request.SubjectDigest || receipt.PlanDigest != request.PlanDigest ||
		receipt.CapabilityDigest != request.CapabilityDigest || receipt.Capability != request.Capability ||
		receipt.ScopeDigest != request.ScopeDigest || receipt.OperationKey != request.OperationKey ||
		!isSHA256(result.AuthorizationDigest) || result.AuthorizationDigest != request.Governance.Digest ||
		(!result.Replayed && receipt.GovernanceDigest != request.Governance.Digest) ||
		postState.Capability != request.Capability || postState.SubjectDigest != request.SubjectDigest ||
		postState.ScopeDigest != request.ScopeDigest || postState.StateDigest != receipt.AfterStateDigest ||
		postState.ResidualCount != 0 || postState.ObservedAt.Before(receipt.CompletedAt) {
		return ErrDeletionReceiptMismatch
	}
	return nil
}

func validateReceiptStates(receipt ExecutionReceipt, before, after PostState) error {
	if before.Capability != receipt.Capability || after.Capability != receipt.Capability ||
		before.SubjectDigest != receipt.SubjectDigest || after.SubjectDigest != receipt.SubjectDigest ||
		before.ScopeDigest != receipt.ScopeDigest || after.ScopeDigest != receipt.ScopeDigest ||
		before.StateDigest != receipt.BeforeStateDigest || after.StateDigest != receipt.AfterStateDigest ||
		before.ResidualCount != receipt.BeforeCount || after.ResidualCount != receipt.AfterCount ||
		after.ObservedAt.Before(before.ObservedAt) || receipt.CompletedAt.Before(after.ObservedAt) {
		return ErrDeletionReceiptMismatch
	}
	return nil
}

func DeletionSubjectDigest(userID uint64) string {
	return sha256Hex("agent-data-deletion-subject/v1\x00" + strconv.FormatUint(userID, 10))
}

func DeletionScopeDigest(parts ...string) string {
	return sha256Hex("agent-data-deletion-scope/v1\x00" + strings.Join(normalizedStrings(parts), "\x00"))
}

func AdapterOperationKey(request AdapterRequest) string {
	return sha256Hex(strings.Join([]string{
		AdapterContractSchemaV1, request.RequestID, request.StageID, request.SubjectDigest,
		request.PlanDigest, request.CapabilityDigest, request.Capability.ID, request.Capability.Adapter,
		request.Capability.Version, request.ScopeDigest,
	}, "\x00"))
}

func governanceDecisionDigest(decision GovernanceDecision) string {
	return sha256Hex(strings.Join([]string{
		decision.SchemaVersion, decision.SubjectDigest, string(decision.Status),
		strconv.FormatInt(decision.Revision, 10), decision.EvidenceDigest,
		decision.ObservedAt.UTC().Format(time.RFC3339Nano), decision.ValidUntil.UTC().Format(time.RFC3339Nano),
	}, "\x00"))
}

func postStateDigest(state PostState) string {
	return sha256Hex(strings.Join([]string{
		state.SchemaVersion, state.Capability.ID, state.Capability.Adapter, state.Capability.Version,
		state.SubjectDigest, state.ScopeDigest, strconv.FormatInt(state.ResidualCount, 10),
	}, "\x00"))
}

func executionReceiptDigest(receipt ExecutionReceipt) string {
	return sha256Hex(strings.Join([]string{
		receipt.SchemaVersion, receipt.RequestID, receipt.StageID, receipt.SubjectDigest,
		receipt.PlanDigest, receipt.CapabilityDigest, receipt.Capability.ID, receipt.Capability.Adapter,
		receipt.Capability.Version, receipt.ScopeDigest, receipt.OperationKey, receipt.GovernanceDigest,
		receipt.BeforeStateDigest, receipt.AfterStateDigest, strconv.FormatInt(receipt.BeforeCount, 10),
		strconv.FormatInt(receipt.AfterCount, 10), strconv.FormatInt(receipt.AffectedCount, 10),
		receipt.CompletedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00"))
}

func deletionStage(stageID string) (Stage, bool) {
	for _, stage := range defaultStages() {
		if stage.ID == stageID {
			return stage, true
		}
	}
	return Stage{}, false
}

func ValidateObservationRequest(request ObservationRequest) error {
	if !isBoundedIdentity(request.RequestID, 64) || request.UserID == 0 ||
		request.SubjectDigest != DeletionSubjectDigest(request.UserID) || !isSHA256(request.ScopeDigest) ||
		!isSHA256(request.OperationKey) {
		return errors.New("data deletion observation request is invalid")
	}
	if err := validateCapabilityDescriptor(request.Capability); err != nil {
		return err
	}
	stage, ok := deletionStage(request.StageID)
	if !ok || !stage.Destructive || stage.RequiredCapability != request.Capability.ID {
		return fmt.Errorf("data deletion observation stage %q is invalid", request.StageID)
	}
	return nil
}
