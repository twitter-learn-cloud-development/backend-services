package service

import (
	"context"
	"strings"

	"twitter-clone/internal/module/agent/repository"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

var contextualFollowUpTerms = []string{
	"\u8be6\u7ec6", "\u5c55\u5f00", "\u7ee7\u7eed", "\u66f4\u591a", "\u7ed3\u679c\u5462",
	"\u6765\u6e90\u5462", "\u539f\u6587", "\u521a\u624d", "\u4e0a\u8ff0", "\u8fd9\u4e9b",
	"\u90a3\u51e0\u6761", "\u5b83\u4eec", "\u518d\u5199", "\u518d\u8bf4", "\u63a5\u7740",
	"\u5177\u4f53\u5185\u5bb9",
	"tell me more", "more detail", "expand", "continue", "those results", "the sources",
}

// inferDialogueCapabilityHints preserves execution context only for an
// elliptical follow-up. Explicit intent and explicit selections win.
func (s *AgentService) inferDialogueCapabilityHints(
	ctx context.Context,
	request UnifiedAgentRequest,
) []string {
	if s == nil || s.repo == nil || !isContextualFollowUp(request.Content) {
		return nil
	}
	dialogueKey := strings.TrimSpace(resolveDialogueKey(request.DialogueID, request.DialogueKey))
	if dialogueKey == "" || dialogueKey == "0" {
		return nil
	}
	dialogueID, err := primitive.ObjectIDFromHex(dialogueKey)
	if err != nil {
		return nil
	}
	dialogue, err := s.repo.GetDialogue(ctx, dialogueID)
	if err != nil || dialogue == nil || dialogue.UserID != request.UserID {
		return nil
	}

	previous := s.lastReusableDialogueCapabilities(ctx, dialogue)
	if len(previous) == 0 {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(request.Content))
	if containsAny(query, workflowIntentTerms) ||
		containsAny(query, searchIntentTerms) ||
		containsAny(query, webSearchIntentTerms) {
		return nil
	}
	if containsAny(query, draftIntentTerms) {
		if containsCapability(previous, CapabilityPlatformSearch) {
			return []string{CapabilityPlatformSearch, CapabilityContentDraft}
		}
		if containsCapability(previous, CapabilityWebSearch) {
			return []string{CapabilityWebSearch, CapabilityContentDraft}
		}
		return []string{CapabilityContentDraft}
	}
	return previous
}

func (s *AgentService) lastReusableDialogueCapabilities(
	ctx context.Context,
	dialogue *repository.Dialogue,
) []string {
	messages, err := s.repo.GetRecentMessages(ctx, dialogue.ID, MaxContextMessages)
	if err == nil {
		for index := len(messages) - 1; index >= 0; index-- {
			message := messages[index]
			if message == nil || message.Role != repository.RoleAssistant {
				continue
			}
			capabilities := reusableCapabilitySet(
				metadataStringSlice(message.Metadata, "capability_ids"),
			)
			if len(capabilities) > 0 {
				return capabilities
			}
		}
	}

	// Compatibility runs did not persist capability_ids. Dialogue mode is used
	// only as a narrow migration fallback for search and draft conversations.
	switch dialogue.Mode {
	case repository.ModeConsult:
		return []string{CapabilityPlatformSearch}
	case repository.ModeAssist:
		return []string{CapabilityContentDraft}
	default:
		return nil
	}
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	switch value := metadata[key].(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func reusableCapabilitySet(values []string) []string {
	values = uniqueCapabilityIDs(values)
	hasPlatform := containsCapability(values, CapabilityPlatformSearch)
	hasWeb := containsCapability(values, CapabilityWebSearch)
	hasDraft := containsCapability(values, CapabilityContentDraft)
	switch {
	case hasPlatform && hasDraft && !hasWeb:
		return []string{CapabilityPlatformSearch, CapabilityContentDraft}
	case hasWeb && hasDraft && !hasPlatform:
		return []string{CapabilityWebSearch, CapabilityContentDraft}
	case hasPlatform && !hasWeb:
		return []string{CapabilityPlatformSearch}
	case hasWeb && !hasPlatform:
		return []string{CapabilityWebSearch}
	case hasDraft:
		return []string{CapabilityContentDraft}
	default:
		return nil
	}
}

func isContextualFollowUp(content string) bool {
	return containsAny(strings.ToLower(strings.TrimSpace(content)), contextualFollowUpTerms)
}
