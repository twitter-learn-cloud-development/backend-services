package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

type RouterCase struct {
	ID             string `json:"id"`
	Query          string `json:"query"`
	Category       string `json:"category,omitempty"`
	ExpectedIntent string `json:"expected_intent"`
	ExpectedLayer  string `json:"expected_layer,omitempty"`
}

type RouterDecision struct {
	Intent         string
	Layer          string
	Stage          string
	RewrittenQuery string
	SemanticScore  float64
	SemanticError  string
	LLMError       string
}

type RouterClassifier func(context.Context, string) (RouterDecision, error)

type RouterCaseResult struct {
	CaseID         string  `json:"case_id"`
	Query          string  `json:"query"`
	Category       string  `json:"category,omitempty"`
	ExpectedIntent string  `json:"expected_intent"`
	ActualIntent   string  `json:"actual_intent,omitempty"`
	ExpectedLayer  string  `json:"expected_layer,omitempty"`
	ActualLayer    string  `json:"actual_layer,omitempty"`
	Stage          string  `json:"stage,omitempty"`
	RewrittenQuery string  `json:"rewritten_query,omitempty"`
	SemanticScore  float64 `json:"semantic_score,omitempty"`
	SemanticError  string  `json:"semantic_error,omitempty"`
	LLMError       string  `json:"llm_error,omitempty"`
	Correct        bool    `json:"correct"`
	DurationMS     int64   `json:"duration_ms"`
	Error          string  `json:"error,omitempty"`
}

type RouterLayerMetrics struct {
	Cases        int     `json:"cases"`
	Correct      int     `json:"correct"`
	Misrouted    int     `json:"misrouted"`
	Errors       int     `json:"errors"`
	Accuracy     float64 `json:"accuracy"`
	MisrouteRate float64 `json:"misroute_rate"`
}

type RouterStageMetrics struct {
	Cases    int     `json:"cases"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
	Share    float64 `json:"share"`
}

type RouterMetrics struct {
	Cases              int                           `json:"cases"`
	Correct            int                           `json:"correct"`
	Misrouted          int                           `json:"misrouted"`
	Errors             int                           `json:"errors"`
	Accuracy           float64                       `json:"accuracy"`
	MisrouteRate       float64                       `json:"misroute_rate"`
	ErrorRate          float64                       `json:"error_rate"`
	ProviderErrorCases int                           `json:"provider_error_cases"`
	SemanticErrorCases int                           `json:"semantic_error_cases"`
	LLMErrorCases      int                           `json:"llm_error_cases"`
	ProviderErrorRate  float64                       `json:"provider_error_rate"`
	P50MS              int64                         `json:"p50_ms"`
	P95MS              int64                         `json:"p95_ms"`
	ByLayer            map[string]RouterLayerMetrics `json:"by_layer"`
	ByStage            map[string]RouterStageMetrics `json:"by_stage"`
}

type RouterProviderReport struct {
	Provider            string `json:"provider,omitempty"`
	Endpoint            string `json:"endpoint,omitempty"`
	Model               string `json:"model,omitempty"`
	Requests            int    `json:"requests"`
	FailedRequests      int    `json:"failed_requests"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	TotalTokens         int    `json:"total_tokens"`
	EstimatedCostMicros int64  `json:"estimated_cost_micros"`
	PricingVersion      string `json:"pricing_version,omitempty"`
}

type RouterExecutionReport struct {
	Mode                string               `json:"mode,omitempty"`
	SemanticThreshold   float64              `json:"semantic_threshold,omitempty"`
	CaseTimeoutMS       int64                `json:"case_timeout_ms,omitempty"`
	InitializationError string               `json:"initialization_error,omitempty"`
	Embedding           RouterProviderReport `json:"embedding,omitempty"`
	LLM                 RouterProviderReport `json:"llm,omitempty"`
}

type RouterRunnerConfig struct {
	DatasetVersion string
	Environment    string
	Seed           int64
	CaseTimeout    time.Duration
	Execution      RouterExecutionReport
	Now            func() time.Time
}

