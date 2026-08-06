package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/module/agent/profile"
)

func TestCreateAgentProfileDraftUsesJWTActorAndInternalCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "0123456789abcdef0123456789abcdef"
	client := &profileAdminClientFake{}
	handler := NewAgentHandler(client, WithProfileAdministration(token, []uint64{42}))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/profile-catalog/versions", strings.NewReader(`{
		"profile_id":"assist.custom","version":"v1","prompt_id":"assist.custom.system",
		"prompt_version":"v1","system_prompt":"Be useful.","max_steps":4,
		"max_input_tokens":1000,"max_output_tokens":500,"max_total_tokens":1500,
		"max_estimated_cost_micros":10000,"timeout_millis":30000,"allowed_tools":["WebSearch"]
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.CreateAgentProfileDraft(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if client.request == nil || client.request.ActorUserId != 42 {
		t.Fatalf("gRPC request = %+v", client.request)
	}
	outgoingMetadata, _ := metadata.FromOutgoingContext(client.ctx)
	values := outgoingMetadata.Get(profile.AdminTokenMetadataKey)
	if len(values) != 1 || values[0] != token {
		t.Fatalf("profile admin metadata = %v", values)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestCreateAgentProfileDraftRejectsNonAdministratorBeforeGRPC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &profileAdminClientFake{}
	handler := NewAgentHandler(client, WithProfileAdministration("0123456789abcdef0123456789abcdef", []uint64{42}))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/profile-catalog/versions", nil)
	ctx.Set("user_id", uint64(7))

	handler.CreateAgentProfileDraft(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if client.request != nil {
		t.Fatalf("forbidden request reached gRPC: %+v", client.request)
	}
}

func TestProfileManagementRolesKeepEditorAndApproverSeparate(t *testing.T) {
	handler := NewAgentHandler(nil, WithProfileManagementRoles(
		"0123456789abcdef0123456789abcdef",
		[]uint64{7}, []uint64{42}, []uint64{43}, []uint64{44}, false,
	))
	if !handler.hasProfileRole(42, ProfileRoleEditor) || handler.hasProfileRole(42, ProfileRoleApprover) {
		t.Fatal("editor unexpectedly inherited approver role")
	}
	if !handler.hasProfileRole(43, ProfileRoleApprover) || handler.hasProfileRole(43, ProfileRoleEditor) {
		t.Fatal("approver unexpectedly inherited editor role")
	}
	if !handler.hasProfileRole(44, ProfileRoleEditor) || !handler.hasProfileRole(44, ProfileRoleApprover) || !handler.hasProfileRole(44, ProfileRoleAdmin) {
		t.Fatal("administrator did not inherit all management capabilities")
	}
	if !handler.hasProfileRole(7, ProfileRoleViewer) || handler.hasProfileRole(7, ProfileRoleEditor) {
		t.Fatal("viewer authorization is invalid")
	}
}

func TestCreateAgentProfileDraftAcceptsDynamicEditorRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "0123456789abcdef0123456789abcdef"
	client := &profileAdminClientFake{accessImplemented: true, accessRoles: []string{"editor"}}
	handler := NewAgentHandler(client, WithProfileManagementRoles(token, nil, nil, nil, []uint64{1}, false))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/profile-catalog/versions", strings.NewReader(`{
		"profile_id":"assist.dynamic","version":"v1","prompt_id":"assist.dynamic.system",
		"prompt_version":"v1","system_prompt":"Be useful.","max_steps":4,
		"max_input_tokens":1000,"max_output_tokens":500,"max_total_tokens":1500,
		"timeout_millis":30000,"allowed_tools":[]
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.CreateAgentProfileDraft(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestStartAgentProfileExperimentUsesJWTActorAndCASRevision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "0123456789abcdef0123456789abcdef"
	client := &profileAdminClientFake{}
	handler := NewAgentHandler(client, WithProfileAdministration(token, []uint64{42}))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/profile-catalog/experiments", strings.NewReader(`{
		"profile_id":"assist.custom","expected_release_revision":7,"policy":{
			"min_samples_per_arm":20,"target_samples_per_arm":80,
			"max_error_rate_increase_basis_points":500,
			"max_p95_latency_increase_basis_points":1500,
			"max_average_cost_increase_basis_points":1000,
			"outcome_signal":"draft_published",
			"min_outcome_samples_per_arm":10,
			"max_outcome_rate_decrease_basis_points":750
		}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.StartAgentProfileExperiment(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if client.experimentRequest == nil || client.experimentRequest.ActorUserId != 42 || client.experimentRequest.ExpectedReleaseRevision != 7 {
		t.Fatalf("gRPC experiment request = %+v", client.experimentRequest)
	}
	if client.experimentRequest.Policy == nil || client.experimentRequest.Policy.TargetSamplesPerArm != 80 {
		t.Fatalf("experiment policy = %+v", client.experimentRequest.Policy)
	}
	if client.experimentRequest.Policy.OutcomeSignal != "draft_published" || client.experimentRequest.Policy.MinOutcomeSamplesPerArm != 10 {
		t.Fatalf("experiment outcome policy = %+v", client.experimentRequest.Policy)
	}
	outgoingMetadata, _ := metadata.FromOutgoingContext(client.experimentContext)
	if values := outgoingMetadata.Get(profile.AdminTokenMetadataKey); len(values) != 1 || values[0] != token {
		t.Fatalf("profile admin metadata = %v", values)
	}
}

func TestRecordAgentProfileExperimentOutcomeUsesAdminActorAndForwardsNegativeValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "0123456789abcdef0123456789abcdef"
	client := &profileAdminClientFake{}
	handler := NewAgentHandler(client, WithProfileAdministration(token, []uint64{42}))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "experiment_id", Value: "507f1f77bcf86cd799439011"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/profile-catalog/experiments/507f1f77bcf86cd799439011/outcomes", strings.NewReader(`{
		"event_id":"run-42","signal":"draft_published","positive":false
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.RecordAgentProfileExperimentOutcome(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	request := client.outcomeRequest
	if request == nil || request.ActorUserId != 42 || request.EventId != "run-42" || request.Signal != "draft_published" || request.Positive {
		t.Fatalf("gRPC outcome request = %+v", request)
	}
	outgoingMetadata, _ := metadata.FromOutgoingContext(client.outcomeContext)
	if values := outgoingMetadata.Get(profile.AdminTokenMetadataKey); len(values) != 1 || values[0] != token {
		t.Fatalf("profile admin metadata = %v", values)
	}
}

func TestRequestAgentProfilePublishApprovalForwardsQualityEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "0123456789abcdef0123456789abcdef"
	client := &profileAdminClientFake{}
	handler := NewAgentHandler(client, WithProfileAdministration(token, []uint64{42}))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "profile_id", Value: "assist.writer"}, {Key: "version", Value: "v2"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/profile-catalog/versions/assist.writer/v2/publish-requests", strings.NewReader(`{
		"expected_version_revision":3,
		"quality_evidence":{
			"storage":"minio","bucket":"agent-eval","key":"agent-task-eval/a/report.json",
			"version_id":"version-1","report_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"length":1024,"content_type":"application/json","retention_mode":"COMPLIANCE",
			"retain_until":1780000000000,"archived_at":1770000000000,"dataset_version":"dataset-v1",
			"dataset_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"execution_config_sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"integrity_key_id":"eval-key-v1"
		}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.RequestAgentProfilePublishApproval(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	request := client.publishApprovalRequest
	if request == nil || request.ActorUserId != 42 || request.ExpectedVersionRevision != 3 || request.QualityEvidence == nil {
		t.Fatalf("gRPC publish approval request = %+v", request)
	}
	if request.QualityEvidence.VersionId != "version-1" || request.QualityEvidence.IntegrityKeyId != "eval-key-v1" {
		t.Fatalf("gRPC quality evidence = %+v", request.QualityEvidence)
	}
}

type profileAdminClientFake struct {
	aiAgentv1.AiAgentServiceClient
	ctx                    context.Context
	request                *aiAgentv1.CreateAgentProfileDraftRequest
	accessImplemented      bool
	accessRoles            []string
	experimentContext      context.Context
	experimentRequest      *aiAgentv1.StartAgentProfileExperimentRequest
	outcomeContext         context.Context
	outcomeRequest         *aiAgentv1.RecordAgentProfileExperimentOutcomeRequest
	publishApprovalRequest *aiAgentv1.RequestAgentProfilePublishApprovalRequest
}

func (f *profileAdminClientFake) RequestAgentProfilePublishApproval(
	_ context.Context,
	req *aiAgentv1.RequestAgentProfilePublishApprovalRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.RequestAgentProfilePublishApprovalResponse, error) {
	f.publishApprovalRequest = req
	return &aiAgentv1.RequestAgentProfilePublishApprovalResponse{
		Code: 200, Msg: "success",
		Approval: &aiAgentv1.AgentProfilePublishApproval{
			ApprovalId: "507f1f77bcf86cd799439011", ProfileId: req.ProfileId, Version: req.Version,
			ExpectedVersionRevision: req.ExpectedVersionRevision, Status: "pending", Revision: 1,
		},
	}, nil
}

func (f *profileAdminClientFake) StartAgentProfileExperiment(
	ctx context.Context,
	req *aiAgentv1.StartAgentProfileExperimentRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.StartAgentProfileExperimentResponse, error) {
	f.experimentContext = ctx
	f.experimentRequest = req
	return &aiAgentv1.StartAgentProfileExperimentResponse{
		Code: 200, Msg: "success",
		Experiment: &aiAgentv1.AgentProfileExperiment{
			ExperimentId: "507f1f77bcf86cd799439011", ProfileId: req.ProfileId,
			Policy: req.Policy, Status: "running", Decision: "collecting", Revision: 1, CreatedBy: req.ActorUserId,
		},
	}, nil
}

func (f *profileAdminClientFake) RecordAgentProfileExperimentOutcome(
	ctx context.Context,
	req *aiAgentv1.RecordAgentProfileExperimentOutcomeRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.RecordAgentProfileExperimentOutcomeResponse, error) {
	f.outcomeContext = ctx
	f.outcomeRequest = req
	return &aiAgentv1.RecordAgentProfileExperimentOutcomeResponse{
		Code: 200, Msg: "success", IdempotentReplay: false,
	}, nil
}

func (f *profileAdminClientFake) GetAgentProfileManagementAccess(
	_ context.Context,
	_ *aiAgentv1.GetAgentProfileManagementAccessRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetAgentProfileManagementAccessResponse, error) {
	if !f.accessImplemented {
		return nil, status.Error(codes.Unimplemented, "legacy Agent Service")
	}
	return &aiAgentv1.GetAgentProfileManagementAccessResponse{
		Code: 200, Msg: "success",
		Access: &aiAgentv1.AgentProfileManagementAccess{Roles: append([]string(nil), f.accessRoles...), DynamicRbacEnabled: true},
	}, nil
}

func (f *profileAdminClientFake) CreateAgentProfileDraft(
	ctx context.Context,
	req *aiAgentv1.CreateAgentProfileDraftRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.CreateAgentProfileDraftResponse, error) {
	f.ctx = ctx
	f.request = req
	return &aiAgentv1.CreateAgentProfileDraftResponse{
		Code: 200,
		Msg:  "success",
		ProfileVersion: &aiAgentv1.AgentProfileVersion{
			Id: "version-id", Spec: req.Spec, Status: "draft", Revision: 1, CreatedBy: req.ActorUserId,
		},
	}, nil
}
