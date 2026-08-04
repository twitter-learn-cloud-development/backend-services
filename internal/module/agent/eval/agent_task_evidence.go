package eval

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type AgentTaskEvidenceStatus string

const (
	AgentTaskEvidenceSufficient   AgentTaskEvidenceStatus = "sufficient"
	AgentTaskEvidenceInsufficient AgentTaskEvidenceStatus = "insufficient"

	agentTaskEvidenceMaxItems       = 8
	agentTaskEvidenceMaxClaims      = 16
	agentTaskEvidenceMaxItemRunes   = 4_000
	agentTaskEvidenceMaxTotalRunes  = 16_000
	agentTaskEvidenceMaxPhraseRunes = 256
	agentTaskEvidenceMaxSourceRunes = 256
	agentTaskEvidenceMaxTitleRunes  = 512
	agentTaskEvidenceMaxURLRunes    = 2_048
	agentTaskEvidenceCitationWindow = 240
)

var agentTaskEvidenceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// AgentTaskEvidenceContract defines deterministic grounding assertions for a
// task. It belongs to the dataset only; raw evidence is never copied into the
// signed evaluation report.
type AgentTaskEvidenceContract struct {
	Status                  AgentTaskEvidenceStatus  `json:"status"`
	Items                   []AgentTaskEvidenceItem  `json:"items,omitempty"`
	RequiredClaims          []AgentTaskRequiredClaim `json:"required_claims,omitempty"`
	InsufficientOutputAnyOf []string                 `json:"insufficient_output_any_of,omitempty"`
	RefusalPhrases          []string                 `json:"refusal_phrases,omitempty"`
	UnsupportedClaimPhrases []string                 `json:"unsupported_claim_phrases,omitempty"`
	ForbiddenMetadata       []string                 `json:"forbidden_metadata,omitempty"`
}