type RouterReport struct {
	GeneratedAt    time.Time             `json:"generated_at"`
	DatasetVersion string                `json:"dataset_version,omitempty"`
	Environment    string                `json:"environment,omitempty"`
	Seed           int64                 `json:"seed"`
	Execution      RouterExecutionReport `json:"execution"`
	Metrics        RouterMetrics         `json:"metrics"`
	CaseResults    []RouterCaseResult    `json:"case_results"`
}

func LoadRouterDataset(reader io.Reader) ([]RouterCase, error) {
	if reader == nil {
		return nil, fmt.Errorf("router evaluation dataset reader is nil")
	}
	var dataset []RouterCase
	if _, err := decodeBoundedEvaluationJSON(reader, &dataset, "router evaluation dataset"); err != nil {
		return nil, err
	}
	if err := validateEvaluationCaseCount(len(dataset), "router evaluation dataset"); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(dataset))
	for index := range dataset {
		sample := dataset[index]
		sample.ID = strings.TrimSpace(sample.ID)
		sample.Query = strings.TrimSpace(sample.Query)
		sample.Category = strings.TrimSpace(sample.Category)
		sample.ExpectedIntent = strings.TrimSpace(sample.ExpectedIntent)
		sample.ExpectedLayer = strings.TrimSpace(sample.ExpectedLayer)
		if sample.ID == "" || sample.Query == "" || sample.ExpectedIntent == "" {
			return nil, fmt.Errorf("router evaluation case %d is missing id, query or expected_intent", index)
		}
		if utf8.RuneCountInString(sample.ID) > maxEvaluationIdentifierRunes ||
			utf8.RuneCountInString(sample.Category) > maxEvaluationIdentifierRunes ||
			utf8.RuneCountInString(sample.ExpectedIntent) > maxEvaluationIdentifierRunes ||
			utf8.RuneCountInString(sample.ExpectedLayer) > maxEvaluationIdentifierRunes ||
			utf8.RuneCountInString(sample.Query) > maxEvaluationTextRunes {
			return nil, fmt.Errorf("router evaluation case %d exceeds string limits", index)
		}
		if _, exists := seen[sample.ID]; exists {
			return nil, fmt.Errorf("router evaluation dataset contains duplicate id %q", sample.ID)
		}
		seen[sample.ID] = struct{}{}
		derivedLayer := RouteLayer(sample.ExpectedIntent)
		if derivedLayer == "unknown" {
			return nil, fmt.Errorf("router evaluation case %d has unsupported expected_intent %q", index, sample.ExpectedIntent)
		}
		if sample.ExpectedLayer == "" {
			sample.ExpectedLayer = derivedLayer
		} else if sample.ExpectedLayer != derivedLayer {
			return nil, fmt.Errorf("router evaluation case %d expected_layer %q does not match intent %q", index, sample.ExpectedLayer, sample.ExpectedIntent)
		}
		dataset[index] = sample
	}
	return dataset, nil
}

