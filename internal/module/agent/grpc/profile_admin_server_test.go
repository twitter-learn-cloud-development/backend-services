package grpc

import (
	"context"
	"math"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/service"
)

func TestAuthorizeProfileAdministrationRequiresExactServerCredential(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	server := NewAgentServer(nil, WithProfileAdministration(&service.ProfileCatalogManager{}, token))

	tests := []struct {
		name string
		ctx  context.Context
		want codes.Code
	}{
		{name: "missing", ctx: context.Background(), want: codes.Unauthenticated},
		{name: "wrong", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs(profile.AdminTokenMetadataKey, "wrong")), want: codes.PermissionDenied},
		{name: "duplicate", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs(profile.AdminTokenMetadataKey, token, profile.AdminTokenMetadataKey, token)), want: codes.PermissionDenied},
		{name: "valid", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs(profile.AdminTokenMetadataKey, token)), want: codes.OK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := server.authorizeProfileAdministration(test.ctx)
			if got := status.Code(err); got != test.want {
				t.Fatalf("status code = %s, want %s (error=%v)", got, test.want, err)
			}
		})
	}
}

func TestAuthorizeProfileAdministrationDisabledWithoutManager(t *testing.T) {
	server := NewAgentServer(nil)
	if got := status.Code(server.authorizeProfileAdministration(context.Background())); got != codes.Unavailable {
		t.Fatalf("status code = %s, want %s", got, codes.Unavailable)
	}
}

func TestAgentProfileFromProtoRejectsDurationOverflow(t *testing.T) {
	_, err := agentProfileFromProto(&aiAgentv1.AgentProfileSpec{TimeoutMillis: math.MaxInt64/int64(time.Millisecond) + 1})
	if err == nil {
		t.Fatal("agentProfileFromProto() accepted overflowing timeout")
	}
}

func TestAgentProfileExperimentPolicyProtoMappingIncludesBusinessOutcomeGate(t *testing.T) {
	policyValue := agentProfileExperimentPolicyFromProto(&aiAgentv1.AgentProfileExperimentPolicy{
		MinSamplesPerArm: 10, TargetSamplesPerArm: 20,
		OutcomeSignal: "response_accepted", MinOutcomeSamplesPerArm: 8,
		MaxOutcomeRateDecreaseBasisPoints: 500,
	})
	if policyValue.OutcomeSignal != profile.ExperimentOutcomeSignalResponseAccepted || policyValue.MinOutcomeSamplesPerArm != 8 || policyValue.MaxOutcomeRateDecreaseBasisPoints != 500 {
		t.Fatalf("mapped policy = %+v", policyValue)
	}
}

func TestDirectProfilePublishingIsDisabledByDefault(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	accessManager := service.NewProfileAccessManager(nil, service.ProfileStaticRoleAssignments{AdminUserIDs: []uint64{42}}, false)
	server := NewAgentServer(nil,
		WithProfileAdministration(&service.ProfileCatalogManager{}, token),
		WithProfileAccessManager(accessManager),
	)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(profile.AdminTokenMetadataKey, token))
	_, err := server.PublishAgentProfileVersion(ctx, &aiAgentv1.PublishAgentProfileVersionRequest{
		ActorUserId: 42, ProfileId: "assist.custom", Version: "v1", ExpectedRevision: 1,
	})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("status code = %s, want %s (error=%v)", got, codes.PermissionDenied, err)
	}
}

func TestProfileManagementRPCRejectsActorWithoutRequiredRole(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	accessManager := service.NewProfileAccessManager(nil, service.ProfileStaticRoleAssignments{ViewerUserIDs: []uint64{7}}, false)
	server := NewAgentServer(nil,
		WithProfileAdministration(&service.ProfileCatalogManager{}, token),
		WithProfileAccessManager(accessManager),
	)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(profile.AdminTokenMetadataKey, token))
	_, err := server.CreateAgentProfileDraft(ctx, &aiAgentv1.CreateAgentProfileDraftRequest{
		ActorUserId: 7,
		Spec:        &aiAgentv1.AgentProfileSpec{ProfileId: "assist.custom", Version: "v1"},
	})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("status code = %s, want %s (error=%v)", got, codes.PermissionDenied, err)
	}
}