type AgentTaskEvidenceItem struct {
	CitationID string `json:"citation_id"`
	SourceID   string `json:"source_id"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	Content    string `json:"content"`
}

type AgentTaskRequiredClaim struct {
	ID          string   `json:"id"`
	Terms       []string `json:"terms"`
	EvidenceIDs []string `json:"evidence_ids"`
}

func normalizeAgentTaskEvidenceContract(input *AgentTaskEvidenceContract) (*AgentTaskEvidenceContract, error) {
	if input == nil {
		return nil, nil
	}

	contract := *input
	if contract.Status != AgentTaskEvidenceSufficient && contract.Status != AgentTaskEvidenceInsufficient {
		return nil, fmt.Errorf("invalid status %q", contract.Status)
	}
	if len(contract.Items) > agentTaskEvidenceMaxItems {
		return nil, fmt.Errorf("contains more than %d evidence items", agentTaskEvidenceMaxItems)
	}
	if len(contract.RequiredClaims) > agentTaskEvidenceMaxClaims {
		return nil, fmt.Errorf("contains more than %d required claims", agentTaskEvidenceMaxClaims)
	}

	items := make([]AgentTaskEvidenceItem, len(contract.Items))
	itemsByID := make(map[string]AgentTaskEvidenceItem, len(contract.Items))
	sourceIDs := make(map[string]struct{}, len(contract.Items))
	urls := make(map[string]struct{}, len(contract.Items))
	totalRunes := 0
	for index, raw := range contract.Items {
		item := raw
		item.CitationID = strings.TrimSpace(item.CitationID)
		item.SourceID = strings.TrimSpace(item.SourceID)
		item.URL = strings.TrimSpace(item.URL)
		item.Title = strings.TrimSpace(item.Title)
		item.Content = strings.TrimSpace(item.Content)
		if !agentTaskEvidenceIDPattern.MatchString(item.CitationID) {
			return nil, fmt.Errorf("item %d has invalid citation_id %q", index, item.CitationID)
		}
		if item.SourceID == "" {
			return nil, fmt.Errorf("item %d is missing source_id", index)
		}
		if utf8.RuneCountInString(item.SourceID) > agentTaskEvidenceMaxSourceRunes {
			return nil, fmt.Errorf("item %d source_id exceeds %d characters", index, agentTaskEvidenceMaxSourceRunes)
		}
		sourceKey := strings.ToLower(item.SourceID)
		if _, exists := sourceIDs[sourceKey]; exists {
			return nil, fmt.Errorf("contains duplicate source_id %q", item.SourceID)
		}
		sourceIDs[sourceKey] = struct{}{}
		if item.Content == "" {
			return nil, fmt.Errorf("item %d is missing content", index)
		}
		itemRunes := utf8.RuneCountInString(item.Content)
		if itemRunes > agentTaskEvidenceMaxItemRunes {
			return nil, fmt.Errorf("item %d content exceeds %d characters", index, agentTaskEvidenceMaxItemRunes)
		}
		totalRunes += itemRunes
		if totalRunes > agentTaskEvidenceMaxTotalRunes {
			return nil, fmt.Errorf("evidence content exceeds %d characters", agentTaskEvidenceMaxTotalRunes)
		}
		if item.URL != "" {
			if utf8.RuneCountInString(item.URL) > agentTaskEvidenceMaxURLRunes {
				return nil, fmt.Errorf("item %d URL exceeds %d characters", index, agentTaskEvidenceMaxURLRunes)
			}
			parsed, err := url.ParseRequestURI(item.URL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
				return nil, fmt.Errorf("item %d has invalid URL", index)
			}
			urlKey := strings.ToLower(item.URL)
			if _, exists := urls[urlKey]; exists {
				return nil, fmt.Errorf("contains duplicate URL %q", item.URL)
			}
			urls[urlKey] = struct{}{}
		}
		if utf8.RuneCountInString(item.Title) > agentTaskEvidenceMaxTitleRunes {
			return nil, fmt.Errorf("item %d title exceeds %d characters", index, agentTaskEvidenceMaxTitleRunes)
		}
		key := strings.ToLower(item.CitationID)
		if _, exists := itemsByID[key]; exists {
			return nil, fmt.Errorf("contains duplicate citation_id %q", item.CitationID)
		}
		items[index] = item
		itemsByID[key] = item
	}
	contract.Items = items

	claims := make([]AgentTaskRequiredClaim, len(contract.RequiredClaims))
	claimIDs := make(map[string]struct{}, len(contract.RequiredClaims))
	for index, raw := range contract.RequiredClaims {
		claim := raw
		claim.ID = strings.TrimSpace(claim.ID)
		if !agentTaskEvidenceIDPattern.MatchString(claim.ID) {
			return nil, fmt.Errorf("claim %d has invalid id %q", index, claim.ID)
		}
		key := strings.ToLower(claim.ID)
		if _, exists := claimIDs[key]; exists {
			return nil, fmt.Errorf("contains duplicate claim id %q", claim.ID)
		}
		claimIDs[key] = struct{}{}
		var err error
		if claim.Terms, err = normalizeBoundedAgentTaskEvidenceStrings("terms", claim.Terms, 16); err != nil {
			return nil, fmt.Errorf("claim %q: %w", claim.ID, err)
		}
		if len(claim.Terms) == 0 {
			return nil, fmt.Errorf("claim %q has no terms", claim.ID)
		}
		if claim.EvidenceIDs, err = normalizeBoundedAgentTaskEvidenceStrings("evidence_ids", claim.EvidenceIDs, agentTaskEvidenceMaxItems); err != nil {
			return nil, fmt.Errorf("claim %q: %w", claim.ID, err)
		}
		if len(claim.EvidenceIDs) == 0 {
			return nil, fmt.Errorf("claim %q has no evidence_ids", claim.ID)
		}
		grounded := false
		for evidenceIndex, evidenceID := range claim.EvidenceIDs {
			item, exists := itemsByID[strings.ToLower(evidenceID)]
			if !exists {
				return nil, fmt.Errorf("claim %q references unknown evidence_id %q", claim.ID, evidenceID)
			}
			if agentTaskContainsAllTerms(item.Content, claim.Terms) {
				grounded = true
			}
			claim.EvidenceIDs[evidenceIndex] = item.CitationID
		}
		if !grounded {
			return nil, fmt.Errorf("claim %q terms are not jointly grounded by a referenced evidence item", claim.ID)
		}
		claims[index] = claim
	}
	contract.RequiredClaims = claims

	var err error
	if contract.InsufficientOutputAnyOf, err = normalizeBoundedAgentTaskEvidenceStrings("insufficient_output_any_of", contract.InsufficientOutputAnyOf, 16); err != nil {
		return nil, err
	}
	if contract.RefusalPhrases, err = normalizeBoundedAgentTaskEvidenceStrings("refusal_phrases", contract.RefusalPhrases, 16); err != nil {
		return nil, err
	}
	if contract.UnsupportedClaimPhrases, err = normalizeBoundedAgentTaskEvidenceStrings("unsupported_claim_phrases", contract.UnsupportedClaimPhrases, 32); err != nil {
		return nil, err
	}
	if contract.ForbiddenMetadata, err = normalizeBoundedAgentTaskEvidenceStrings("forbidden_metadata", contract.ForbiddenMetadata, 32); err != nil {
		return nil, err
	}

	switch contract.Status {
	case AgentTaskEvidenceSufficient:
		if len(contract.Items) == 0 || len(contract.RequiredClaims) == 0 {
			return nil, fmt.Errorf("sufficient evidence requires items and required_claims")
		}
		if len(contract.InsufficientOutputAnyOf) > 0 {
			return nil, fmt.Errorf("sufficient evidence cannot define insufficient_output_any_of")
		}
	case AgentTaskEvidenceInsufficient:
		if len(contract.Items) > 0 || len(contract.RequiredClaims) > 0 {
			return nil, fmt.Errorf("insufficient evidence cannot define items or required_claims")
		}
		if len(contract.InsufficientOutputAnyOf) == 0 {
			return nil, fmt.Errorf("insufficient evidence requires insufficient_output_any_of")
		}
	}
	return &contract, nil
}

func validateAgentTaskEvidenceToolProjection(contract *AgentTaskEvidenceContract, expectedTools, allowedTools []string) error {
	if contract == nil || contract.Status == AgentTaskEvidenceInsufficient {
		return nil
	}
	platform := false
	web := false
	for _, tool := range append(append([]string(nil), expectedTools...), allowedTools...) {
		switch tool {
		case "hybrid_search_tweets":
			platform = true
		case "web_search", "page_read":
			web = true
		}
	}
	for _, item := range contract.Items {
		if platform {
			if _, err := strconv.ParseUint(item.SourceID, 10, 64); err != nil {
				return fmt.Errorf("citation %q requires a numeric source_id for platform search", item.CitationID)
			}
		}
		if web && item.URL == "" {
			return fmt.Errorf("citation %q requires a URL for web search/page read", item.CitationID)
		}
	}
	return nil
}

func normalizeBoundedAgentTaskEvidenceStrings(label string, values []string, maxCount int) ([]string, error) {
	if len(values) > maxCount {
		return nil, fmt.Errorf("%s contains more than %d values", label, maxCount)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s contains an empty value", label)
		}
		if utf8.RuneCountInString(value) > agentTaskEvidenceMaxPhraseRunes {
			return nil, fmt.Errorf("%s value exceeds %d characters", label, agentTaskEvidenceMaxPhraseRunes)
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%s contains duplicate value %q", label, value)
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func evaluateAgentTaskEvidenceAssertions(contract AgentTaskEvidenceContract, output string) []string {
	normalized := strings.ToLower(output)
	failures := make([]string, 0, 6)
	for _, phrase := range contract.UnsupportedClaimPhrases {
		if strings.Contains(normalized, strings.ToLower(phrase)) {
			failures = appendUniqueAgentTaskFailure(failures, "contains_unsupported_claim")
		}
	}
	for _, phrase := range contract.ForbiddenMetadata {
		if strings.Contains(normalized, strings.ToLower(phrase)) {
			failures = appendUniqueAgentTaskFailure(failures, "contains_internal_metadata")
		}
	}

	if contract.Status == AgentTaskEvidenceInsufficient {
		if !agentTaskContainsAnyTerm(normalized, contract.InsufficientOutputAnyOf) {
			failures = appendUniqueAgentTaskFailure(failures, "missing_insufficient_evidence_notice")
		}
		return failures
	}

	if agentTaskContainsAnyTerm(normalized, contract.RefusalPhrases) {
		failures = appendUniqueAgentTaskFailure(failures, "rejects_sufficient_evidence")
	}
	for _, claim := range contract.RequiredClaims {
		claimPresent := agentTaskContainsAllTerms(normalized, claim.Terms)
		if !claimPresent {
			failures = appendUniqueAgentTaskFailure(failures, "missing_required_claim")
			continue
		}
		if !agentTaskContainsAnyCitation(normalized, claim.EvidenceIDs) {
			failures = appendUniqueAgentTaskFailure(failures, "missing_required_citation")
			continue
		}
		if !agentTaskClaimLinkedToCitation(normalized, claim) {
			failures = appendUniqueAgentTaskFailure(failures, "claim_not_linked_to_citation")
		}
	}
	return failures
}

func agentTaskContainsAllTerms(text string, terms []string) bool {
	text = strings.ToLower(text)
	for _, term := range terms {
		if !strings.Contains(text, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

func agentTaskContainsAnyTerm(normalized string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(normalized, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func agentTaskContainsAnyCitation(normalized string, evidenceIDs []string) bool {
	for _, evidenceID := range evidenceIDs {
		if strings.Contains(normalized, strings.ToLower("["+evidenceID+"]")) {
			return true
		}
	}
	return false
}

func agentTaskClaimLinkedToCitation(normalized string, claim AgentTaskRequiredClaim) bool {
	textRunes := []rune(normalized)
	for _, evidenceID := range claim.EvidenceIDs {
		markerRunes := []rune(strings.ToLower("[" + evidenceID + "]"))
		for offset := 0; offset+len(markerRunes) <= len(textRunes); {
			relative := indexAgentTaskRunes(textRunes[offset:], markerRunes)
			if relative < 0 {
				break
			}
			markerStart := offset + relative
			start := markerStart - agentTaskEvidenceCitationWindow
			if start < 0 {
				start = 0
			}
			end := markerStart + len(markerRunes) + agentTaskEvidenceCitationWindow
			if end > len(textRunes) {
				end = len(textRunes)
			}
			if agentTaskContainsAllTerms(string(textRunes[start:end]), claim.Terms) {
				return true
			}
			offset = markerStart + len(markerRunes)
		}
	}
	return false
}

func indexAgentTaskRunes(text, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	for index := 0; index+len(needle) <= len(text); index++ {
		match := true
		for needleIndex := range needle {
			if text[index+needleIndex] != needle[needleIndex] {
				match = false
				break
			}
		}
		if match {
			return index
		}
	}
	return -1
}

func appendUniqueAgentTaskFailure(failures []string, code string) []string {
	for _, existing := range failures {
		if existing == code {
			return failures
		}
	}
	return append(failures, code)
}