func RunRouter(ctx context.Context, dataset []RouterCase, classifier RouterClassifier, cfg RouterRunnerConfig) (RouterReport, error) {
	if ctx == nil {
		return RouterReport{}, fmt.Errorf("router evaluation context is nil")
	}
	if classifier == nil {
		return RouterReport{}, fmt.Errorf("router evaluation classifier is nil")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	report := RouterReport{
		GeneratedAt:    now().UTC(),
		DatasetVersion: cfg.DatasetVersion,
		Environment:    cfg.Environment,
		Seed:           cfg.Seed,
		Execution:      cfg.Execution,
		CaseResults:    make([]RouterCaseResult, 0, len(dataset)),
		Metrics: RouterMetrics{
			Cases:   len(dataset),
			ByLayer: make(map[string]RouterLayerMetrics),
			ByStage: make(map[string]RouterStageMetrics),
		},
	}
	durations := make([]int64, 0, len(dataset))

	for _, sample := range dataset {
		started := time.Now()
		caseCtx := ctx
		cancel := func() {}
		if cfg.CaseTimeout > 0 {
			caseCtx, cancel = context.WithTimeout(ctx, cfg.CaseTimeout)
		}
		decision, err := classifier(caseCtx, sample.Query)
		cancel()
		elapsed := time.Since(started).Milliseconds()
		if elapsed < 0 {
			elapsed = 0
		}
		durations = append(durations, elapsed)
		result := RouterCaseResult{
			CaseID:         sample.ID,
			Query:          sample.Query,
			Category:       sample.Category,
			ExpectedIntent: sample.ExpectedIntent,
			ExpectedLayer:  sample.ExpectedLayer,
			DurationMS:     elapsed,
		}
		if err != nil {
			result.Error = err.Error()
			report.Metrics.Errors++
			layer := report.Metrics.ByLayer[sample.ExpectedLayer]
			layer.Cases++
			layer.Errors++
			report.Metrics.ByLayer[sample.ExpectedLayer] = layer
		} else {
			result.ActualIntent = decision.Intent
			result.ActualLayer = decision.Layer
			if result.ActualLayer == "" {
				result.ActualLayer = RouteLayer(decision.Intent)
			}
			result.Stage = decision.Stage
			result.RewrittenQuery = decision.RewrittenQuery
			result.SemanticScore = decision.SemanticScore
			result.SemanticError = decision.SemanticError
			result.LLMError = decision.LLMError
			if result.SemanticError != "" {
				report.Metrics.SemanticErrorCases++
			}
			if result.LLMError != "" {
				report.Metrics.LLMErrorCases++
			}
			if result.SemanticError != "" || result.LLMError != "" {
				report.Metrics.ProviderErrorCases++
			}
			result.Correct = decision.Intent == sample.ExpectedIntent && result.ActualLayer == sample.ExpectedLayer
			if result.Correct {
				report.Metrics.Correct++
			} else {
				report.Metrics.Misrouted++
			}
			stage := report.Metrics.ByStage[result.Stage]
			stage.Cases++
			if result.Correct {
				stage.Correct++
			}
			report.Metrics.ByStage[result.Stage] = stage

			layer := report.Metrics.ByLayer[sample.ExpectedLayer]
			layer.Cases++
			if result.Correct {
				layer.Correct++
			} else {
				layer.Misrouted++
			}
			report.Metrics.ByLayer[sample.ExpectedLayer] = layer
		}
		report.CaseResults = append(report.CaseResults, result)
	}

	if report.Metrics.Cases > 0 {
		denominator := float64(report.Metrics.Cases)
		report.Metrics.Accuracy = float64(report.Metrics.Correct) / denominator
		report.Metrics.MisrouteRate = float64(report.Metrics.Misrouted) / denominator
		report.Metrics.ErrorRate = float64(report.Metrics.Errors) / denominator
		report.Metrics.ProviderErrorRate = float64(report.Metrics.ProviderErrorCases) / denominator
	}
	report.Metrics.P50MS = percentile(durations, 0.50)
	report.Metrics.P95MS = percentile(durations, 0.95)
	for layer, metrics := range report.Metrics.ByLayer {
		if metrics.Cases > 0 {
			metrics.Accuracy = float64(metrics.Correct) / float64(metrics.Cases)
			metrics.MisrouteRate = float64(metrics.Misrouted) / float64(metrics.Cases)
		}
		report.Metrics.ByLayer[layer] = metrics
	}
	for stage, metrics := range report.Metrics.ByStage {
		if metrics.Cases > 0 {
			metrics.Accuracy = float64(metrics.Correct) / float64(metrics.Cases)
		}
		if report.Metrics.Cases > 0 {
			metrics.Share = float64(metrics.Cases) / float64(report.Metrics.Cases)
		}
		report.Metrics.ByStage[stage] = metrics
	}
	return report, nil
}

func WriteRouterReport(writer io.Writer, report RouterReport) error {
	if writer == nil {
		return fmt.Errorf("router evaluation report writer is nil")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode router evaluation report: %w", err)
	}
	return nil
}

func RouteLayer(intent string) string {
	switch intent {
	case "persona_memory":
		return "l1_persona"
	case "episodic":
		return "l2_episodic"
	case "global":
		return "l3_global"
	case "direct_action":
		return "action"
	default:
		return "unknown"
	}
}
