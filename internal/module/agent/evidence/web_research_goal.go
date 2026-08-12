package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	WebSearchSourcesCriterion = "web_search_sources_observed"
	WebPageContentCriterion   = "web_page_content_observed"

	WebSearchObservationMissingCode = "web_search_observation_missing"
	WebSearchProviderErrorCode      = "web_search_provider_error"
	WebSearchEmptyResultCode        = "web_search_empty_result"
	WebSearchReferenceInvalidCode   = "web_search_reference_invalid"
	WebSearchEvidenceInvalidCode    = "web_search_evidence_invalid"
	WebPageBlockedBySearchCode      = "web_page_blocked_by_search"
	WebPageReadMissingCode          = "web_page_read_missing"
	WebPageReadErrorCode            = "web_page_read_error"
	WebPageReferenceInvalidCode     = "web_page_reference_invalid"
	WebPageEvidenceInvalidCode      = "web_page_evidence_invalid"

	webSearchTool = "web_search"
	webPageTool   = "page_read"
)

type webResearchRecord struct {
	Source     string
	Digest     string
	Reference  string
	StepIndex  int
	ActionID   string
	CapturedAt time.Time
}

type webResearchDiagnostic struct {
	CriterionID string
	Source      string
	Code        string
	Digest      string
	StepIndex   int
	ActionID    string
	CapturedAt  time.Time
	Observed    bool
}

type webToolAttempt struct {
	StepIndex   int
	FinishedAt  time.Time
	Action      agentRuntime.Action
	Observation agentRuntime.Observation
	Observed    bool
}

// WebResearchGoalCollector proves a search-to-page chain from paired,
// structured Runtime observations. Raw provider and page bodies remain in the
// owning Runtime result; the ledger receives only digests and canonical URLs.
type WebResearchGoalCollector struct{}

func (WebResearchGoalCollector) Collect(
	_ context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	if err := validateWebResearchTask(request.Task); err != nil {
		return nil, err
	}

	searches, pages := trustedWebResearchRecords(request.Run)
	diagnostics := trustedWebResearchDiagnostics(request.Run, searches, pages)
	items := make([]agentRuntime.Evidence, 0, len(searches)+len(pages)+len(diagnostics))
	for _, record := range searches {
		items = append(items, webResearchEvidence(
			"web-search", WebSearchSourcesCriterion, request, record,
		))
	}
	for _, record := range pages {
		items = append(items, webResearchEvidence(
			"web-page", WebPageContentCriterion, request, record,
		))
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Observed {
			items = append(items, webResearchDiagnosticEvidence(request, diagnostic))
		}
	}
	return items, nil
}

type WebResearchGoalVerifier struct{}

func (WebResearchGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	if err := validateWebResearchTask(request.Task); err != nil {
		return agentRuntime.VerificationResult{}, err
	}

	searches, pages := trustedWebResearchRecords(request.Run)
	validSearches := webResearchRecordIndex(searches)
	validPages := webResearchRecordIndex(pages)
	diagnostics := trustedWebResearchDiagnostics(request.Run, searches, pages)
	validDiagnostics := webResearchDiagnosticIndex(diagnostics)
	searchIDsByReference := make(map[string][]string)
	pageIDsByReference := make(map[string][]string)
	diagnosticIDs := make(map[string][]string)
	for _, item := range request.Evidence.Items {
		if item.Kind != agentRuntime.EvidenceToolObservation {
			continue
		}
		key := item.Source + "|" + item.Digest + "|" + item.Reference
		if diagnostic, ok := validDiagnostics[key]; ok {
			if containsString(item.CriterionIDs, diagnostic.CriterionID) {
				diagnosticIDs[diagnostic.CriterionID] = append(
					diagnosticIDs[diagnostic.CriterionID], item.ID,
				)
			}
			continue
		}
		if containsString(item.CriterionIDs, WebSearchSourcesCriterion) {
			if _, ok := validSearches[key]; ok {
				searchIDsByReference[item.Reference] = append(searchIDsByReference[item.Reference], item.ID)
			}
			continue
		}
		if containsString(item.CriterionIDs, WebPageContentCriterion) {
			if _, ok := validPages[key]; ok {
				pageIDsByReference[item.Reference] = append(pageIDsByReference[item.Reference], item.ID)
			}
		}
	}

	searchEvidenceIDs := make([]string, 0)
	pageEvidenceIDs := make([]string, 0)
	for _, searchIDs := range searchIDsByReference {
		searchEvidenceIDs = append(searchEvidenceIDs, searchIDs...)
	}
	for reference, pageIDs := range pageIDsByReference {
		if len(searchIDsByReference[reference]) == 0 {
			continue
		}
		pageEvidenceIDs = append(pageEvidenceIDs, pageIDs...)
	}
	sort.Strings(searchEvidenceIDs)
	sort.Strings(pageEvidenceIDs)
	replaceCheck(&base, webResearchCheck(
		WebSearchSourcesCriterion,
		"web_search_source_verified",
		webResearchDiagnosticCode(diagnostics, WebSearchSourcesCriterion),
		searchEvidenceIDs,
		diagnosticIDs[WebSearchSourcesCriterion],
	))
	replaceCheck(&base, webResearchCheck(
		WebPageContentCriterion,
		"web_page_content_verified",
		webResearchDiagnosticCode(diagnostics, WebPageContentCriterion),
		pageEvidenceIDs,
		diagnosticIDs[WebPageContentCriterion],
	))
	base.MissingEvidence = missingRequiredCriteria(request.Task, base.Checks)
	if len(base.MissingEvidence) == 0 {
		base.Status = agentRuntime.VerificationPassed
		base.Retryable = false
	} else {
		base.Status = agentRuntime.VerificationFailed
		base.Retryable = request.RepairAttempts < request.Task.MaxRepairAttempts
	}
	return base, nil
}

