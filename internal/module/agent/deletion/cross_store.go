package deletion

import (
	"errors"
	"sort"
	"strings"
	"time"
)

const CrossStoreReportSchemaV1 = "agent-data-deletion-cross-store-report/v1"

var requiredCrossStoreCapabilities = []string{
	CapabilityRelationshipMutation,
	CapabilityMinIOSubjectDelete,
	CapabilityQdrantSubjectDelete,
	CapabilityTweetServiceDelete,
	CapabilityMongoOwnedDelete,
}

type CrossStoreVerificationReport struct {
	SchemaVersion    string      `json:"schema_version"`
	ReportDigest     string      `json:"report_digest"`
	RequestID        string      `json:"request_id"`
	SubjectDigest    string      `json:"subject_digest"`
	PlanDigest       string      `json:"plan_digest"`
	CapabilityDigest string      `json:"capability_digest"`
	States           []PostState `json:"states"`
	ResidualCount    int64       `json:"residual_count"`
	ObservedAt       time.Time   `json:"observed_at"`
}

func BuildCrossStoreVerificationReport(
	requestID string,
	userID uint64,
	planDigest string,
	capabilities CapabilitySnapshot,
	states []PostState,
	observedAt time.Time,
) (CrossStoreVerificationReport, error) {
	report := CrossStoreVerificationReport{
		SchemaVersion: CrossStoreReportSchemaV1,
		RequestID:     strings.TrimSpace(requestID), SubjectDigest: DeletionSubjectDigest(userID),
		PlanDigest: strings.ToLower(strings.TrimSpace(planDigest)), CapabilityDigest: capabilities.Digest,
		States: append([]PostState(nil), states...), ObservedAt: observedAt.UTC(),
	}
	if userID == 0 || !isBoundedIdentity(report.RequestID, 64) || !isSHA256(report.PlanDigest) || observedAt.IsZero() {
		return CrossStoreVerificationReport{}, errors.New("cross-store verification request is invalid")
	}
	if err := ValidateCapabilitySnapshot(capabilities); err != nil {
		return CrossStoreVerificationReport{}, err
	}
	expected := make(map[string]CapabilityDescriptor, len(requiredCrossStoreCapabilities))
	for _, capabilityID := range requiredCrossStoreCapabilities {
		for _, descriptor := range capabilities.Capabilities {
			if descriptor.ID == capabilityID {
				expected[capabilityID] = descriptor
				break
			}
		}
		if _, ok := expected[capabilityID]; !ok {
			return CrossStoreVerificationReport{}, errors.New("cross-store verification capability is missing")
		}
	}
	seen := make(map[string]struct{}, len(states))
	for _, state := range report.States {
		if err := ValidatePostState(state); err != nil {
			return CrossStoreVerificationReport{}, err
		}
		descriptor, ok := expected[state.Capability.ID]
		if !ok || descriptor != state.Capability || state.SubjectDigest != report.SubjectDigest || state.ObservedAt.After(report.ObservedAt) {
			return CrossStoreVerificationReport{}, errors.New("cross-store verification state binding is invalid")
		}
		if _, duplicate := seen[state.Capability.ID]; duplicate {
			return CrossStoreVerificationReport{}, errors.New("cross-store verification contains a duplicate capability")
		}
		seen[state.Capability.ID] = struct{}{}
		report.ResidualCount += state.ResidualCount
	}
	if len(seen) != len(requiredCrossStoreCapabilities) {
		return CrossStoreVerificationReport{}, errors.New("cross-store verification state is incomplete")
	}
	sort.Slice(report.States, func(left, right int) bool {
		return report.States[left].Capability.ID < report.States[right].Capability.ID
	})
	report.ReportDigest = crossStoreReportDigest(report)
	if report.ResidualCount != 0 {
		return report, ErrDeletionResidualsRemain
	}
	return report, nil
}

func ValidateCrossStoreVerificationReport(report CrossStoreVerificationReport) error {
	if report.SchemaVersion != CrossStoreReportSchemaV1 || !isSHA256(report.ReportDigest) ||
		!isBoundedIdentity(report.RequestID, 64) || !isSHA256(report.SubjectDigest) ||
		!isSHA256(report.PlanDigest) || !isSHA256(report.CapabilityDigest) || report.ObservedAt.IsZero() ||
		report.ResidualCount != 0 || report.ReportDigest != crossStoreReportDigest(report) {
		return errors.New("cross-store verification report is invalid")
	}
	expectedIDs := append([]string(nil), requiredCrossStoreCapabilities...)
	sort.Strings(expectedIDs)
	previous := ""
	for index, state := range report.States {
		if err := ValidatePostState(state); err != nil {
			return err
		}
		if index >= len(expectedIDs) || state.Capability.ID != expectedIDs[index] || state.Capability.ID <= previous ||
			state.SubjectDigest != report.SubjectDigest || state.ResidualCount != 0 || state.ObservedAt.After(report.ObservedAt) {
			return errors.New("cross-store verification report state is invalid")
		}
		previous = state.Capability.ID
	}
	if len(report.States) != len(requiredCrossStoreCapabilities) {
		return errors.New("cross-store verification report state is incomplete")
	}
	return nil
}

func crossStoreReportDigest(report CrossStoreVerificationReport) string {
	parts := []string{
		report.SchemaVersion, report.RequestID, report.SubjectDigest, report.PlanDigest,
		report.CapabilityDigest, report.ObservedAt.UTC().Format(time.RFC3339Nano),
	}
	for _, state := range report.States {
		parts = append(parts, state.Capability.ID+"|"+state.Capability.Adapter+"|"+state.Capability.Version+"|"+state.ScopeDigest+"|"+state.StateDigest)
	}
	return sha256Hex(strings.Join(parts, "\x00"))
}
