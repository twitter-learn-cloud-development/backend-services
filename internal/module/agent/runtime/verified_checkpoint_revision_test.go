package runtime

import "testing"

func TestValidateVerifiedCheckpointRejectsMissingRevision(t *testing.T) {
	checkpoint := verifiedApprovalCheckpoint("run-missing-revision", 0)
	checkpoint.Revision = 0

	if err := ValidateVerifiedCheckpoint(checkpoint); err == nil {
		t.Fatal("ValidateVerifiedCheckpoint() error = nil")
	}
}
