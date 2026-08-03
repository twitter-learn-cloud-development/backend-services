package profile

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strings"
)

const (
	ExperimentStatusRunning    = "running"
	ExperimentStatusPassed     = "passed"
	ExperimentStatusRolledBack = "rolled_back"
	ExperimentStatusStopped    = "stopped"
	ExperimentStatusSuperseded = "superseded"

	ExperimentDecisionCollecting = "collecting"
	ExperimentDecisionContinue   = "continue"
	ExperimentDecisionPass       = "pass"
	ExperimentDecisionRollback   = "rollback"
	ExperimentDecisionStop       = "stop"
	ExperimentDecisionSuperseded = "superseded"

	ExperimentArmStable    = "stable"
	ExperimentArmCandidate = "candidate"

	ExperimentOutcomeSignalResponseAccepted = "response_accepted"
	ExperimentOutcomeSignalDraftPublished   = "draft_published"
	ExperimentOutcomeSignalContentEngaged   = "content_engaged"

	DefaultExperimentMinSamplesPerArm    = 50
	DefaultExperimentTargetSamplesPerArm = 200
	DefaultExperimentOutcomeDecreaseBPS  = 1_000
	MaxExperimentSamplesPerArm           = 5_000
	MinExperimentArmBasisPoints          = 100
	MaxExperimentObservationScan         = 100_000
)

type ExperimentPolicy struct {
	MinSamplesPerArm                  int    `bson:"min_samples_per_arm" json:"min_samples_per_arm"`
	TargetSamplesPerArm               int    `bson:"target_samples_per_arm" json:"target_samples_per_arm"`
	MaxErrorRateIncreaseBasisPoints   int    `bson:"max_error_rate_increase_basis_points" json:"max_error_rate_increase_basis_points"`
	MaxP95LatencyIncreaseBasisPoints  int    `bson:"max_p95_latency_increase_basis_points" json:"max_p95_latency_increase_basis_points"`
	MaxAverageCostIncreaseBasisPoints int    `bson:"max_average_cost_increase_basis_points" json:"max_average_cost_increase_basis_points"`
	OutcomeSignal                     string `bson:"outcome_signal,omitempty" json:"outcome_signal,omitempty"`
	MinOutcomeSamplesPerArm           int    `bson:"min_outcome_samples_per_arm,omitempty" json:"min_outcome_samples_per_arm,omitempty"`
	MaxOutcomeRateDecreaseBasisPoints int    `bson:"max_outcome_rate_decrease_basis_points,omitempty" json:"max_outcome_rate_decrease_basis_points,omitempty"`
}

type ExperimentObservation struct {
	Arm             string
	Success         bool
	DurationMS      int64
	CostMicros      int64
	OutcomeObserved bool
	OutcomePositive bool
}

type ExperimentArmStats struct {
	Samples           int   `bson:"samples" json:"samples"`
	Successes         int   `bson:"successes" json:"successes"`
	Failures          int   `bson:"failures" json:"failures"`
	ErrorRateBPS      int   `bson:"error_rate_bps" json:"error_rate_bps"`
	P95LatencyMS      int64 `bson:"p95_latency_ms" json:"p95_latency_ms"`
	AverageCostMicros int64 `bson:"average_cost_micros" json:"average_cost_micros"`
	OutcomeSamples    int   `bson:"outcome_samples,omitempty" json:"outcome_samples,omitempty"`
	OutcomePositives  int   `bson:"outcome_positives,omitempty" json:"outcome_positives,omitempty"`
	OutcomeRateBPS    int   `bson:"outcome_rate_bps,omitempty" json:"outcome_rate_bps,omitempty"`
}

type ExperimentStats struct {
	Stable    ExperimentArmStats `bson:"stable" json:"stable"`
	Candidate ExperimentArmStats `bson:"candidate" json:"candidate"`
}

type ExperimentGateDecision struct {
	Outcome string          `json:"outcome"`
	Reason  string          `json:"reason"`
	Stats   ExperimentStats `json:"stats"`
}

