package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// CompositeEvidenceCollector combines independent domain collectors without
// teaching VerifiedRunner about artifact or tool-specific semantics.
type CompositeEvidenceCollector struct {
	Collectors []EvidenceCollector
}

func (collector CompositeEvidenceCollector) Collect(
	ctx context.Context,
	request EvidenceCollectionRequest,
) ([]Evidence, error) {
	var result []Evidence
	for index, child := range collector.Collectors {
		if child == nil {
			return nil, fmt.Errorf("evidence collector %d is not configured", index)
		}
		items, err := child.Collect(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("collect evidence with collector %d: %w", index, err)
		}
		result = append(result, items...)
	}
	return result, nil
}

// FinalAnswerArtifactEvidenceCollector records a digest/reference for a
// completed model output. It never copies the answer into the evidence ledger.
type FinalAnswerArtifactEvidenceCollector struct {
	ArtifactType string
	CriterionIDs []string
}

func (collector FinalAnswerArtifactEvidenceCollector) Collect(
	_ context.Context,
	request EvidenceCollectionRequest,
) ([]Evidence, error) {
	artifactType := strings.TrimSpace(collector.ArtifactType)
	if artifactType == "" {
		return nil, fmt.Errorf("artifact type is required")
	}
	criterionIDs := sortedUniqueStrings(collector.CriterionIDs)
	if len(criterionIDs) == 0 {
		return nil, fmt.Errorf("artifact criterion IDs are required")
	}
	answer := strings.TrimSpace(request.Run.FinalAnswer)
	if request.Run.Status != RunStatusCompleted || answer == "" {
		return nil, nil
	}

	digest := sha256.Sum256([]byte(answer))
	return []Evidence{{
		ID: fmt.Sprintf(
			"artifact:%s:%d:final-answer",
			request.Run.Context.RunID,
			request.Attempt,
		),
		Kind:         EvidenceArtifact,
		Source:       artifactType,
		CriterionIDs: criterionIDs,
		Digest:       "sha256:" + hex.EncodeToString(digest[:]),
		Reference: fmt.Sprintf(
			"agent-run://%s/attempt/%d/final-answer",
			request.Run.Context.RunID,
			request.Attempt,
		),
		StepIndex: len(request.Run.Steps),
	}}, nil
}