func validateWebResearchTask(task agentRuntime.TaskSpec) error {
	for _, criterion := range task.CompletionCriteria {
		if !criterion.Required {
			continue
		}
		if criterion.ID != WebSearchSourcesCriterion && criterion.ID != WebPageContentCriterion {
			return fmt.Errorf("web research verifier cannot prove required criterion %q", criterion.ID)
		}
	}
	if !taskHasCriterion(task, WebSearchSourcesCriterion) ||
		!taskHasCriterion(task, WebPageContentCriterion) {
		return fmt.Errorf("web research task is missing required criteria")
	}
	return nil
}

func webResearchEvidence(
	prefix string,
	criterionID string,
	request agentRuntime.EvidenceCollectionRequest,
	record webResearchRecord,
) agentRuntime.Evidence {
	idDigest := sha256.Sum256([]byte(fmt.Sprintf(
		"%s|%d|%d|%s|%s|%s",
		request.Run.Context.RunID,
		request.Attempt,
		record.StepIndex,
		record.ActionID,
		record.Source,
		record.Reference,
	)))
	return agentRuntime.Evidence{
		ID:           prefix + ":" + hex.EncodeToString(idDigest[:12]),
		Kind:         agentRuntime.EvidenceToolObservation,
		Source:       record.Source,
		CriterionIDs: []string{criterionID},
		Digest:       record.Digest,
		Reference:    record.Reference,
		StepIndex:    record.StepIndex,
		CapturedAt:   record.CapturedAt,
	}
}

func webResearchRecordIndex(records []webResearchRecord) map[string]struct{} {
	result := make(map[string]struct{}, len(records))
	for _, record := range records {
		result[record.Source+"|"+record.Digest+"|"+record.Reference] = struct{}{}
	}
	return result
}

func webResearchDiagnosticEvidence(
	request agentRuntime.EvidenceCollectionRequest,
	diagnostic webResearchDiagnostic,
) agentRuntime.Evidence {
	idDigest := sha256.Sum256([]byte(fmt.Sprintf(
		"%s|%d|%d|%s|%s|%s",
		request.Run.Context.RunID,
		request.Attempt,
		diagnostic.StepIndex,
		diagnostic.ActionID,
		diagnostic.Source,
		diagnostic.Code,
	)))
	return agentRuntime.Evidence{
		ID:           "web-diagnostic:" + hex.EncodeToString(idDigest[:12]),
		Kind:         agentRuntime.EvidenceToolObservation,
		Source:       diagnostic.Source,
		CriterionIDs: []string{diagnostic.CriterionID},
		Digest:       diagnostic.Digest,
		StepIndex:    diagnostic.StepIndex,
		CapturedAt:   diagnostic.CapturedAt,
	}
}

func webResearchDiagnosticIndex(
	diagnostics []webResearchDiagnostic,
) map[string]webResearchDiagnostic {
	result := make(map[string]webResearchDiagnostic, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Observed {
			result[diagnostic.Source+"|"+diagnostic.Digest+"|"] = diagnostic
		}
	}
	return result
}

func webResearchDiagnosticCode(diagnostics []webResearchDiagnostic, criterionID string) string {
	for _, diagnostic := range diagnostics {
		if diagnostic.CriterionID == criterionID {
			return diagnostic.Code
		}
	}
	if criterionID == WebSearchSourcesCriterion {
		return WebSearchObservationMissingCode
	}
	return WebPageReadMissingCode
}

