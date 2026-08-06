package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"twitter-clone/internal/module/agent/profile"
)

var (
	ErrProfileExperimentNotFound            = errors.New("profile experiment not found")
	ErrProfileExperimentConflict            = errors.New("profile experiment revision conflict")
	ErrProfileExperimentAlreadyRunning      = errors.New("a profile experiment is already running")
	ErrProfileExperimentObservationLimit    = errors.New("profile experiment observation scan limit exceeded")
	ErrProfileExperimentObservationNotFound = errors.New("profile experiment run observation not found")
	ErrProfileExperimentOutcomeConflict     = errors.New("profile experiment outcome conflicts with the recorded value")
)

const maxProfileExperimentFutureSkew = 5 * time.Minute

type ProfileExperimentRecord struct {
	ID                   primitive.ObjectID       `bson:"_id,omitempty" json:"id"`
	ProfileID            string                   `bson:"profile_id" json:"profile_id"`
	StableVersion        string                   `bson:"stable_version" json:"stable_version"`
	CandidateVersion     string                   `bson:"candidate_version" json:"candidate_version"`
	CandidateBasisPoints int                      `bson:"candidate_basis_points" json:"candidate_basis_points"`
	ReleaseRevision      int64                    `bson:"release_revision" json:"release_revision"`
	ReleaseSalt          string                   `bson:"release_salt" json:"release_salt"`
	Policy               profile.ExperimentPolicy `bson:"policy" json:"policy"`
	Status               string                   `bson:"status" json:"status"`
	Decision             string                   `bson:"decision" json:"decision"`
	DecisionReason       string                   `bson:"decision_reason" json:"decision_reason"`
	Stats                profile.ExperimentStats  `bson:"stats" json:"stats"`
	Revision             int64                    `bson:"revision" json:"revision"`
	CreatedBy            uint64                   `bson:"created_by" json:"created_by"`
	UpdatedBy            uint64                   `bson:"updated_by" json:"updated_by"`
	StartedAt            time.Time                `bson:"started_at" json:"started_at"`
	CompletedAt          time.Time                `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	UpdatedAt            time.Time                `bson:"updated_at" json:"updated_at"`
}

type ProfileExperimentObservationRecord struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ExperimentID      primitive.ObjectID `bson:"experiment_id" json:"experiment_id"`
	EventID           string             `bson:"event_id" json:"event_id"`
	ProfileVersion    string             `bson:"profile_version" json:"profile_version"`
	Arm               string             `bson:"arm" json:"arm"`
	Success           bool               `bson:"success" json:"success"`
	DurationMS        int64              `bson:"duration_ms" json:"duration_ms"`
	CostMicros        int64              `bson:"cost_micros" json:"cost_micros"`
	OccurredAt        time.Time          `bson:"occurred_at" json:"occurred_at"`
	OutcomeObserved   bool               `bson:"outcome_observed,omitempty" json:"outcome_observed,omitempty"`
	OutcomeSignal     string             `bson:"outcome_signal,omitempty" json:"outcome_signal,omitempty"`
	OutcomePositive   bool               `bson:"outcome_positive,omitempty" json:"outcome_positive,omitempty"`
	OutcomeRecordedBy uint64             `bson:"outcome_recorded_by,omitempty" json:"outcome_recorded_by,omitempty"`
	OutcomeRecordedAt time.Time          `bson:"outcome_recorded_at,omitempty" json:"outcome_recorded_at,omitempty"`
}

type ProfileExperimentRepository interface {
	CreateProfileExperiment(ctx context.Context, experiment *ProfileExperimentRecord) error
	GetProfileExperiment(ctx context.Context, experimentID primitive.ObjectID) (*ProfileExperimentRecord, error)
	GetRunningProfileExperiment(ctx context.Context, profileID string) (*ProfileExperimentRecord, error)
	ListProfileExperiments(ctx context.Context, profileID, status string, page, pageSize int) ([]*ProfileExperimentRecord, int64, error)
	AppendProfileExperimentObservation(ctx context.Context, observation *ProfileExperimentObservationRecord) (bool, error)
	RecordProfileExperimentOutcome(ctx context.Context, experimentID primitive.ObjectID, eventID, signal string, positive bool, recordedBy uint64) (*ProfileExperimentObservationRecord, bool, error)
	ListProfileExperimentObservations(ctx context.Context, experimentID primitive.ObjectID, limit int) ([]*ProfileExperimentObservationRecord, error)
	UpdateProfileExperimentDecision(ctx context.Context, experimentID primitive.ObjectID, expectedRevision int64, status, decision, reason string, stats profile.ExperimentStats, updatedBy uint64) error
}

func (r *MongoProfileRepository) ensureProfileExperimentIndexes(ctx context.Context) error {
	if err := r.requireProfileRepository(ctx, r.experimentColl, r.experimentRunColl); err != nil {
		return err
	}
	experimentIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "profile_id", Value: 1}},
			Options: options.Index().SetName("uniq_running_profile_experiment").SetUnique(true).
				SetPartialFilterExpression(bson.M{"status": profile.ExperimentStatusRunning}),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "updated_at", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("idx_profile_experiment_status_updated"),
		},
	}
	if _, err := r.experimentColl.Indexes().CreateMany(ctx, experimentIndexes); err != nil {
		return fmt.Errorf("create profile experiment indexes failed: %w", err)
	}
	observationIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "experiment_id", Value: 1}, {Key: "event_id", Value: 1}},
			Options: options.Index().SetName("uniq_profile_experiment_event").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "experiment_id", Value: 1}, {Key: "occurred_at", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetName("idx_profile_experiment_observation_time"),
		},
	}
	if _, err := r.experimentRunColl.Indexes().CreateMany(ctx, observationIndexes); err != nil {
		return fmt.Errorf("create profile experiment observation indexes failed: %w", err)
	}
	return nil
}

func (r *MongoProfileRepository) CreateProfileExperiment(ctx context.Context, experiment *ProfileExperimentRecord) error {
	if err := r.requireProfileRepository(ctx, r.experimentColl); err != nil {
		return err
	}
	if err := prepareProfileExperiment(experiment, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := r.experimentColl.InsertOne(ctx, experiment); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrProfileExperimentAlreadyRunning
		}
		return fmt.Errorf("insert profile experiment failed: %w", err)
	}
	return nil
}

func (r *MongoProfileRepository) GetProfileExperiment(ctx context.Context, experimentID primitive.ObjectID) (*ProfileExperimentRecord, error) {
	if err := r.requireProfileRepository(ctx, r.experimentColl); err != nil {
		return nil, err
	}
	if experimentID.IsZero() {
		return nil, errors.New("profile experiment id is required")
	}
	var record ProfileExperimentRecord
	if err := r.experimentColl.FindOne(ctx, bson.M{"_id": experimentID}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProfileExperimentNotFound
		}
		return nil, fmt.Errorf("find profile experiment failed: %w", err)
	}
	return &record, nil
}

func (r *MongoProfileRepository) GetRunningProfileExperiment(ctx context.Context, profileID string) (*ProfileExperimentRecord, error) {
	if err := r.requireProfileRepository(ctx, r.experimentColl); err != nil {
		return nil, err
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" || len(profileID) > maxProfileIdentityLength {
		return nil, errors.New("profile id is required")
	}
	var record ProfileExperimentRecord
	if err := r.experimentColl.FindOne(ctx, bson.M{"profile_id": profileID, "status": profile.ExperimentStatusRunning}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProfileExperimentNotFound
		}
		return nil, fmt.Errorf("find running profile experiment failed: %w", err)
	}
	return &record, nil
}

func (r *MongoProfileRepository) ListProfileExperiments(ctx context.Context, profileID, status string, page, pageSize int) ([]*ProfileExperimentRecord, int64, error) {
	if err := r.requireProfileRepository(ctx, r.experimentColl); err != nil {
		return nil, 0, err
	}
	_, pageSize, skip, err := normalizeProfilePagination(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	filter := bson.M{}
	if profileID = strings.TrimSpace(profileID); profileID != "" {
		if len(profileID) > maxProfileIdentityLength {
			return nil, 0, errors.New("profile id is too long")
		}
		filter["profile_id"] = profileID
	}
	if status = strings.TrimSpace(status); status != "" {
		if !validProfileExperimentStatus(status) {
			return nil, 0, errors.New("profile experiment status is invalid")
		}
		filter["status"] = status
	}
	sortOrder := -1
	if status == profile.ExperimentStatusRunning {
		sortOrder = 1
	}
	total, err := r.experimentColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count profile experiments failed: %w", err)
	}
	cursor, err := r.experimentColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: sortOrder}, {Key: "_id", Value: sortOrder}}).
		SetSkip(skip).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find profile experiments failed: %w", err)
	}
	defer cursor.Close(ctx)
	var records []*ProfileExperimentRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, 0, fmt.Errorf("decode profile experiments failed: %w", err)
	}
	return records, total, nil
}

func (r *MongoProfileRepository) AppendProfileExperimentObservation(ctx context.Context, observation *ProfileExperimentObservationRecord) (bool, error) {
	if err := r.requireProfileRepository(ctx, r.experimentRunColl); err != nil {
		return false, err
	}
	if err := prepareProfileExperimentObservation(observation, time.Now().UTC()); err != nil {
		return false, err
	}
	if _, err := r.experimentRunColl.InsertOne(ctx, observation); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, fmt.Errorf("insert profile experiment observation failed: %w", err)
	}
	return true, nil
}

func (r *MongoProfileRepository) RecordProfileExperimentOutcome(
	ctx context.Context,
	experimentID primitive.ObjectID,
	eventID, signal string,
	positive bool,
	recordedBy uint64,
) (*ProfileExperimentObservationRecord, bool, error) {
	if err := r.requireProfileRepository(ctx, r.experimentRunColl); err != nil {
		return nil, false, err
	}
	eventID, signal, err := prepareProfileExperimentOutcome(experimentID, eventID, signal, recordedBy)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	result, err := r.experimentRunColl.UpdateOne(ctx, bson.M{
		"experiment_id":    experimentID,
		"event_id":         eventID,
		"outcome_observed": bson.M{"$ne": true},
	}, bson.M{"$set": bson.M{
		"outcome_observed": true, "outcome_signal": signal, "outcome_positive": positive,
		"outcome_recorded_by": recordedBy, "outcome_recorded_at": now,
	}})
	if err != nil {
		return nil, false, fmt.Errorf("record profile experiment outcome failed: %w", err)
	}
	var observation ProfileExperimentObservationRecord
	if err := r.experimentRunColl.FindOne(ctx, bson.M{"experiment_id": experimentID, "event_id": eventID}).Decode(&observation); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, false, ErrProfileExperimentObservationNotFound
		}
		return nil, false, fmt.Errorf("read profile experiment outcome observation failed: %w", err)
	}
	if result.MatchedCount == 1 {
		return &observation, false, nil
	}
	if observation.OutcomeObserved && observation.OutcomeSignal == signal && observation.OutcomePositive == positive {
		return &observation, true, nil
	}
	return nil, false, ErrProfileExperimentOutcomeConflict
}

func (r *MongoProfileRepository) ListProfileExperimentObservations(ctx context.Context, experimentID primitive.ObjectID, limit int) ([]*ProfileExperimentObservationRecord, error) {
	if err := r.requireProfileRepository(ctx, r.experimentRunColl); err != nil {
		return nil, err
	}
	if experimentID.IsZero() || limit < 1 || limit > profile.MaxExperimentObservationScan {
		return nil, errors.New("profile experiment id and valid observation limit are required")
	}
	cursor, err := r.experimentRunColl.Find(ctx, bson.M{"experiment_id": experimentID}, options.Find().
		SetSort(bson.D{{Key: "occurred_at", Value: 1}, {Key: "_id", Value: 1}}).
		SetLimit(int64(limit+1)))
	if err != nil {
		return nil, fmt.Errorf("find profile experiment observations failed: %w", err)
	}
	defer cursor.Close(ctx)
	var records []*ProfileExperimentObservationRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, fmt.Errorf("decode profile experiment observations failed: %w", err)
	}
	if len(records) > limit {
		return nil, ErrProfileExperimentObservationLimit
	}
	return records, nil
}

func (r *MongoProfileRepository) UpdateProfileExperimentDecision(ctx context.Context, experimentID primitive.ObjectID, expectedRevision int64, status, decision, reason string, stats profile.ExperimentStats, updatedBy uint64) error {
	if err := r.requireProfileRepository(ctx, r.experimentColl); err != nil {
		return err
	}
	if experimentID.IsZero() || expectedRevision < 1 || updatedBy == 0 {
		return errors.New("profile experiment identity, revision and updater are required")
	}
	status = strings.TrimSpace(status)
	decision = strings.TrimSpace(decision)
	reason = strings.TrimSpace(reason)
	if status == "" || decision == "" || reason == "" || len(reason) > 128 {
		return errors.New("profile experiment status, decision and bounded reason are required")
	}
	if !validProfileExperimentTransition(status, decision) {
		return errors.New("profile experiment status and decision are inconsistent")
	}
	if err := validateProfileExperimentStats(stats); err != nil {
		return err
	}
	now := time.Now().UTC()
	set := bson.M{
		"status": status, "decision": decision, "decision_reason": reason,
		"stats": stats, "updated_by": updatedBy, "updated_at": now,
	}
	if status != profile.ExperimentStatusRunning {
		set["completed_at"] = now
	}
	result, err := r.experimentColl.UpdateOne(ctx, bson.M{
		"_id": experimentID, "revision": expectedRevision, "status": profile.ExperimentStatusRunning,
	}, bson.M{"$set": set, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return fmt.Errorf("update profile experiment decision failed: %w", err)
	}
	if result.MatchedCount == 0 {
		count, countErr := r.experimentColl.CountDocuments(ctx, bson.M{"_id": experimentID})
		if countErr != nil {
			return fmt.Errorf("verify profile experiment mutation failed: %w", countErr)
		}
		if count == 0 {
			return ErrProfileExperimentNotFound
		}
		return ErrProfileExperimentConflict
	}
	return nil
}

func prepareProfileExperiment(experiment *ProfileExperimentRecord, now time.Time) error {
	if experiment == nil {
		return errors.New("profile experiment is required")
	}
	experiment.ProfileID = strings.TrimSpace(experiment.ProfileID)
	experiment.StableVersion = strings.TrimSpace(experiment.StableVersion)
	experiment.CandidateVersion = strings.TrimSpace(experiment.CandidateVersion)
	experiment.ReleaseSalt = strings.TrimSpace(experiment.ReleaseSalt)
	policy, err := profile.NormalizeExperimentPolicy(experiment.Policy)
	if err != nil {
		return err
	}
	if experiment.ProfileID == "" || experiment.StableVersion == "" || experiment.CandidateVersion == "" ||
		len(experiment.ProfileID) > maxProfileIdentityLength || len(experiment.StableVersion) > maxProfileIdentityLength ||
		len(experiment.CandidateVersion) > maxProfileIdentityLength || len(experiment.ReleaseSalt) > maxProfileReleaseSalt ||
		experiment.ReleaseRevision < 1 || experiment.CreatedBy == 0 {
		return errors.New("profile experiment identity, release revision and creator are required")
	}
	if experiment.StableVersion == experiment.CandidateVersion {
		return errors.New("stable and candidate profile experiment versions must differ")
	}
	if _, err := profile.ExperimentObservationLimit(policy, experiment.CandidateBasisPoints); err != nil {
		return err
	}
	if experiment.ID.IsZero() {
		experiment.ID = primitive.NewObjectID()
	}
	experiment.Policy = policy
	experiment.Status = profile.ExperimentStatusRunning
	experiment.Decision = profile.ExperimentDecisionCollecting
	experiment.DecisionReason = "minimum_samples_not_reached"
	experiment.Stats = profile.ExperimentStats{}
	experiment.Revision = 1
	experiment.UpdatedBy = experiment.CreatedBy
	now = now.UTC()
	experiment.StartedAt = now
	experiment.UpdatedAt = now
	experiment.CompletedAt = time.Time{}
	return nil
}

func prepareProfileExperimentObservation(observation *ProfileExperimentObservationRecord, now time.Time) error {
	if observation == nil || observation.ExperimentID.IsZero() {
		return errors.New("profile experiment observation identity is required")
	}
	observation.EventID = strings.TrimSpace(observation.EventID)
	observation.ProfileVersion = strings.TrimSpace(observation.ProfileVersion)
	observation.Arm = strings.TrimSpace(observation.Arm)
	if observation.EventID == "" || observation.ProfileVersion == "" || len(observation.EventID) > maxProfileIdentityLength ||
		len(observation.ProfileVersion) > maxProfileIdentityLength ||
		(observation.Arm != profile.ExperimentArmStable && observation.Arm != profile.ExperimentArmCandidate) {
		return errors.New("profile experiment observation event, version and arm are required")
	}
	if observation.DurationMS < 0 || observation.CostMicros < 0 {
		return errors.New("profile experiment observation duration and cost cannot be negative")
	}
	if observation.OutcomeObserved || observation.OutcomeSignal != "" || observation.OutcomePositive ||
		observation.OutcomeRecordedBy != 0 || !observation.OutcomeRecordedAt.IsZero() {
		return errors.New("profile experiment observation outcome must be recorded separately")
	}
	if observation.ID.IsZero() {
		observation.ID = primitive.NewObjectID()
	}
	now = now.UTC()
	if observation.OccurredAt.IsZero() {
		observation.OccurredAt = now
	} else {
		observation.OccurredAt = observation.OccurredAt.UTC()
		if observation.OccurredAt.After(now.Add(maxProfileExperimentFutureSkew)) {
			return errors.New("profile experiment observation occurrence time is too far in the future")
		}
	}
	return nil
}

func prepareProfileExperimentOutcome(experimentID primitive.ObjectID, eventID, signal string, recordedBy uint64) (string, string, error) {
	eventID = strings.TrimSpace(eventID)
	signal = strings.TrimSpace(signal)
	if experimentID.IsZero() || eventID == "" || len(eventID) > 128 || recordedBy == 0 {
		return "", "", errors.New("experiment, bounded event id and recorder are required")
	}
	if !profile.ValidExperimentOutcomeSignal(signal) {
		return "", "", errors.New("profile experiment outcome signal is invalid")
	}
	return eventID, signal, nil
}

func validProfileExperimentStatus(status string) bool {
	switch status {
	case profile.ExperimentStatusRunning,
		profile.ExperimentStatusPassed,
		profile.ExperimentStatusRolledBack,
		profile.ExperimentStatusStopped,
		profile.ExperimentStatusSuperseded:
		return true
	default:
		return false
	}
}

func validProfileExperimentTransition(status, decision string) bool {
	switch status {
	case profile.ExperimentStatusRunning:
		return decision == profile.ExperimentDecisionCollecting || decision == profile.ExperimentDecisionContinue
	case profile.ExperimentStatusPassed:
		return decision == profile.ExperimentDecisionPass
	case profile.ExperimentStatusRolledBack:
		return decision == profile.ExperimentDecisionRollback
	case profile.ExperimentStatusStopped:
		return decision == profile.ExperimentDecisionStop
	case profile.ExperimentStatusSuperseded:
		return decision == profile.ExperimentDecisionSuperseded
	default:
		return false
	}
}

func validateProfileExperimentStats(stats profile.ExperimentStats) error {
	for _, arm := range []profile.ExperimentArmStats{stats.Stable, stats.Candidate} {
		if arm.Samples < 0 || arm.Successes < 0 || arm.Failures < 0 ||
			arm.Successes > arm.Samples || arm.Failures > arm.Samples || arm.Successes+arm.Failures != arm.Samples ||
			arm.ErrorRateBPS < 0 || arm.ErrorRateBPS > profile.MaxReleaseBasisPoints ||
			arm.P95LatencyMS < 0 || arm.AverageCostMicros < 0 ||
			arm.OutcomeSamples < 0 || arm.OutcomeSamples > arm.Samples ||
			arm.OutcomePositives < 0 || arm.OutcomePositives > arm.OutcomeSamples ||
			arm.OutcomeRateBPS < 0 || arm.OutcomeRateBPS > profile.MaxReleaseBasisPoints {
			return errors.New("profile experiment stats are invalid")
		}
	}
	return nil
}
