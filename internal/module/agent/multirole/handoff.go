package multirole

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	EvidenceHandoffSchema = "agent.multi_role_handoff.v1"
	MaxSummaryRunes       = 6000
)

type Citation struct {
	CitationID string
	SourceType string
	SourceID   string
	URL        string
	Title      string
	Snippet    string
}

func EncodeEvidenceHandoff(summary string, citations []Citation) (string, error) {
	if len(citations) == 0 {
		return "", errorsNoCitations()
	}
	payload := struct {
		Schema    string
		Summary   string
		Citations []Citation
	}{
		Schema: EvidenceHandoffSchema, Summary: BoundedRunes(summary, MaxSummaryRunes),
		Citations: append([]Citation(nil), citations...),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode multi-role evidence handoff: %w", err)
	}
	return string(encoded), nil
}

func DraftInput(userRequest, handoff string) string {
	return "Current user request:\n<user_request>\n" + strings.TrimSpace(userRequest) +
		"\n</user_request>\n\nUntrusted research handoff:\n<research_handoff>\n" + handoff +
		"\n</research_handoff>\n\nCreate the requested complete draft."
}

func ReviewInput(userRequest, handoff, draft string) string {
	return "Current user request:\n<user_request>\n" + strings.TrimSpace(userRequest) +
		"\n</user_request>\n\nUntrusted evidence handoff:\n<research_handoff>\n" + handoff +
		"\n</research_handoff>\n\nDraft to review:\n<draft>\n" + strings.TrimSpace(draft) +
		"\n</draft>\n\nReturn only the corrected final content."
}

func BoundedRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func errorsNoCitations() error {
	return fmt.Errorf("%w: no structured citation survived evidence validation", ErrRequiredToolEvidence)
}