func webResearchCheck(
	criterionID string,
	passCode string,
	failCode string,
	verifiedIDs []string,
	diagnosticIDs []string,
) agentRuntime.CheckResult {
	if len(verifiedIDs) > 0 {
		sort.Strings(verifiedIDs)
		return agentRuntime.CheckResult{
			CriterionID: criterionID,
			Status:      agentRuntime.VerificationPassed,
			Code:        passCode,
			EvidenceIDs: verifiedIDs,
		}
	}
	sort.Strings(diagnosticIDs)
	return agentRuntime.CheckResult{
		CriterionID: criterionID,
		Status:      agentRuntime.VerificationFailed,
		Code:        failCode,
		EvidenceIDs: diagnosticIDs,
	}
}

func trustedWebResearchDiagnostics(
	run agentRuntime.RunResult,
	searches []webResearchRecord,
	pages []webResearchRecord,
) []webResearchDiagnostic {
	diagnostics := make([]webResearchDiagnostic, 0, 2)
	if len(searches) == 0 {
		diagnostics = append(diagnostics, diagnoseWebSearch(run))
	}
	if len(pages) == 0 {
		diagnostics = append(diagnostics, diagnoseWebPage(run, searches))
	}
	return diagnostics
}

func diagnoseWebSearch(run agentRuntime.RunResult) webResearchDiagnostic {
	attempts := webToolAttempts(run, webSearchTool)
	if len(attempts) == 0 {
		return newWebResearchDiagnostic(
			run.Context.RunID, WebSearchSourcesCriterion, webSearchTool,
			WebSearchObservationMissingCode, webToolAttempt{}, false,
		)
	}
	best := newWebResearchDiagnostic(
		run.Context.RunID, WebSearchSourcesCriterion, webSearchTool,
		WebSearchObservationMissingCode, attempts[0], false,
	)
	bestPriority := 5
	for _, attempt := range attempts {
		if !attempt.Observed {
			continue
		}
		code := WebSearchEvidenceInvalidCode
		priority := 4
		if attempt.Observation.IsError {
			code = WebSearchProviderErrorCode
			priority = 1
		} else {
			var arguments struct {
				Query string `json:"query"`
			}
			var result WebSearchResult
			validEnvelope := len(attempt.Observation.StructuredContent) > 0 &&
				len(attempt.Observation.StructuredContent) <= maxGoalStructuredEvidenceSize &&
				attempt.Observation.Name == webSearchTool &&
				json.Unmarshal(attempt.Action.Arguments, &arguments) == nil &&
				json.Unmarshal(attempt.Observation.StructuredContent, &result) == nil &&
				result.Schema == WebSearchSchema && strings.TrimSpace(result.Provider) != "" &&
				normalizeWebQuery(arguments.Query) != "" &&
				normalizeWebQuery(arguments.Query) == normalizeWebQuery(result.Query)
			switch {
			case !validEnvelope:
				code = WebSearchEvidenceInvalidCode
				priority = 4
			case len(result.Items) == 0:
				code = WebSearchEmptyResultCode
				priority = 2
			default:
				code = WebSearchReferenceInvalidCode
				priority = 3
			}
		}
		if priority < bestPriority {
			best = newWebResearchDiagnostic(
				run.Context.RunID, WebSearchSourcesCriterion, webSearchTool,
				code, attempt, true,
			)
			bestPriority = priority
		}
	}
	return best
}

