package deletion

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const ProtocolSchemaV1 = "agent-data-deletion-protocol/v1"

const maxStageLease = 15 * time.Minute

const (
	CapabilitySubjectWriteFence    = "subject_write_fence"
	CapabilityRelationshipMutation = "relationship_mutation"
	CapabilityMinIOSubjectDelete   = "minio_subject_delete"
	CapabilityQdrantSubjectDelete  = "qdrant_subject_delete"
	CapabilityTweetServiceDelete   = "tweet_service_delete"
	CapabilityMongoOwnedDelete     = "mongo_owned_delete"
	CapabilityCrossStoreVerify     = "cross_store_verification"
	CapabilityRequestFinalize      = "request_finalization"
)

const (
	StageEnforceWriteFence   = "enforce_write_fence"
	StageDetachRelationships = "detach_shared_relationships"
	StageDeleteMinIO         = "delete_minio_tool_results"
	StageDeleteQdrant        = "delete_qdrant_memory"
	StageDeleteTweets        = "request_tweet_service_deletion"
	StageDeleteMongo         = "delete_agent_owned_mongo"
	StageVerify              = "verify_zero_residuals"
	StageFinalize            = "finalize_request"
)

var knownExternalResources = map[string]struct{}{
	"minio_tool_result_objects":       {},
	"qdrant_episodic_memory":          {},
	"tweet_service_published_content": {},
}

var relationshipResolutionRequired = map[string]struct{}{
	"project_ownerships":                     {},
	"project_scoped_mcp_connections_created": {},
	"marketplace_publisher_ownerships":       {},
}

var (
	ErrPlanNotReady       = errors.New("data deletion execution plan is not ready")
	ErrCheckpointConflict = errors.New("data deletion checkpoint revision conflict")
	ErrStageBusy          = errors.New("data deletion stage lease is active")
	ErrStageLeaseExpired  = errors.New("data deletion stage lease has expired")
	ErrStageNotRunning    = errors.New("data deletion stage is not running for this lease")
)

type Inventory struct {
	Owned       map[string]int64
	Related     map[string]int64
	Retained    map[string]int64
	Unsupported []string
}

type Stage struct {
	ID                 string   `bson:"id" json:"id"`
	Sequence           int      `bson:"sequence" json:"sequence"`
	DependsOn          []string `bson:"depends_on,omitempty" json:"depends_on,omitempty"`
	Boundary           string   `bson:"boundary" json:"boundary"`
	RequiredCapability string   `bson:"required_capability" json:"required_capability"`
	Destructive        bool     `bson:"destructive" json:"destructive"`
	Irreversible       bool     `bson:"irreversible" json:"irreversible"`
	RequiredEvidence   []string `bson:"required_evidence" json:"required_evidence"`
	PreservesOnFailure []string `bson:"preserves_on_failure,omitempty" json:"preserves_on_failure,omitempty"`
}

type Plan struct {
	SchemaVersion    string    `bson:"schema_version" json:"schema_version"`
	Digest           string    `bson:"digest" json:"digest"`
	InventoryDigest  string    `bson:"inventory_digest" json:"inventory_digest"`
	CapabilityDigest string    `bson:"capability_digest" json:"capability_digest"`
	Ready            bool      `bson:"ready" json:"ready"`
	Blockers         []string  `bson:"blockers,omitempty" json:"blockers,omitempty"`
	Stages           []Stage   `bson:"stages" json:"stages"`
	GeneratedAt      time.Time `bson:"generated_at" json:"generated_at"`
}

type StageStatus string

const (
	StagePending   StageStatus = "pending"
	StageRunning   StageStatus = "running"
	StageSucceeded StageStatus = "succeeded"
	StageFailed    StageStatus = "failed"
)

