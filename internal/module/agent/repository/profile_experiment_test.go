package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/profile"
)

func TestPrepareProfileExperimentNormalizesDefaults(t *testing.T) {
	now := time.Now()
	record := &ProfileExperimentRecord{
		ProfileID: " assist.draft ", StableVersion: " v1 ", CandidateVersion: " v2 ",
		CandidateBasisPoints: 1_000, ReleaseRevision: 3, CreatedBy: 7,
	}
	require.NoError(t, prepareProfileExperiment(record, now))
	require.Equal(t, profile.ExperimentStatusRunning, record.Status)
	require.Equal(t, profile.DefaultExperimentTargetSamplesPerArm, record.Policy.TargetSamplesPerArm)
	require.Equal(t, int64(1), record.Revision)
	require.Equal(t, "assist.draft", record.ProfileID)
}

func TestPrepareProfileExperimentObservationRejectsUnknownArm(t *testing.T) {
	err := prepareProfileExperimentObservation(&ProfileExperimentObservationRecord{
		ExperimentID: primitive.NewObjectID(), EventID: "run-1", ProfileVersion: "v2", Arm: "other",
	}, time.Now())
	require.Error(t, err)
}

func TestPrepareProfileExperimentOutcomeValidatesBoundedIdentityAndSignal(t *testing.T) {
	experimentID := primitive.NewObjectID()
	eventID, signal, err := prepareProfileExperimentOutcome(
		experimentID, " run-1 ", profile.ExperimentOutcomeSignalResponseAccepted, 7,
	)
	require.NoError(t, err)
	require.Equal(t, "run-1", eventID)
	require.Equal(t, profile.ExperimentOutcomeSignalResponseAccepted, signal)

	_, _, err = prepareProfileExperimentOutcome(experimentID, "run-1", "dynamic", 7)
	require.Error(t, err)
	_, _, err = prepareProfileExperimentOutcome(experimentID, "run-1", signal, 0)
	require.Error(t, err)
}