func diagnoseWebPage(
	run agentRuntime.RunResult,
	searches []webResearchRecord,
) webResearchDiagnostic {
	if len(searches) == 0 {
		return newWebResearchDiagnostic(
			run.Context.RunID, WebPageContentCriterion, webPageTool,
			WebPageBlockedBySearchCode, webToolAttempt{}, false,
		)
	}
	attempts := webToolAttempts(run, webPageTool)
	if len(attempts) == 0 {
		return newWebResearchDiagnostic(
			run.Context.RunID, WebPageContentCriterion, webPageTool,
			WebPageReadMissingCode, webToolAttempt{}, false,
		)
	}
	searchStepByReference := make(map[string]int, len(searches))
	for _, search := range searches {
		if previous, ok := searchStepByReference[search.Reference]; !ok || search.StepIndex < previous {
			searchStepByReference[search.Reference] = search.StepIndex
		}
	}
	best := newWebResearchDiagnostic(
		run.Context.RunID, WebPageContentCriterion, webPageTool,
		WebPageReadMissingCode, attempts[0], false,
	)
	bestPriority := 4
	for _, attempt := range attempts {
		if !attempt.Observed {
			continue
		}
		code := WebPageEvidenceInvalidCode
		priority := 3
		if attempt.Observation.IsError {
			code = WebPageReadErrorCode
			priority = 1
		} else {
			var arguments struct {
				URL string `json:"url"`
			}
			var result WebPageResult
			validEnvelope := len(attempt.Observation.StructuredContent) > 0 &&
				len(attempt.Observation.StructuredContent) <= maxGoalStructuredEvidenceSize &&
				attempt.Observation.Name == webPageTool &&
				json.Unmarshal(attempt.Action.Arguments, &arguments) == nil &&
				json.Unmarshal(attempt.Observation.StructuredContent, &result) == nil &&
				result.Schema == WebPageSchema && strings.TrimSpace(result.Content) != "" &&
				supportedWebPageContentType(result.ContentType)
			if validEnvelope {
				actionURL, actionOK := canonicalPublicWebURL(arguments.URL)
				resultURL, resultOK := canonicalPublicWebURL(result.URL)
				searchStep, discovered := searchStepByReference[resultURL]
				if !actionOK || !resultOK || actionURL != resultURL ||
					!discovered || searchStep >= attempt.StepIndex {
					code = WebPageReferenceInvalidCode
					priority = 2
				}
			}
		}
		if priority < bestPriority {
			best = newWebResearchDiagnostic(
				run.Context.RunID, WebPageContentCriterion, webPageTool,
				code, attempt, true,
			)
			bestPriority = priority
		}
	}
	return best
}

func newWebResearchDiagnostic(
	runID string,
	criterionID string,
	source string,
	code string,
	attempt webToolAttempt,
	observed bool,
) webResearchDiagnostic {
	diagnostic := webResearchDiagnostic{
		CriterionID: criterionID,
		Source:      source,
		Code:        code,
		StepIndex:   attempt.StepIndex,
		ActionID:    attempt.Action.ID,
		CapturedAt:  attempt.FinishedAt,
		Observed:    observed,
	}
	if observed {
		digest := sha256.Sum256([]byte(fmt.Sprintf(
			"%s|%d|%s|%s|%s",
			runID, attempt.StepIndex, attempt.Action.ID, source, code,
		)))
		diagnostic.Digest = "sha256:" + hex.EncodeToString(digest[:])
	}
	return diagnostic
}

func webToolAttempts(run agentRuntime.RunResult, toolName string) []webToolAttempt {
	attempts := make([]webToolAttempt, 0)
	for _, step := range run.Steps {
		for _, action := range step.Actions {
			if action.Type != agentRuntime.ActionToolCall || action.Name != toolName {
				continue
			}
			observed := false
			for _, observation := range step.Observations {
				if observation.ActionID != action.ID {
					continue
				}
				attempts = append(attempts, webToolAttempt{
					StepIndex: step.Index, FinishedAt: step.FinishedAt,
					Action: action, Observation: observation, Observed: true,
				})
				observed = true
			}
			if !observed {
				attempts = append(attempts, webToolAttempt{
					StepIndex: step.Index, FinishedAt: step.FinishedAt, Action: action,
				})
			}
		}
	}
	return attempts
}

func trustedWebResearchRecords(
	run agentRuntime.RunResult,
) ([]webResearchRecord, []webResearchRecord) {
	searches := make([]webResearchRecord, 0)
	firstSearchStep := make(map[string]int)
	for _, step := range run.Steps {
		for _, observation := range step.Observations {
			result, action, ok := trustedWebSearchObservation(step, observation)
			if !ok {
				continue
			}
			for _, item := range result.Items {
				reference, ok := canonicalPublicWebURL(item.URL)
				if !ok {
					continue
				}
				item.URL = reference
				digest := digestWebSearchItem(result.Provider, result.Query, item)
				searches = append(searches, webResearchRecord{
					Source: webSearchTool, Digest: digest, Reference: reference,
					StepIndex: step.Index, ActionID: action.ID, CapturedAt: step.FinishedAt,
				})
				if previous, exists := firstSearchStep[reference]; !exists || step.Index < previous {
					firstSearchStep[reference] = step.Index
				}
			}
		}
	}

	pages := make([]webResearchRecord, 0)
	for _, step := range run.Steps {
		for _, observation := range step.Observations {
			result, action, ok := trustedWebPageObservation(step, observation)
			if !ok {
				continue
			}
			reference, ok := canonicalPublicWebURL(result.URL)
			if !ok {
				continue
			}
			searchStep, discovered := firstSearchStep[reference]
			if !discovered || searchStep >= step.Index {
				continue
			}
			result.URL = reference
			pages = append(pages, webResearchRecord{
				Source: webPageTool, Digest: digestWebPage(result), Reference: reference,
				StepIndex: step.Index, ActionID: action.ID, CapturedAt: step.FinishedAt,
			})
		}
	}
	return uniqueWebResearchRecords(searches), uniqueWebResearchRecords(pages)
}