type StageCheckpoint struct {
	StageID       string      `bson:"stage_id" json:"stage_id"`
	Status        StageStatus `bson:"status" json:"status"`
	Attempts      int64       `bson:"attempts" json:"attempts"`
	LeaseToken    string      `bson:"lease_token,omitempty" json:"-"`
	LeaseUntil    time.Time   `bson:"lease_until,omitempty" json:"-"`
	ReceiptDigest string      `bson:"receipt_digest,omitempty" json:"receipt_digest,omitempty"`
	FailureCode   string      `bson:"failure_code,omitempty" json:"failure_code,omitempty"`
	StartedAt     time.Time   `bson:"started_at,omitempty" json:"started_at,omitempty"`
	CompletedAt   time.Time   `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
}

type Checkpoint struct {
	PlanDigest     string            `bson:"plan_digest" json:"plan_digest"`
	Revision       int64             `bson:"revision" json:"revision"`
	CurrentStageID string            `bson:"current_stage_id,omitempty" json:"current_stage_id,omitempty"`
	Stages         []StageCheckpoint `bson:"stages" json:"stages"`
	UpdatedAt      time.Time         `bson:"updated_at" json:"updated_at"`
}

type Protocol struct {
	Plan       Plan       `bson:"plan" json:"plan"`
	Checkpoint Checkpoint `bson:"checkpoint" json:"checkpoint"`
}

func BuildProtocol(inventory Inventory, capabilities CapabilitySnapshot, now time.Time) (*Protocol, error) {
	plan, err := BuildPlan(inventory, capabilities, now)
	if err != nil {
		return nil, err
	}
	checkpoint, err := NewCheckpoint(plan, now)
	if err != nil {
		return nil, err
	}
	return &Protocol{Plan: plan, Checkpoint: checkpoint}, nil
}

func BuildPlan(inventory Inventory, capabilities CapabilitySnapshot, now time.Time) (Plan, error) {
	if now.IsZero() {
		return Plan{}, errors.New("data deletion plan time is required")
	}
	if err := validateInventory(inventory); err != nil {
		return Plan{}, err
	}
	if err := ValidateCapabilitySnapshot(capabilities); err != nil {
		return Plan{}, err
	}
	stages := defaultStages()
	blockers := make([]string, 0, len(stages)+len(inventory.Unsupported))
	for _, stage := range stages {
		if !CapabilityAvailable(capabilities, stage.RequiredCapability) {
			blockers = append(blockers, "missing_capability:"+stage.RequiredCapability)
		}
	}
	for _, resource := range normalizedStrings(inventory.Unsupported) {
		if _, known := knownExternalResources[resource]; !known {
			blockers = append(blockers, "unsupported_resource:"+resource)
		}
	}
	for relationship, count := range inventory.Related {
		if count <= 0 {
			continue
		}
		if _, requiresResolution := relationshipResolutionRequired[relationship]; requiresResolution {
			blockers = append(blockers, "relationship_resolution_required:"+relationship)
		}
	}
	blockers = normalizedStrings(blockers)
	inventoryDigest := inventoryDigest(inventory)
	plan := Plan{
		SchemaVersion: ProtocolSchemaV1, InventoryDigest: inventoryDigest,
		CapabilityDigest: capabilities.Digest,
		Ready:            len(blockers) == 0, Blockers: blockers, Stages: stages, GeneratedAt: now.UTC(),
	}
	plan.Digest = planDigest(plan)
	return plan, nil
}

func NewCheckpoint(plan Plan, now time.Time) (Checkpoint, error) {
	if err := ValidatePlan(plan); err != nil {
		return Checkpoint{}, err
	}
	if now.IsZero() {
		return Checkpoint{}, errors.New("data deletion checkpoint time is required")
	}
	checkpoint := Checkpoint{
		PlanDigest: plan.Digest, Revision: 1, UpdatedAt: now.UTC(),
		Stages: make([]StageCheckpoint, len(plan.Stages)),
	}
	for index, stage := range plan.Stages {
		checkpoint.Stages[index] = StageCheckpoint{StageID: stage.ID, Status: StagePending}
	}
	return checkpoint, nil
}

func ClaimNextStage(checkpoint *Checkpoint, plan Plan, expectedRevision int64, token string, now, leaseUntil time.Time) (*StageCheckpoint, error) {
	if checkpoint == nil || expectedRevision != checkpoint.Revision {
		return nil, ErrCheckpointConflict
	}
	if err := ValidatePlan(plan); err != nil {
		return nil, err
	}
	if err := validateCheckpoint(*checkpoint, plan); err != nil {
		return nil, err
	}
	if !plan.Ready {
		return nil, ErrPlanNotReady
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 128 || now.IsZero() || !leaseUntil.After(now) || leaseUntil.Sub(now) > maxStageLease {
		return nil, errors.New("data deletion stage claim is invalid")
	}
	index, err := nextStageIndex(checkpoint)
	if err != nil {
		return nil, err
	}
	stage := &checkpoint.Stages[index]
	if stage.Status == StageRunning && stage.LeaseUntil.After(now) {
		return nil, ErrStageBusy
	}
	stage.Status = StageRunning
	stage.Attempts++
	stage.LeaseToken = token
	stage.LeaseUntil = leaseUntil.UTC()
	stage.ReceiptDigest = ""
	stage.FailureCode = ""
	stage.StartedAt = now.UTC()
	stage.CompletedAt = time.Time{}
	checkpoint.CurrentStageID = stage.StageID
	checkpoint.Revision++
	checkpoint.UpdatedAt = now.UTC()
	result := *stage
	return &result, nil
}

func CompleteStage(checkpoint *Checkpoint, plan Plan, expectedRevision int64, token string, status StageStatus, receiptDigest, failureCode string, now time.Time) error {
	if checkpoint == nil || expectedRevision != checkpoint.Revision {
		return ErrCheckpointConflict
	}
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	if err := validateCheckpoint(*checkpoint, plan); err != nil {
		return err
	}
	if status != StageSucceeded && status != StageFailed {
		return errors.New("data deletion stage completion status is invalid")
	}
	token = strings.TrimSpace(token)
	receiptDigest = strings.ToLower(strings.TrimSpace(receiptDigest))
	failureCode = strings.TrimSpace(failureCode)
	if token == "" || now.IsZero() {
		return errors.New("data deletion stage completion is invalid")
	}
	if status == StageSucceeded && !isSHA256(receiptDigest) {
		return errors.New("successful data deletion stage requires a SHA-256 receipt digest")
	}
	if status == StageFailed && !isFailureCode(failureCode) {
		return errors.New("failed data deletion stage requires a bounded failure code")
	}
	index := -1
	for candidate := range checkpoint.Stages {
		stage := &checkpoint.Stages[candidate]
		if stage.StageID == checkpoint.CurrentStageID && stage.Status == StageRunning && stage.LeaseToken == token {
			index = candidate
			break
		}
	}
	if index < 0 {
		return ErrStageNotRunning
	}
	stage := &checkpoint.Stages[index]
	if !stage.LeaseUntil.After(now) {
		return ErrStageLeaseExpired
	}
	stage.Status = status
	stage.ReceiptDigest = receiptDigest
	stage.FailureCode = failureCode
	stage.LeaseToken = ""
	stage.LeaseUntil = time.Time{}
	stage.CompletedAt = now.UTC()
	checkpoint.CurrentStageID = ""
	checkpoint.Revision++
	checkpoint.UpdatedAt = now.UTC()
	return nil
}

func ValidatePlan(plan Plan) error {
	if plan.SchemaVersion != ProtocolSchemaV1 || !isSHA256(plan.Digest) || !isSHA256(plan.InventoryDigest) || !isSHA256(plan.CapabilityDigest) || plan.GeneratedAt.IsZero() {
		return errors.New("data deletion plan identity is invalid")
	}
	want := defaultStages()
	if len(plan.Stages) != len(want) {
		return errors.New("data deletion plan stage count is invalid")
	}
	for index := range want {
		if plan.Stages[index].ID != want[index].ID || plan.Stages[index].Sequence != index+1 || plan.Stages[index].RequiredCapability != want[index].RequiredCapability {
			return errors.New("data deletion plan stages are invalid")
		}
		if index > 0 && (len(plan.Stages[index].DependsOn) != 1 || plan.Stages[index].DependsOn[0] != want[index-1].ID) {
			return errors.New("data deletion plan dependency chain is invalid")
		}
	}
	if plan.Ready != (len(plan.Blockers) == 0) {
		return errors.New("data deletion plan readiness is invalid")
	}
	if plan.Digest != planDigest(plan) {
		return errors.New("data deletion plan digest mismatch")
	}
	return nil
}

func ValidatePlanCapabilities(plan Plan, capabilities CapabilitySnapshot) error {
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	if err := ValidateCapabilitySnapshot(capabilities); err != nil {
		return err
	}
	if plan.CapabilityDigest != capabilities.Digest {
		return errors.New("data deletion capability snapshot changed; preflight is required")
	}
	for _, stage := range plan.Stages {
		if !CapabilityAvailable(capabilities, stage.RequiredCapability) {
			return fmt.Errorf("data deletion capability %q is unavailable", stage.RequiredCapability)
		}
	}
	return nil
}

func ValidateProtocol(protocol Protocol) error {
	if err := ValidatePlan(protocol.Plan); err != nil {
		return err
	}
	return validateCheckpoint(protocol.Checkpoint, protocol.Plan)
}

func validateCheckpoint(checkpoint Checkpoint, plan Plan) error {
	if checkpoint.PlanDigest != plan.Digest || checkpoint.Revision < 1 || checkpoint.UpdatedAt.IsZero() || len(checkpoint.Stages) != len(plan.Stages) {
		return errors.New("data deletion checkpoint identity is invalid")
	}
	frontierSeen := false
	runningStageID := ""
	for index, stage := range checkpoint.Stages {
		if stage.StageID != plan.Stages[index].ID || stage.Attempts < 0 {
			return errors.New("data deletion checkpoint stages are invalid")
		}
		if frontierSeen && stage.Status != StagePending {
			return errors.New("data deletion checkpoint stage order is invalid")
		}
		switch stage.Status {
		case StageSucceeded:
			if frontierSeen || !isSHA256(stage.ReceiptDigest) || stage.CompletedAt.IsZero() || stage.LeaseToken != "" || !stage.LeaseUntil.IsZero() {
				return errors.New("data deletion succeeded checkpoint is invalid")
			}
		case StagePending:
			frontierSeen = true
			if stage.LeaseToken != "" || !stage.LeaseUntil.IsZero() || stage.ReceiptDigest != "" || stage.FailureCode != "" || !stage.CompletedAt.IsZero() {
				return errors.New("data deletion pending checkpoint is invalid")
			}
		case StageRunning:
			if frontierSeen || runningStageID != "" || strings.TrimSpace(stage.LeaseToken) == "" || stage.LeaseUntil.IsZero() || stage.StartedAt.IsZero() || stage.ReceiptDigest != "" || stage.FailureCode != "" || !stage.CompletedAt.IsZero() {
				return errors.New("data deletion running checkpoint is invalid")
			}
			frontierSeen = true
			runningStageID = stage.StageID
		case StageFailed:
			if frontierSeen || !isFailureCode(stage.FailureCode) || stage.CompletedAt.IsZero() || stage.LeaseToken != "" || !stage.LeaseUntil.IsZero() || stage.ReceiptDigest != "" {
				return errors.New("data deletion failed checkpoint is invalid")
			}
			frontierSeen = true
		default:
			return errors.New("data deletion checkpoint stage status is invalid")
		}
	}
	if checkpoint.CurrentStageID != runningStageID {
		return errors.New("data deletion checkpoint current stage is invalid")
	}
	return nil
}

func defaultStages() []Stage {
	definitions := []struct {
		id, boundary, capability  string
		destructive, irreversible bool
		evidence                  []string
		preserves                 []string
	}{
		{StageEnforceWriteFence, "control", CapabilitySubjectWriteFence, false, false, []string{"subject_fence_receipt", "legal_hold_recheck"}, nil},
		{StageDetachRelationships, "mongo", CapabilityRelationshipMutation, true, true, []string{"relationship_count_before", "relationship_count_after"}, nil},
		{StageDeleteMinIO, "minio", CapabilityMinIOSubjectDelete, true, true, []string{"object_manifest_digest", "zero_remaining_objects"}, []string{"mongo_tool_result_references"}},
		{StageDeleteQdrant, "qdrant", CapabilityQdrantSubjectDelete, true, true, []string{"collection_filter_digest", "zero_remaining_points"}, nil},
		{StageDeleteTweets, "tweet_service", CapabilityTweetServiceDelete, true, true, []string{"service_request_id", "service_completion_receipt"}, nil},
		{StageDeleteMongo, "mongo", CapabilityMongoOwnedDelete, true, true, []string{"collection_counts_before", "collection_counts_after"}, []string{"deletion_request", "governance_retention_records"}},
		{StageVerify, "verification", CapabilityCrossStoreVerify, false, false, []string{"cross_store_zero_residual_report"}, nil},
		{StageFinalize, "control", CapabilityRequestFinalize, false, false, []string{"final_protocol_digest", "completed_at"}, nil},
	}
	stages := make([]Stage, len(definitions))
	for index, definition := range definitions {
		stage := Stage{
			ID: definition.id, Sequence: index + 1, Boundary: definition.boundary,
			RequiredCapability: definition.capability, Destructive: definition.destructive,
			Irreversible:       definition.irreversible,
			RequiredEvidence:   append([]string(nil), definition.evidence...),
			PreservesOnFailure: append([]string(nil), definition.preserves...),
		}
		if index > 0 {
			stage.DependsOn = []string{definitions[index-1].id}
		}
		stages[index] = stage
	}
	return stages
}

func nextStageIndex(checkpoint *Checkpoint) (int, error) {
	for index := range checkpoint.Stages {
		stage := checkpoint.Stages[index]
		if stage.Status == StageSucceeded {
			continue
		}
		if index > 0 && checkpoint.Stages[index-1].Status != StageSucceeded {
			return -1, errors.New("data deletion stage dependency is incomplete")
		}
		if stage.Status != StagePending && stage.Status != StageFailed && stage.Status != StageRunning {
			return -1, errors.New("data deletion stage status is invalid")
		}
		return index, nil
	}
	return -1, errors.New("data deletion protocol is already complete")
}

func validateInventory(inventory Inventory) error {
	for category, values := range map[string]map[string]int64{
		"owned": inventory.Owned, "related": inventory.Related, "retained": inventory.Retained,
	} {
		for name, count := range values {
			if strings.TrimSpace(name) == "" || count < 0 {
				return fmt.Errorf("data deletion %s inventory is invalid", category)
			}
		}
	}
	return nil
}

func inventoryDigest(inventory Inventory) string {
	parts := []string{ProtocolSchemaV1}
	parts = appendMap(parts, "owned", inventory.Owned)
	parts = appendMap(parts, "related", inventory.Related)
	parts = appendMap(parts, "retained", inventory.Retained)
	for _, value := range normalizedStrings(inventory.Unsupported) {
		parts = append(parts, "unsupported:"+value)
	}
	return sha256Hex(strings.Join(parts, "\x00"))
}

func planDigest(plan Plan) string {
	parts := []string{plan.SchemaVersion, plan.InventoryDigest, plan.CapabilityDigest, plan.GeneratedAt.UTC().Format(time.RFC3339Nano), fmt.Sprint(plan.Ready)}
	for _, blocker := range normalizedStrings(plan.Blockers) {
		parts = append(parts, "blocker:"+blocker)
	}
	for _, stage := range plan.Stages {
		parts = append(parts, strings.Join([]string{
			stage.ID, fmt.Sprint(stage.Sequence), strings.Join(stage.DependsOn, ","), stage.Boundary,
			stage.RequiredCapability, fmt.Sprint(stage.Destructive), fmt.Sprint(stage.Irreversible),
			strings.Join(stage.RequiredEvidence, ","), strings.Join(stage.PreservesOnFailure, ","),
		}, "|"))
	}
	return sha256Hex(strings.Join(parts, "\x00"))
}

func appendMap(parts []string, category string, values map[string]int64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%s=%d", category, key, values[key]))
	}
	return parts
}

func normalizedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func isSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func isFailureCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}
