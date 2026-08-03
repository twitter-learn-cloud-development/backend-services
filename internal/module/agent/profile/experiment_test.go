package profile

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateExperimentCollectsPassesAndRollsBack(t *testing.T) {
	policy := ExperimentPolicy{
		MinSamplesPerArm: 2, TargetSamplesPerArm: 3,
		MaxErrorRateIncreaseBasisPoints:   1_000,
		MaxP95LatencyIncreaseBasisPoints:  2_000,
		MaxAverageCostIncreaseBasisPoints: 2_000,
	}
	collecting, err := EvaluateExperiment(policy, []ExperimentObservation{
		{Arm: ExperimentArmStable, Success: true, DurationMS: 100, CostMicros: 100},
		{Arm: ExperimentArmCandidate, Success: true, DurationMS: 100, CostMicros: 100},
	})
	require.NoError(t, err)
	require.Equal(t, ExperimentDecisionCollecting, collecting.Outcome)

	passing := make([]ExperimentObservation, 0, 6)
	for i := 0; i < 3; i++ {
		passing = append(passing,
			ExperimentObservation{Arm: ExperimentArmStable, Success: true, DurationMS: 100, CostMicros: 100},
			ExperimentObservation{Arm: ExperimentArmCandidate, Success: true, DurationMS: 110, CostMicros: 110},
		)
	}
	passed, err := EvaluateExperiment(policy, passing)
	require.NoError(t, err)
	require.Equal(t, ExperimentDecisionPass, passed.Outcome)

	regressed := append([]ExperimentObservation(nil), passing...)
	regressed[1].Success = false
	regressed[3].Success = false
	rolledBack, err := EvaluateExperiment(policy, regressed)
	require.NoError(t, err)
	require.Equal(t, ExperimentDecisionRollback, rolledBack.Outcome)
	require.Equal(t, "candidate_error_rate_regressed", rolledBack.Reason)
}

func TestEvaluateExperimentUsesNearestRankP95AndSkipsZeroBaselines(t *testing.T) {
	policy := ExperimentPolicy{
		MinSamplesPerArm: 1, TargetSamplesPerArm: 2,
		MaxErrorRateIncreaseBasisPoints:   100,
		MaxP95LatencyIncreaseBasisPoints:  100,
		MaxAverageCostIncreaseBasisPoints: 100,
	}
	decision, err := EvaluateExperiment(policy, []ExperimentObservation{
		{Arm: ExperimentArmStable, Success: true},
		{Arm: ExperimentArmCandidate, Success: true, DurationMS: 5, CostMicros: 5},
	})
	require.NoError(t, err)
	require.Equal(t, ExperimentDecisionContinue, decision.Outcome)
}

func TestExperimentObservationLimitRejectsTinyArms(t *testing.T) {
	_, err := ExperimentObservationLimit(ExperimentPolicy{}, MinExperimentArmBasisPoints-1)
	require.Error(t, err)
	limit, err := ExperimentObservationLimit(ExperimentPolicy{TargetSamplesPerArm: 200}, 1_000)
	require.NoError(t, err)
	require.Equal(t, 4_000, limit)
}

func TestEvaluateExperimentWaitsForOutcomeSamplesAndRollsBackRegression(t *testing.T) {
	policy := ExperimentPolicy{
		MinSamplesPerArm: 1, TargetSamplesPerArm: 2,
		OutcomeSignal:           ExperimentOutcomeSignalResponseAccepted,
		MinOutcomeSamplesPerArm: 2, MaxOutcomeRateDecreaseBasisPoints: 1_000,
	}
	observations := []ExperimentObservation{
		{Arm: ExperimentArmStable, Success: true, OutcomeObserved: true, OutcomePositive: true},
		{Arm: ExperimentArmStable, Success: true},
		{Arm: ExperimentArmCandidate, Success: true, OutcomeObserved: true},
		{Arm: ExperimentArmCandidate, Success: true},
	}
	collecting, err := EvaluateExperiment(policy, observations)
	require.NoError(t, err)
	require.Equal(t, ExperimentDecisionCollecting, collecting.Outcome)
	require.Equal(t, "minimum_outcome_samples_not_reached", collecting.Reason)

	observations[1].OutcomeObserved = true
	observations[1].OutcomePositive = true
	observations[3].OutcomeObserved = true
	regressed, err := EvaluateExperiment(policy, observations)
	require.NoError(t, err)
	require.Equal(t, ExperimentDecisionRollback, regressed.Outcome)
	require.Equal(t, "candidate_outcome_rate_regressed", regressed.Reason)
	require.Equal(t, 10_000, regressed.Stats.Stable.OutcomeRateBPS)
	require.Equal(t, 0, regressed.Stats.Candidate.OutcomeRateBPS)
}

func TestNormalizeExperimentPolicyKeepsLegacyAndValidatesOutcomeGate(t *testing.T) {
	legacy, err := NormalizeExperimentPolicy(ExperimentPolicy{})
	require.NoError(t, err)
	require.Empty(t, legacy.OutcomeSignal)
	require.Zero(t, legacy.MinOutcomeSamplesPerArm)

	configured, err := NormalizeExperimentPolicy(ExperimentPolicy{
		MinSamplesPerArm: 3, TargetSamplesPerArm: 5,
		OutcomeSignal: ExperimentOutcomeSignalDraftPublished,
	})
	require.NoError(t, err)
	require.Equal(t, 3, configured.MinOutcomeSamplesPerArm)
	require.Equal(t, DefaultExperimentOutcomeDecreaseBPS, configured.MaxOutcomeRateDecreaseBasisPoints)

	_, err = NormalizeExperimentPolicy(ExperimentPolicy{OutcomeSignal: "custom_dynamic_signal"})
	require.Error(t, err)
	_, err = NormalizeExperimentPolicy(ExperimentPolicy{MinOutcomeSamplesPerArm: 1})
	require.Error(t, err)
}

func TestEvaluateExperimentRejectsInvalidOutcomeAndCostOverflow(t *testing.T) {
	policy := ExperimentPolicy{MinSamplesPerArm: 1, TargetSamplesPerArm: 1}

	_, err := EvaluateExperiment(policy, []ExperimentObservation{
		{Arm: ExperimentArmStable, Success: true, OutcomePositive: true},
		{Arm: ExperimentArmCandidate, Success: true},
	})
	require.ErrorContains(t, err, "positive experiment outcome must be marked observed")

	_, err = EvaluateExperiment(policy, []ExperimentObservation{
		{Arm: ExperimentArmStable, Success: true, CostMicros: math.MaxInt64},
		{Arm: ExperimentArmStable, Success: true, CostMicros: 1},
		{Arm: ExperimentArmCandidate, Success: true},
	})
	require.ErrorContains(t, err, "cost total overflows int64")
}

func TestExceedsRelativeIncreaseDoesNotOverflow(t *testing.T) {
	require.False(t, exceedsRelativeIncrease(math.MaxInt64, math.MaxInt64, 0))
	require.True(t, exceedsRelativeIncrease(math.MaxInt64, math.MaxInt64/2, 0))
}