func trustedWebSearchObservation(
	step agentRuntime.Step,
	observation agentRuntime.Observation,
) (WebSearchResult, agentRuntime.Action, bool) {
	if observation.IsError || observation.Name != webSearchTool ||
		len(observation.StructuredContent) == 0 ||
		len(observation.StructuredContent) > maxGoalStructuredEvidenceSize {
		return WebSearchResult{}, agentRuntime.Action{}, false
	}
	for _, action := range step.Actions {
		if action.ID != observation.ActionID || action.Type != agentRuntime.ActionToolCall ||
			action.Name != webSearchTool {
			continue
		}
		var arguments struct {
			Query string `json:"query"`
		}
		var result WebSearchResult
		if json.Unmarshal(action.Arguments, &arguments) != nil ||
			json.Unmarshal(observation.StructuredContent, &result) != nil ||
			result.Schema != WebSearchSchema || strings.TrimSpace(result.Provider) == "" ||
			normalizeWebQuery(arguments.Query) == "" ||
			normalizeWebQuery(arguments.Query) != normalizeWebQuery(result.Query) {
			return WebSearchResult{}, agentRuntime.Action{}, false
		}
		result.Provider = strings.TrimSpace(result.Provider)
		result.Query = normalizeWebQuery(result.Query)
		return result, action, true
	}
	return WebSearchResult{}, agentRuntime.Action{}, false
}

func trustedWebPageObservation(
	step agentRuntime.Step,
	observation agentRuntime.Observation,
) (WebPageResult, agentRuntime.Action, bool) {
	if observation.IsError || observation.Name != webPageTool ||
		len(observation.StructuredContent) == 0 ||
		len(observation.StructuredContent) > maxGoalStructuredEvidenceSize {
		return WebPageResult{}, agentRuntime.Action{}, false
	}
	for _, action := range step.Actions {
		if action.ID != observation.ActionID || action.Type != agentRuntime.ActionToolCall ||
			action.Name != webPageTool {
			continue
		}
		var arguments struct {
			URL string `json:"url"`
		}
		var result WebPageResult
		if json.Unmarshal(action.Arguments, &arguments) != nil ||
			json.Unmarshal(observation.StructuredContent, &result) != nil ||
			result.Schema != WebPageSchema || strings.TrimSpace(result.Content) == "" ||
			!supportedWebPageContentType(result.ContentType) {
			return WebPageResult{}, agentRuntime.Action{}, false
		}
		actionURL, actionOK := canonicalPublicWebURL(arguments.URL)
		resultURL, resultOK := canonicalPublicWebURL(result.URL)
		if !actionOK || !resultOK || actionURL != resultURL {
			return WebPageResult{}, agentRuntime.Action{}, false
		}
		result.URL = resultURL
		result.Content = strings.TrimSpace(result.Content)
		return result, action, true
	}
	return WebPageResult{}, agentRuntime.Action{}, false
}

func canonicalPublicWebURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") ||
		strings.HasSuffix(hostname, ".local") {
		return "", false
	}
	if address, parseErr := netip.ParseAddr(hostname); parseErr == nil &&
		(address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() ||
			address.IsLinkLocalMulticast() || address.IsUnspecified() || address.IsMulticast()) {
		return "", false
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return parsed.String(), true
}

func normalizeWebQuery(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func supportedWebPageContentType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "text/html", "application/xhtml+xml", "text/plain":
		return true
	default:
		return false
	}
}

func digestWebSearchItem(provider, query string, item WebSearchEvidence) string {
	canonical, _ := json.Marshal(struct {
		Provider string            `json:"provider"`
		Query    string            `json:"query"`
		Item     WebSearchEvidence `json:"item"`
	}{Provider: provider, Query: query, Item: item})
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func digestWebPage(result WebPageResult) string {
	canonical, _ := json.Marshal(result)
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func uniqueWebResearchRecords(records []webResearchRecord) []webResearchRecord {
	result := make([]webResearchRecord, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		key := record.Source + "|" + record.Digest + "|" + record.Reference
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, record)
	}
	return result
}