func NormalizeExperimentPolicy(policy ExperimentPolicy) (ExperimentPolicy, error) {
	policy.OutcomeSignal = strings.TrimSpace(policy.OutcomeSignal)
	if policy.MinSamplesPerArm == 0 {
		policy.MinSamplesPerArm = DefaultExperimentMinSamplesPerArm
	}
	if policy.TargetSamplesPerArm == 0 {
		policy.TargetSamplesPerArm = DefaultExperimentTargetSamplesPerArm
	}
	if policy.MaxErrorRateIncreaseBasisPoints == 0 {
		policy.MaxErrorRateIncreaseBasisPoints = 500
	}
	if policy.MaxP95LatencyIncreaseBasisPoints == 0 {
		policy.MaxP95LatencyIncreaseBasisPoints = 2_000
	}
	if policy.MaxAverageCostIncreaseBasisPoints == 0 {
		policy.MaxAverageCostIncreaseBasisPoints = 2_000
	}
	if policy.MinSamplesPerArm < 1 || policy.MinSamplesPerArm > MaxExperimentSamplesPerArm {
		return ExperimentPolicy{}, fmt.Errorf("min_samples_per_arm must be within 1..%d", MaxExperimentSamplesPerArm)
	}
	if policy.TargetSamplesPerArm < policy.MinSamplesPerArm || policy.TargetSamplesPerArm > MaxExperimentSamplesPerArm {
		return ExperimentPolicy{}, fmt.Errorf("target_samples_per_arm must be within min_samples_per_arm..%d", MaxExperimentSamplesPerArm)
	}
	thresholds := []int{
		policy.MaxErrorRateIncreaseBasisPoints,
		policy.MaxP95LatencyIncreaseBasisPoints,
		policy.MaxAverageCostIncreaseBasisPoints,
	}
	for _, threshold := range thresholds {
		if threshold < 0 || threshold > MaxReleaseBasisPoints {
			return ExperimentPolicy{}, fmt.Errorf("experiment thresholds must be within 0..%d basis points", MaxReleaseBasisPoints)
		}
	}
	if policy.OutcomeSignal == "" {
		if policy.MinOutcomeSamplesPerArm != 0 || policy.MaxOutcomeRateDecreaseBasisPoints != 0 {
			return ExperimentPolicy{}, errors.New("outcome thresholds require an outcome_signal")
		}
		return policy, nil
	}
	if !ValidExperimentOutcomeSignal(policy.OutcomeSignal) {
		return ExperimentPolicy{}, fmt.Errorf("unsupported experiment outcome signal %q", policy.OutcomeSignal)
	}
	if policy.MinOutcomeSamplesPerArm == 0 {
		policy.MinOutcomeSamplesPerArm = policy.MinSamplesPerArm
	}
	if policy.MaxOutcomeRateDecreaseBasisPoints == 0 {
		policy.MaxOutcomeRateDecreaseBasisPoints = DefaultExperimentOutcomeDecreaseBPS
	}
	if policy.MinOutcomeSamplesPerArm < 1 || policy.MinOutcomeSamplesPerArm > policy.TargetSamplesPerArm {
		return ExperimentPolicy{}, errors.New("min_outcome_samples_per_arm must be within 1..target_samples_per_arm")
	}
	if policy.MaxOutcomeRateDecreaseBasisPoints < 0 || policy.MaxOutcomeRateDecreaseBasisPoints > MaxReleaseBasisPoints {
		return ExperimentPolicy{}, fmt.Errorf("max_outcome_rate_decrease_basis_points must be within 0..%d", MaxReleaseBasisPoints)
	}
	return policy, nil
}

func ValidExperimentOutcomeSignal(signal string) bool {
	switch strings.TrimSpace(signal) {
	case ExperimentOutcomeSignalResponseAccepted,
		ExperimentOutcomeSignalDraftPublished,
		ExperimentOutcomeSignalContentEngaged:
		return true
	default:
		return false
	}
}

func ExperimentObservationLimit(policy ExperimentPolicy, candidateBasisPoints int) (int, error) {
	policy, err := NormalizeExperimentPolicy(policy)
	if err != nil {
		return 0, err
	}
	stableBasisPoints := MaxReleaseBasisPoints - candidateBasisPoints
	minorityBasisPoints := candidateBasisPoints
	if stableBasisPoints < minorityBasisPoints {
		minorityBasisPoints = stableBasisPoints
	}
	if minorityBasisPoints < MinExperimentArmBasisPoints {
		return 0, fmt.Errorf("each experiment arm must receive at least %d basis points", MinExperimentArmBasisPoints)
	}
	limit := policy.TargetSamplesPerArm * MaxReleaseBasisPoints / minorityBasisPoints * 2
	if limit < policy.TargetSamplesPerArm*2 {
		limit = policy.TargetSamplesPerArm * 2
	}
	if limit > MaxExperimentObservationScan {
		return 0, fmt.Errorf("experiment observation scan would exceed %d records", MaxExperimentObservationScan)
	}
	return limit, nil
}

