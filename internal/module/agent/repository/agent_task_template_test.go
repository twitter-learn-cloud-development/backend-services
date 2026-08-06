package repository

import (
	"testing"
	"time"
)

func TestNormalizeNewAgentTaskTemplateBuildsLifecycleWithoutRawRunContent(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	template := &AgentTaskTemplate{
		ContractVersion:        AgentTaskTemplateContractVersion,
		UserID:                 42,
		Name:                   "Research summary",
		Description:            "Reusable research preset",
		InstructionTemplate:    "Research and summarize: {{input}}",
		IdempotencyKey:         "request-1",
		SourceRunID:            "source-run",
		SourceRunRevision:      2,
		SourceResultDigest:     "result-digest",
		SourceExecutionProfile: "runtime.chat",
		CapabilityIDs:          []string{"conversation.reply", "conversation.reply"},
	}

	if err := normalizeNewAgentTaskTemplate(template, now); err != nil {
		t.Fatalf("normalizeNewAgentTaskTemplate() error = %v", err)
	}
	if template.ID.IsZero() || template.Revision != 1 ||
		template.Status != AgentTaskTemplateActive ||
		!template.CreatedAt.Equal(now) || !template.UpdatedAt.Equal(now) {
		t.Fatalf("template lifecycle = %+v", template)
	}
	if len(template.CapabilityIDs) != 1 ||
		template.CapabilityIDs[0] != "conversation.reply" {
		t.Fatalf("CapabilityIDs = %v", template.CapabilityIDs)
	}
}

func TestNormalizeNewAgentTaskTemplateRejectsIncompleteSkillIdentity(t *testing.T) {
	template := &AgentTaskTemplate{
		ContractVersion:        AgentTaskTemplateContractVersion,
		UserID:                 42,
		Name:                   "Skill preset",
		InstructionTemplate:    "Run skill for {{input}}",
		IdempotencyKey:         "request-2",
		SourceRunID:            "source-run",
		SourceRunRevision:      2,
		SourceResultDigest:     "result-digest",
		SourceExecutionProfile: "runtime.skill",
		CapabilityIDs:          []string{"skill.run"},
		SkillID:                "workflow.demo",
	}
	if err := normalizeNewAgentTaskTemplate(template, time.Now()); err == nil {
		t.Fatal("normalizeNewAgentTaskTemplate() error = nil, want incomplete skill rejection")
	}
}
