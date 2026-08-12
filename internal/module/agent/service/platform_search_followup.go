package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"twitter-clone/internal/module/agent/repository"
)

const platformTweetReferencesMetadataKey = "platform_tweet_refs"

var (
	ErrPlatformTweetReferenceAmbiguous = errors.New("platform tweet reference is ambiguous")
	ErrPlatformTweetReferenceUntrusted = errors.New("platform tweet reference is not present in prior search results")

	platformTweetIDPattern = regexp.MustCompile(`(?:^|[^0-9])([0-9]{5,20})(?:[^0-9]|$)`)
	platformOrdinalPattern = regexp.MustCompile(`第\s*([0-9一二三四五六七八九十]+)\s*(?:条|个|篇)?`)
	englishOrdinalPattern  = regexp.MustCompile(`\b(first|second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth|[0-9]+(?:st|nd|rd|th))\b`)
)

type platformTweetFollowUp struct {
	TweetID   string
	Reference string
}

func (s *AgentService) resolvePlatformTweetFollowUp(
	ctx context.Context,
	dialogue *repository.Dialogue,
	content string,
) (platformTweetFollowUp, bool, error) {
	if s == nil || s.repo == nil || dialogue == nil || !isContextualFollowUp(content) {
		return platformTweetFollowUp{}, false, nil
	}
	references := s.lastPlatformTweetReferences(ctx, dialogue)
	if len(references) == 0 {
		return platformTweetFollowUp{}, false, nil
	}

	explicitIDs := explicitPlatformTweetIDs(content)
	if len(explicitIDs) > 0 {
		if len(explicitIDs) != 1 {
			return platformTweetFollowUp{}, false, fmt.Errorf(
				"%w: select exactly one prior tweet",
				ErrPlatformTweetReferenceAmbiguous,
			)
		}
		explicitID := explicitIDs[0]
		for _, reference := range references {
			if reference.TweetID == explicitID {
				return reference, true, nil
			}
		}
		return platformTweetFollowUp{}, false, fmt.Errorf(
			"%w: tweet %s",
			ErrPlatformTweetReferenceUntrusted,
			explicitID,
		)
	}

	if ordinal, ok := platformTweetOrdinal(content); ok {
		if ordinal < 1 || ordinal > len(references) {
			return platformTweetFollowUp{}, false, fmt.Errorf(
				"%w: result %d is outside the prior result set",
				ErrPlatformTweetReferenceAmbiguous,
				ordinal,
			)
		}
		return references[ordinal-1], true, nil
	}
	if len(references) == 1 {
		return references[0], true, nil
	}
	return platformTweetFollowUp{}, false, fmt.Errorf(
		"%w: select one of the %d prior results",
		ErrPlatformTweetReferenceAmbiguous,
		len(references),
	)
}

func (s *AgentService) lastPlatformTweetReferences(
	ctx context.Context,
	dialogue *repository.Dialogue,
) []platformTweetFollowUp {
	messages, err := s.repo.GetRecentMessages(ctx, dialogue.ID, MaxContextMessages)
	if err != nil {
		return nil
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message == nil || message.Role != repository.RoleAssistant {
			continue
		}
		values := metadataStringSlice(message.Metadata, platformTweetReferencesMetadataKey)
		references := make([]platformTweetFollowUp, 0, len(values))
		seen := make(map[string]struct{}, len(values))
		for _, value := range values {
			reference, ok := parsePlatformTweetReference(value)
			if !ok {
				continue
			}
			if _, exists := seen[reference.TweetID]; exists {
				continue
			}
			seen[reference.TweetID] = struct{}{}
			references = append(references, reference)
		}
		if len(references) > 0 {
			return references
		}
	}
	return nil
}

func parsePlatformTweetReference(value string) (platformTweetFollowUp, bool) {
	value = strings.TrimSpace(value)
	const prefix = "/tweets/"
	if !strings.HasPrefix(value, prefix) {
		return platformTweetFollowUp{}, false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 64)
	if err != nil || id == 0 {
		return platformTweetFollowUp{}, false
	}
	normalized := strconv.FormatUint(id, 10)
	return platformTweetFollowUp{TweetID: normalized, Reference: prefix + normalized}, true
}

func explicitPlatformTweetIDs(content string) []string {
	matches := platformTweetIDPattern.FindAllStringSubmatch(content, -1)
	result := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		id, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil || id == 0 {
			continue
		}
		normalized := strconv.FormatUint(id, 10)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func platformTweetOrdinal(content string) (int, bool) {
	if match := platformOrdinalPattern.FindStringSubmatch(content); len(match) == 2 {
		return parsePlatformOrdinal(match[1])
	}
	match := englishOrdinalPattern.FindStringSubmatch(strings.ToLower(content))
	if len(match) != 2 {
		return 0, false
	}
	return parsePlatformOrdinal(match[1])
}

func parsePlatformOrdinal(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	english := map[string]int{
		"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5,
		"sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9, "tenth": 10,
	}
	if ordinal, ok := english[value]; ok {
		return ordinal, true
	}
	chinese := map[string]int{
		"一": 1, "二": 2, "三": 3, "四": 4, "五": 5,
		"六": 6, "七": 7, "八": 8, "九": 9, "十": 10,
	}
	if ordinal, ok := chinese[value]; ok {
		return ordinal, true
	}
	value = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(value, "st"), "nd"), "rd"), "th")
	ordinal, err := strconv.Atoi(value)
	return ordinal, err == nil && ordinal > 0
}

func platformTweetReferences(citations []AgentCitation) []string {
	references := make([]string, 0, len(citations))
	seen := make(map[string]struct{}, len(citations))
	for _, citation := range citations {
		if citation.SourceType != AgentCitationPlatformTweet ||
			!validPlatformTweetCitationURL(citation.URL, citation.SourceID) {
			continue
		}
		reference := platformTweetCitationURL(citation.SourceID)
		if _, exists := seen[reference]; exists {
			continue
		}
		seen[reference] = struct{}{}
		references = append(references, reference)
	}
	return references
}

func platformTweetFollowUpSystemPrompt(selection platformTweetFollowUp) string {
	return fmt.Sprintf(`
This request selects a trusted result from the immediately preceding platform search.
You must call get_tweets_by_ids exactly for tweet_ids %q before answering.
Use only the structured platform.tweet_detail.v1 result. If it does not contain that tweet ID, state that authoritative detail is unavailable.
Do not search again, substitute another result, or reconstruct missing content from conversation summaries.`, selection.TweetID)
}
