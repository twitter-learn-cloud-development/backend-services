package main

import (
	"errors"
	"strings"

	"twitter-clone/internal/module/agent/eval"
)

func buildAgentTaskContentReviewDecisionTemplate(
	output agentTaskEvaluationOutput,
	binding eval.AgentTaskContentReviewBundleBinding,
) (eval.AgentTaskContentReviewDecision, error) {
	if output.Integrity == nil || output.Stable == nil {
		return eval.AgentTaskContentReviewDecision{}, errors.New("content review decision template requires a signed candidate/stable report")
	}
	if len(output.Candidate.CaseResults) == 0 || len(output.Candidate.CaseResults) != len(output.Stable.CaseResults) {
		return eval.AgentTaskContentReviewDecision{}, errors.New("content review decision template requires aligned candidate/stable cases")
	}
	failed := eval.AgentTaskContentReviewAssessment{
		FactualCorrectness: eval.AgentTaskContentReviewFailed,
		Relevance:          eval.AgentTaskContentReviewFailed,
		EvidenceFidelity:   eval.AgentTaskContentReviewFailed,
		WritingQuality:     eval.AgentTaskContentReviewFailed,
	}
	cases := make([]eval.AgentTaskContentReviewCaseDecision, len(output.Candidate.CaseResults))
	for index, candidate := range output.Candidate.CaseResults {
		if candidate.CaseID == "" || candidate.CaseID != output.Stable.CaseResults[index].CaseID {
			return eval.AgentTaskContentReviewDecision{}, errors.New("content review decision template report cases are not aligned")
		}
		cases[index] = eval.AgentTaskContentReviewCaseDecision{
			CaseID:    candidate.CaseID,
			Candidate: failed,
			Stable:    failed,
		}
	}
	return eval.AgentTaskContentReviewDecision{
		SchemaVersion:       eval.AgentTaskContentReviewDecisionSchemaVersion,
		ReportPayloadSHA256: strings.ToLower(output.Integrity.PayloadSHA256),
		ReviewBundleSHA256:  strings.ToLower(binding.FileSHA256),
		RuleVersion:         eval.AgentTaskContentReviewRuleVersion,
		Reviewer: eval.AgentTaskContentReviewer{
			Kind:              eval.AgentTaskContentReviewerExternalHuman,
			IdentityAssurance: eval.AgentTaskContentReviewerAssertedExternal,
		},
		CandidateVerdict: eval.AgentTaskContentReviewRejected,
		StableVerdict:    eval.AgentTaskContentReviewRejected,
		Cases:            cases,
	}, nil
}

func writeAgentTaskContentReviewDecisionTemplate(
	path string,
	output agentTaskEvaluationOutput,
	binding eval.AgentTaskContentReviewBundleBinding,
) error {
	template, err := buildAgentTaskContentReviewDecisionTemplate(output, binding)
	if err != nil {
		return err
	}
	return writeExclusiveReviewJSON(path, template, "content review decision template")
}
