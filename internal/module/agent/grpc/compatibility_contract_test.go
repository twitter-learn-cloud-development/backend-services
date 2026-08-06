package grpc

import (
	"testing"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestAgentGRPCMethodCompatibilityContract(t *testing.T) {
	wantFullMethods := map[string]string{
		"unified":                     aiAgentv1.AiAgentService_RunAgent_FullMethodName,
		"skill_list":                  aiAgentv1.AiAgentService_ListAgentSkills_FullMethodName,
		"skill_get":                   aiAgentv1.AiAgentService_GetAgentSkill_FullMethodName,
		"extension_list":              aiAgentv1.AiAgentService_ListAgentExtensions_FullMethodName,
		"marketplace_list":            aiAgentv1.AiAgentService_ListAgentMarketplaceExtensions_FullMethodName,
		"marketplace_access":          aiAgentv1.AiAgentService_GetAgentMarketplaceManagementAccess_FullMethodName,
		"marketplace_publish":         aiAgentv1.AiAgentService_PublishAgentMarketplaceRelease_FullMethodName,
		"marketplace_withdraw":        aiAgentv1.AiAgentService_WithdrawAgentMarketplaceRelease_FullMethodName,
		"chat":                        aiAgentv1.AiAgentService_CallApiOfAi_FullMethodName,
		"consult":                     aiAgentv1.AiAgentService_ConsultContent_FullMethodName,
		"assist":                      aiAgentv1.AiAgentService_AssistPublishTwitter_FullMethodName,
		"multi":                       aiAgentv1.AiAgentService_MultiAgentPublishTwitter_FullMethodName,
		"workflow":                    aiAgentv1.AiAgentService_RunWorkflow_FullMethodName,
		"workflow_tool_publish":       aiAgentv1.AiAgentService_PublishWorkflowTool_FullMethodName,
		"workflow_tool_get":           aiAgentv1.AiAgentService_GetWorkflowToolPublication_FullMethodName,
		"workflow_tool_unpublish":     aiAgentv1.AiAgentService_UnpublishWorkflowTool_FullMethodName,
		"workflow_revisions":          aiAgentv1.AiAgentService_ListWorkflowRevisions_FullMethodName,
		"workflow_replay":             aiAgentv1.AiAgentService_GetWorkflowRunReplay_FullMethodName,
		"workflow_blackboard":         aiAgentv1.AiAgentService_SearchWorkflowBlackboard_FullMethodName,
		"resume_grant":                aiAgentv1.AiAgentService_IssueWorkflowResumeGrant_FullMethodName,
		"agent_resume_grant":          aiAgentv1.AiAgentService_IssueAgentResumeGrant_FullMethodName,
		"provider_config":             aiAgentv1.AiAgentService_CreateProviderConfig_FullMethodName,
		"session_end":                 aiAgentv1.AiAgentService_EndDialogueSession_FullMethodName,
		"profile_draft":               aiAgentv1.AiAgentService_CreateAgentProfileDraft_FullMethodName,
		"profile_publish":             aiAgentv1.AiAgentService_PublishAgentProfileVersion_FullMethodName,
		"profile_audits":              aiAgentv1.AiAgentService_ListAgentProfileAuditEvents_FullMethodName,
		"profile_experiment_start":    aiAgentv1.AiAgentService_StartAgentProfileExperiment_FullMethodName,
		"profile_experiment_evaluate": aiAgentv1.AiAgentService_EvaluateAgentProfileExperiment_FullMethodName,
	}
	want := map[string]string{
		"unified":                     "/aiAgent.v1.AiAgentService/runAgent",
		"skill_list":                  "/aiAgent.v1.AiAgentService/listAgentSkills",
		"skill_get":                   "/aiAgent.v1.AiAgentService/getAgentSkill",
		"extension_list":              "/aiAgent.v1.AiAgentService/listAgentExtensions",
		"marketplace_list":            "/aiAgent.v1.AiAgentService/listAgentMarketplaceExtensions",
		"marketplace_access":          "/aiAgent.v1.AiAgentService/getAgentMarketplaceManagementAccess",
		"marketplace_publish":         "/aiAgent.v1.AiAgentService/publishAgentMarketplaceRelease",
		"marketplace_withdraw":        "/aiAgent.v1.AiAgentService/withdrawAgentMarketplaceRelease",
		"chat":                        "/aiAgent.v1.AiAgentService/callApiOfAi",
		"consult":                     "/aiAgent.v1.AiAgentService/consultContent",
		"assist":                      "/aiAgent.v1.AiAgentService/assistPublishTwitter",
		"multi":                       "/aiAgent.v1.AiAgentService/multiAgentPublishTwitter",
		"workflow":                    "/aiAgent.v1.AiAgentService/runWorkflow",
		"workflow_tool_publish":       "/aiAgent.v1.AiAgentService/publishWorkflowTool",
		"workflow_tool_get":           "/aiAgent.v1.AiAgentService/getWorkflowToolPublication",
		"workflow_tool_unpublish":     "/aiAgent.v1.AiAgentService/unpublishWorkflowTool",
		"workflow_revisions":          "/aiAgent.v1.AiAgentService/listWorkflowRevisions",
		"workflow_replay":             "/aiAgent.v1.AiAgentService/getWorkflowRunReplay",
		"workflow_blackboard":         "/aiAgent.v1.AiAgentService/searchWorkflowBlackboard",
		"resume_grant":                "/aiAgent.v1.AiAgentService/issueWorkflowResumeGrant",
		"agent_resume_grant":          "/aiAgent.v1.AiAgentService/issueAgentResumeGrant",
		"provider_config":             "/aiAgent.v1.AiAgentService/createProviderConfig",
		"session_end":                 "/aiAgent.v1.AiAgentService/endDialogueSession",
		"profile_draft":               "/aiAgent.v1.AiAgentService/createAgentProfileDraft",
		"profile_publish":             "/aiAgent.v1.AiAgentService/publishAgentProfileVersion",
		"profile_audits":              "/aiAgent.v1.AiAgentService/listAgentProfileAuditEvents",
		"profile_experiment_start":    "/aiAgent.v1.AiAgentService/startAgentProfileExperiment",
		"profile_experiment_evaluate": "/aiAgent.v1.AiAgentService/evaluateAgentProfileExperiment",
	}
	for mode, fullMethod := range wantFullMethods {
		if fullMethod != want[mode] {
			t.Fatalf("%s full method = %q, want %q", mode, fullMethod, want[mode])
		}
	}
}

func TestAgentProtoFieldCompatibilityContract(t *testing.T) {
	file := aiAgentv1.File_api_aiAgent_v1_aiAgent_proto
	assertProtoFields(t, file.Messages().ByName("MainContent"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id":      2,
		"dialogue_id":  3,
		"content":      4,
		"dialogue_key": 5,
	})
	assertProtoFields(t, file.Messages().ByName("callApiOfAiResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"response":     3,
		"dialogue_key": 4,
	})
	assertProtoFields(t, file.Messages().ByName("RunAgentRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "model_kind_id": 2, "main_content": 3, "preferred_capability_ids": 4,
		"web_search_provider_config_id": 5, "skill_id": 6, "skill_version": 7,
	})
	assertProtoFields(t, file.Messages().ByName("RunAgentResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"response": 3, "dialogue_key": 4, "run_id": 5, "execution_profile": 6,
		"capability_ids": 7, "tweet_list": 8, "publishable_draft": 9,
		"tool_activities": 10, "citations": 11,
		"selected_skill_id": 15, "selected_skill_version": 16,
		"execution_strategy_plan": 19,
	})
	assertProtoFields(t, file.Messages().ByName("AgentToolActivity"), map[protoreflect.Name]protoreflect.FieldNumber{
		"step_index": 1, "tool_name": 2, "status": 3, "result_count": 4,
	})
	assertProtoFields(t, file.Messages().ByName("ListAgentExtensionsRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "kind": 2, "category": 3, "scope": 4, "status": 5,
		"search": 6, "after_cursor": 7, "page_size": 8,
	})
	assertProtoFields(t, file.Messages().ByName("ListAgentExtensionsResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"code": 1, "msg": 2, "contract_version": 3, "extensions": 4,
		"sources": 5, "next_cursor": 6, "has_more": 7,
	})
	assertProtoFields(t, file.Messages().ByName("AgentExtension"), map[protoreflect.Name]protoreflect.FieldNumber{
		"contract_version": 1, "extension_id": 2, "kind": 3, "name": 4,
		"display_name": 5, "description": 6, "version": 7, "source": 8,
		"capability_id": 9, "category": 10, "scope": 11, "status": 12,
		"approval_mode": 13, "health_status": 14, "skill": 15, "mcp": 16,
	})
	assertProtoFields(t, file.Messages().ByName("ListAgentMarketplaceExtensionsRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "kind": 2, "publisher_id": 3, "search": 4, "after_cursor": 5, "page_size": 6,
	})
	assertProtoFields(t, file.Messages().ByName("ListAgentMarketplaceExtensionsResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"code": 1, "msg": 2, "contract_version": 3, "releases": 4, "next_cursor": 5, "has_more": 6,
	})
	assertProtoFields(t, file.Messages().ByName("AgentMarketplacePublisher"), map[protoreflect.Name]protoreflect.FieldNumber{
		"publisher_id": 1, "display_name": 2, "verification": 3,
	})
	assertProtoFields(t, file.Messages().ByName("AgentMarketplaceExtension"), map[protoreflect.Name]protoreflect.FieldNumber{
		"contract_version": 1, "release_id": 2, "package_id": 3, "kind": 4,
		"version": 5, "display_name": 6, "description": 7, "publisher": 8,
		"artifact_digest_sha256": 9, "signature_key_id": 10, "capability_ids": 11,
		"requested_permissions": 12, "published_at_unix_ms": 13, "signature_verified": 14,
	})
	assertProtoFields(t, file.Messages().ByName("AgentMarketplaceManagedPublisher"), map[protoreflect.Name]protoreflect.FieldNumber{
		"contract_version": 1, "publisher_id": 2, "display_name": 3, "verification": 4,
		"signing_keys": 5, "owner_user_ids": 6, "revision": 7,
		"created_by": 8, "updated_by": 9, "verified_at_unix_ms": 10,
		"created_at_unix_ms": 11, "updated_at_unix_ms": 12,
	})
	assertProtoFields(t, file.Messages().ByName("PublishAgentMarketplaceReleaseRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"actor_user_id": 1, "manifest": 2, "signature_key_id": 3,
		"signature_base64": 4, "expected_publisher_revision": 5,
	})
	assertProtoFields(t, file.Messages().ByName("AgentMarketplaceManagedRelease"), map[protoreflect.Name]protoreflect.FieldNumber{
		"contract_version": 1, "release_id": 2, "manifest": 3, "signature_key_id": 4,
		"status": 5, "revision": 6, "published_by": 7, "withdrawn_by": 8,
		"withdrawal_reason_code": 9, "published_at_unix_ms": 10,
		"withdrawn_at_unix_ms": 11, "created_at_unix_ms": 12, "updated_at_unix_ms": 13,
	})
	assertProtoFields(t, file.Messages().ByName("AgentMarketplaceAuditEvent"), map[protoreflect.Name]protoreflect.FieldNumber{
		"contract_version": 1, "event_id": 2, "operation_id": 3, "action": 4,
		"outcome": 5, "actor_user_id": 6, "publisher_id": 7, "package_id": 8,
		"version": 9, "key_id": 10, "revision": 11, "reason_code": 12,
		"error_code": 13, "created_at_unix_ms": 14,
	})
	assertProtoFields(t, file.Messages().ByName("AgentCitation"), map[protoreflect.Name]protoreflect.FieldNumber{
		"citation_id": 1, "source_type": 2, "source_id": 3,
		"url": 4, "title": 5, "snippet": 6,
	})
	assertProtoFields(t, file.Messages().ByName("EndDialogueSessionRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "dialogue_id": 2, "dialogue_key": 3,
	})
	assertProtoFields(t, file.Messages().ByName("consultContentResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"response":     3,
		"tweet_list":   4,
		"dialogue_key": 5,
	})
	assertProtoFields(t, file.Messages().ByName("assistPublishTwitterResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"response":     3,
		"tweet_list":   4,
		"dialogue_key": 5,
		"run_id":       6,
	})
	assertProtoFields(t, file.Messages().ByName("RepositoryContentList"), map[protoreflect.Name]protoreflect.FieldNumber{
		"role": 7, "content": 8, "run_id": 9, "publishable_draft": 10,
	})
	assertProtoFields(t, file.Messages().ByName("ConfirmPublishTwitterRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "content": 2, "source_run_id": 3,
	})
	assertProtoFields(t, file.Messages().ByName("MultiAgentPublishTwitterRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id":             1,
		"domain":              2,
		"author_user_id":      3,
		"style_ratio":         4,
		"reference_tweet_ids": 5,
		"content":             6,
		"dialogue_key":        7,
	})
	assertProtoFields(t, file.Messages().ByName("RunWorkflowResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"run":          3,
		"dialogue_key": 4,
		"response":     5,
	})
	assertProtoFields(t, file.Messages().ByName("RunWorkflowRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id":              1,
		"workflow_id":          2,
		"input_json":           3,
		"workflow_revision_id": 4,
	})
	assertProtoFields(t, file.Messages().ByName("WorkflowDetail"), map[protoreflect.Name]protoreflect.FieldNumber{
		"current_revision_id":     7,
		"current_revision_number": 8,
		"current_dsl_hash":        9,
	})
	assertProtoFields(t, file.Messages().ByName("WorkflowRun"), map[protoreflect.Name]protoreflect.FieldNumber{
		"revision":                 13,
		"workflow_revision_id":     14,
		"workflow_revision_number": 15,
		"state_version":            16,
		"resume_grant_issued_at":   20,
		"resume_grant_expires_at":  21,
		"invocation_source":        22,
		"parent_run_id":            23,
		"parent_action_id":         24,
	})
	assertProtoFields(t, file.Messages().ByName("WorkflowToolPublication"), map[protoreflect.Name]protoreflect.FieldNumber{
		"publication_id": 1, "user_id": 2, "workflow_id": 3,
		"workflow_revision_id": 4, "workflow_revision_number": 5,
		"workflow_dsl_hash": 6, "tool_name": 7, "status": 11, "revision": 12,
	})
	assertProtoFields(t, file.Messages().ByName("IssueWorkflowResumeGrantRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "approval_id": 2, "expected_run_revision": 3,
	})
	assertProtoFields(t, file.Messages().ByName("AgentExecutionRun"), map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "status": 5, "revision": 6, "resume_supported": 7,
		"pending_action_type": 8, "pending_action_name": 9,
		"pending_action_id": 21, "approval_id": 22, "approval_expires_at": 23,
		"skill_id": 24, "skill_version": 25,
		"execution_strategy_plan": 28,
	})
	assertProtoFields(t, file.Messages().ByName("AgentExecutionStrategyPlan"), map[protoreflect.Name]protoreflect.FieldNumber{
		"version": 1, "template_id": 2, "candidate_strategy": 3, "selected_strategy": 4,
		"decision": 5, "reason_code": 6, "complexity_score": 7, "roles": 14,
		"plan_digest": 15,
	})
	assertProtoFields(t, file.Messages().ByName("AgentExecutionStrategyRole"), map[protoreflect.Name]protoreflect.FieldNumber{
		"role_id": 1, "capability_ids": 2, "allowed_tools": 3,
		"max_steps": 4, "max_total_tokens": 5, "timeout_millis": 7,
	})
	assertProtoFields(t, file.Messages().ByName("AgentSkill"), map[protoreflect.Name]protoreflect.FieldNumber{
		"contract_version": 1, "skill_id": 2, "version": 3,
		"allowed_tools": 8, "profile": 10, "budget": 11, "output": 12, "workflow": 13,
	})
	assertProtoFields(t, file.Messages().ByName("ResumeAgentRunRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "run_id": 2, "expected_revision": 3, "human_response": 4,
		"approval_id": 5, "resume_token": 6,
	})
	assertProtoFields(t, file.Messages().ByName("IssueAgentResumeGrantRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "approval_id": 2, "expected_run_revision": 3,
	})
	assertProtoFields(t, file.Messages().ByName("GetWorkflowRunReplayResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"run":           3,
		"revision":      4,
		"events":        5,
		"snapshot":      6,
		"compensations": 7,
		"integrity":     8,
	})
	assertProtoFields(t, file.Messages().ByName("SearchWorkflowBlackboardRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id": 1, "run_id": 2, "state_version": 3, "query": 4,
		"path_prefix": 5, "after_cursor": 6, "page_size": 7,
	})
	assertProtoFields(t, file.Messages().ByName("SearchWorkflowBlackboardResponse"), map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 3, "state_version": 4, "base_snapshot_version": 5,
		"base_snapshot_hash": 6, "state_hash": 7, "verified": 8,
		"entries": 9, "matched_total": 10, "next_cursor": 11, "has_more": 12,
	})
	assertProtoFields(t, file.Messages().ByName("AgentLLMCallTrace"), map[protoreflect.Name]protoreflect.FieldNumber{
		"prompt_template_id": 21, "prompt_template_version": 22,
		"prompt_sample": 23, "completion_sample": 24,
		"prompt_sample_status": 25, "completion_sample_status": 26,
		"content_sample_policy": 27,
	})
	assertProtoFields(t, file.Messages().ByName("AgentProfileSpec"), map[protoreflect.Name]protoreflect.FieldNumber{
		"profile_id": 1, "version": 2, "system_prompt": 5,
		"max_estimated_cost_micros": 10, "timeout_millis": 11, "allowed_tools": 12,
	})
	assertProtoFields(t, file.Messages().ByName("PublishAgentProfileVersionRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"actor_user_id": 1, "profile_id": 2, "version": 3, "expected_revision": 4,
	})
	assertProtoFields(t, file.Messages().ByName("AgentProfileAuditEvent"), map[protoreflect.Name]protoreflect.FieldNumber{
		"operation_id": 2, "outcome": 4, "actor_user_id": 7,
		"snapshot_hash": 10, "error_code": 11, "experiment_id": 14,
	})
	assertProtoFields(t, file.Messages().ByName("AgentProfileExperiment"), map[protoreflect.Name]protoreflect.FieldNumber{
		"experiment_id": 1, "profile_id": 2, "stable_version": 3, "candidate_version": 4,
		"release_revision": 6, "policy": 7, "status": 8, "stats": 11, "revision": 12,
	})
	assertProtoFields(t, file.Messages().ByName("StartAgentProfileExperimentRequest"), map[protoreflect.Name]protoreflect.FieldNumber{
		"actor_user_id": 1, "profile_id": 2, "expected_release_revision": 3, "policy": 4,
	})
}

func assertProtoFields(
	t *testing.T,
	message protoreflect.MessageDescriptor,
	want map[protoreflect.Name]protoreflect.FieldNumber,
) {
	t.Helper()
	if message == nil {
		t.Fatal("protobuf message descriptor is missing")
	}
	for name, number := range want {
		field := message.Fields().ByName(name)
		if field == nil {
			t.Fatalf("%s.%s is missing", message.Name(), name)
		}
		if field.Number() != number {
			t.Fatalf("%s.%s field number = %d, want %d", message.Name(), name, field.Number(), number)
		}
	}
}