func EvaluateExperiment(policy ExperimentPolicy, observations []ExperimentObservation) (ExperimentGateDecision, error) {
	policy, err := NormalizeExperimentPolicy(policy)
	if err != nil {
		return ExperimentGateDecision{}, err
	}
	stats, err := summarizeExperimentObservations(observations)
	if err != nil {
		return ExperimentGateDecision{}, err
	}
	decision := ExperimentGateDecision{Outcome: ExperimentDecisionContinue, Reason: "guardrails_within_limits", Stats: stats}
	if stats.Stable.Samples < policy.MinSamplesPerArm || stats.Candidate.Samples < policy.MinSamplesPerArm {
		decision.Outcome = ExperimentDecisionCollecting
		decision.Reason = "minimum_samples_not_reached"
		return decision, nil
	}
	if stats.Candidate.ErrorRateBPS > stats.Stable.ErrorRateBPS+policy.MaxErrorRateIncreaseBasisPoints {
		decision.Outcome = ExperimentDecisionRollback
		decision.Reason = "candidate_error_rate_regressed"
		return decision, nil
	}
	if exceedsRelativeIncrease(stats.Candidate.P95LatencyMS, stats.Stable.P95LatencyMS, policy.MaxP95LatencyIncreaseBasisPoints) {
		decision.Outcome = ExperimentDecisionRollback
		decision.Reason = "candidate_p95_latency_regressed"
		return decision, nil
	}
	if exceedsRelativeIncrease(stats.Candidate.AverageCostMicros, stats.Stable.AverageCostMicros, policy.MaxAverageCostIncreaseBasisPoints) {
		decision.Outcome = ExperimentDecisionRollback
		decision.Reason = "candidate_average_cost_regressed"
		return decision, nil
	}
	if policy.OutcomeSignal != "" {
		if stats.Stable.OutcomeSamples < policy.MinOutcomeSamplesPerArm || stats.Candidate.OutcomeSamples < policy.MinOutcomeSamplesPerArm {
			decision.Outcome = ExperimentDecisionCollecting
			decision.Reason = "minimum_outcome_samples_not_reached"
			return decision, nil
		}
		if stats.Candidate.OutcomeRateBPS+policy.MaxOutcomeRateDecreaseBasisPoints < stats.Stable.OutcomeRateBPS {
			decision.Outcome = ExperimentDecisionRollback
			decision.Reason = "candidate_outcome_rate_regressed"
			return decision, nil
		}
	}
	if stats.Stable.Samples >= policy.TargetSamplesPerArm && stats.Candidate.Samples >= policy.TargetSamplesPerArm {
		decision.Outcome = ExperimentDecisionPass
		decision.Reason = "target_samples_reached"
	}
	return decision, nil
}

func summarizeExperimentObservations(observations []ExperimentObservation) (ExperimentStats, error) {
	stable := make([]ExperimentObservation, 0)
	candidate := make([]ExperimentObservation, 0)
	for _, observation := range observations {
		if observation.DurationMS < 0 || observation.CostMicros < 0 {
			return ExperimentStats{}, errors.New("experiment observation duration and cost cannot be negative")
		}
		switch observation.Arm {
		case ExperimentArmStable:
			stable = append(stable, observation)
		case ExperimentArmCandidate:
			candidate = append(candidate, observation)
		default:
			return ExperimentStats{}, fmt.Errorf("unknown experiment arm %q", observation.Arm)
		}
	}
	stableStats, err := summarizeExperimentArm(stable)
	if err != nil {
		return ExperimentStats{}, fmt.Errorf("summarize stable experiment arm: %w", err)
	}
	candidateStats, err := summarizeExperimentArm(candidate)
	if err != nil {
		return ExperimentStats{}, fmt.Errorf("summarize candidate experiment arm: %w", err)
	}
	return ExperimentStats{Stable: stableStats, Candidate: candidateStats}, nil
}

func summarizeExperimentArm(observations []ExperimentObservation) (ExperimentArmStats, error) {
	stats := ExperimentArmStats{Samples: len(observations)}
	if len(observations) == 0 {
		return stats, nil
	}
	latencies := make([]int64, 0, len(observations))
	var totalCost int64
	for _, observation := range observations {
		if observation.OutcomePositive && !observation.OutcomeObserved {
			return ExperimentArmStats{}, errors.New("positive experiment outcome must be marked observed")
		}
		if observation.Success {
			stats.Successes++
		} else {
			stats.Failures++
		}
		latencies = append(latencies, observation.DurationMS)
		if observation.CostMicros > math.MaxInt64-totalCost {
			return ExperimentArmStats{}, errors.New("experiment observation cost total overflows int64")
		}
		totalCost += observation.CostMicros
		if observation.OutcomeObserved {
			stats.OutcomeSamples++
			if observation.OutcomePositive {
				stats.OutcomePositives++
			}
		}
	}
	stats.ErrorRateBPS = stats.Failures * MaxReleaseBasisPoints / stats.Samples
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	index := (len(latencies)*95 + 99) / 100
	if index < 1 {
		index = 1
	}
	stats.P95LatencyMS = latencies[index-1]
	stats.AverageCostMicros = totalCost / int64(stats.Samples)
	if stats.OutcomeSamples > 0 {
		stats.OutcomeRateBPS = stats.OutcomePositives * MaxReleaseBasisPoints / stats.OutcomeSamples
	}
	return stats, nil
}

func exceedsRelativeIncrease(candidate, stable int64, allowedBasisPoints int) bool {
	if candidate <= 0 || stable <= 0 {
		return false
	}
	leftHigh, leftLow := bits.Mul64(uint64(candidate), uint64(MaxReleaseBasisPoints))
	rightHigh, rightLow := bits.Mul64(uint64(stable), uint64(MaxReleaseBasisPoints+allowedBasisPoints))
	if leftHigh != rightHigh {
		return leftHigh > rightHigh
	}
	return leftLow > rightLow
}
