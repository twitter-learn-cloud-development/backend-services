package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
	"twitter-clone/internal/module/agent/workflow/rag"
)

func main() {
	datasetPath := flag.String("dataset", "internal/module/agent/eval/testdata/router_cases.json", "router evaluation dataset path")
	outPath := flag.String("out", "", "report output path; empty writes JSON to stdout")
	modeFlag := flag.String("mode", envOr("AGENT_ROUTER_EVAL_MODE", "lexical"), "router mode: lexical, semantic, llm, or full")
	allowLive := flag.Bool("allow-live", false, "allow calls to configured Embedding/LLM providers")
	semanticThreshold := flag.Float64("semantic-threshold", envFloat64("AGENT_ROUTER_EVAL_SEMANTIC_THRESHOLD", 0.82), "semantic anchor cosine threshold")
	caseTimeout := flag.Duration("case-timeout", envDuration("AGENT_ROUTER_EVAL_CASE_TIMEOUT", 15*time.Second), "timeout per evaluation case")
	overallTimeout := flag.Duration("timeout", envDuration("AGENT_ROUTER_EVAL_TIMEOUT", 5*time.Minute), "overall evaluation timeout including anchor initialization")
	embeddingProvider := flag.String("embedding-provider", envOr("AGENT_ROUTER_EVAL_EMBEDDING_PROVIDER", "lmstudio"), "embedding provider label")
	embeddingBaseURL := flag.String("embedding-base-url", envOr("AGENT_ROUTER_EVAL_EMBEDDING_BASE_URL", envOr("LM_STUDIO_API_URL", "http://localhost:1234/v1")), "OpenAI-compatible embedding base URL")
	embeddingModel := flag.String("embedding-model", envOr("AGENT_ROUTER_EVAL_EMBEDDING_MODEL", envOr("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3")), "embedding model")
	llmProvider := flag.String("llm-provider", envOr("AGENT_ROUTER_EVAL_LLM_PROVIDER", "dashscope"), "LLM provider label")
	llmBaseURL := flag.String("llm-base-url", envOr("AGENT_ROUTER_EVAL_LLM_BASE_URL", envOr("DASHSCOPE_API_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1")), "OpenAI-compatible LLM base URL")
	llmModel := flag.String("llm-model", envOr("AGENT_ROUTER_EVAL_LLM_MODEL", envOr("DASHSCOPE_MODEL_CHAT", envOr("PREMIUM_AI_MODEL_CHAT", "qwen-plus"))), "LLM model")
	flag.Parse()

	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	if !validRouterMode(mode) {
		fatalf("invalid --mode %q; expected lexical, semantic, llm, or full", mode)
	}
	if *semanticThreshold <= 0 || *semanticThreshold > 1 {
		fatalf("--semantic-threshold must be in (0, 1]")
	}
	if *caseTimeout <= 0 || *overallTimeout <= 0 {
		fatalf("--case-timeout and --timeout must be positive")
	}
	if mode != "lexical" && !*allowLive {
		fatalf("--mode %s requires explicit --allow-live", mode)
	}

	file, err := os.Open(*datasetPath)
	if err != nil {
		fatalf("open dataset: %v", err)
	}
	dataset, err := eval.LoadRouterDataset(file)
	_ = file.Close()
	if err != nil {
		fatalf("load dataset: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *overallTimeout)
	defer cancel()

	execution := eval.RouterExecutionReport{
		Mode:              mode,
		SemanticThreshold: *semanticThreshold,
		CaseTimeoutMS:     caseTimeout.Milliseconds(),
	}
	var liveClient *liveRouterClient
	routerConfig := rag.CascadeRouterConfig{SemanticThreshold: *semanticThreshold}
	if mode != "lexical" {
		liveConfig := liveRouterClientConfig{Timeout: *caseTimeout}
		if mode == "semantic" || mode == "full" {
			liveConfig.Embedding = &liveProviderConfig{
				Provider:                    *embeddingProvider,
				BaseURL:                     *embeddingBaseURL,
				APIKey:                      routerAPIKey("embedding", *embeddingProvider),
				Model:                       *embeddingModel,
				InputMicrosPerMillionTokens: envNonNegativeInt64("AGENT_ROUTER_EVAL_EMBEDDING_INPUT_MICROS_PER_MILLION", 0),
				PricingVersion:              envOr("AGENT_ROUTER_EVAL_EMBEDDING_PRICING_VERSION", "unpriced"),
			}
		}
		if mode == "llm" || mode == "full" {
			inputRate, outputRate, pricingVersion := llmPricing(*llmProvider)
			liveConfig.LLM = &liveProviderConfig{
				Provider:                     *llmProvider,
				BaseURL:                      *llmBaseURL,
				APIKey:                       routerAPIKey("llm", *llmProvider),
				Model:                        *llmModel,
				InputMicrosPerMillionTokens:  inputRate,
				OutputMicrosPerMillionTokens: outputRate,
				PricingVersion:               pricingVersion,
			}
		}
		var err error
		liveClient, err = newLiveRouterClient(liveConfig)
		if err != nil {
			fatalf("configure live router: %v", err)
		}
		if liveConfig.Embedding != nil {
			routerConfig.EmbeddingClient = liveClient
		}
		if liveConfig.LLM != nil {
			routerConfig.ChatClient = liveClient
			routerConfig.ChatModel = strings.TrimSpace(*llmModel)
		}
	}
	router := rag.NewCascadeRouterWithConfig(routerConfig)
	if mode == "semantic" || mode == "full" {
		if err := router.InitSemanticAnchors(ctx, strings.TrimSpace(*embeddingModel)); err != nil {
			execution.InitializationError = err.Error()
		}
	}

	report, err := eval.RunRouter(ctx, dataset, func(ctx context.Context, query string) (eval.RouterDecision, error) {
		decision, err := router.RouteWithMetadata(ctx, query, strings.TrimSpace(*embeddingModel))
		if err != nil {
			return eval.RouterDecision{}, err
		}
		return eval.RouterDecision{
			Intent:         string(decision.Intent),
			Stage:          string(decision.Stage),
			RewrittenQuery: decision.RewrittenQuery,
			SemanticScore:  decision.SemanticScore,
			SemanticError:  decision.SemanticError,
			LLMError:       decision.LLMError,
		}, nil
	}, eval.RouterRunnerConfig{
		DatasetVersion: envOr("AGENT_ROUTER_EVAL_DATASET_VERSION", "router-cases-v1"),
		Environment:    envOr("APP_ENV", "local"),
		Seed:           0,
		CaseTimeout:    *caseTimeout,
		Execution:      execution,
	})
	if err != nil {
		fatalf("run router evaluation: %v", err)
	}
	if liveClient != nil {
		report.Execution.Embedding, report.Execution.LLM = liveClient.reports()
	}

	if strings.TrimSpace(*outPath) == "" {
		if err := eval.WriteRouterReport(os.Stdout, report); err != nil {
			fatalf("write report: %v", err)
		}
		return
	}
	if dir := filepath.Dir(*outPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fatalf("create report directory: %v", err)
		}
	}
	out, err := os.Create(*outPath)
	if err != nil {
		fatalf("create report: %v", err)
	}
	defer out.Close()
	if err := eval.WriteRouterReport(out, report); err != nil {
		fatalf("write report: %v", err)
	}
	fmt.Printf("Router evaluation report written to %s\n", *outPath)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envFloat64(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
	if err != nil {
		return fallback
	}
	return value
}

func envNonNegativeInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func validRouterMode(mode string) bool {
	switch mode {
	case "lexical", "semantic", "llm", "full":
		return true
	default:
		return false
	}
}

func routerAPIKey(kind, provider string) string {
	if value := strings.TrimSpace(os.Getenv("AGENT_ROUTER_EVAL_" + strings.ToUpper(kind) + "_API_KEY")); value != "" {
		return value
	}
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(provider))
	if normalized == "dashscope" {
		return strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	}
	if value := strings.TrimSpace(os.Getenv("LM_STUDIO_API_KEY")); value != "" {
		return value
	}
	if normalized == "lmstudio" || normalized == "local" || normalized == "ollama" {
		return "lm-studio"
	}
	return ""
}

func llmPricing(provider string) (int64, int64, string) {
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(provider))
	prefix := "AGENT_ROUTER_EVAL_LLM"
	defaultInput := int64(0)
	defaultOutput := int64(0)
	defaultVersion := "unpriced"
	switch normalized {
	case "dashscope":
		defaultInput = envNonNegativeInt64("AGENT_DASHSCOPE_INPUT_MICROS_PER_MILLION", 0)
		defaultOutput = envNonNegativeInt64("AGENT_DASHSCOPE_OUTPUT_MICROS_PER_MILLION", 0)
		defaultVersion = envOr("AGENT_DASHSCOPE_PRICING_VERSION", "unpriced")
	case "lmstudio", "local", "ollama":
		defaultInput = envNonNegativeInt64("AGENT_LMSTUDIO_INPUT_MICROS_PER_MILLION", 0)
		defaultOutput = envNonNegativeInt64("AGENT_LMSTUDIO_OUTPUT_MICROS_PER_MILLION", 0)
		defaultVersion = envOr("AGENT_LMSTUDIO_PRICING_VERSION", "local-v1")
	}
	return envNonNegativeInt64(prefix+"_INPUT_MICROS_PER_MILLION", defaultInput),
		envNonNegativeInt64(prefix+"_OUTPUT_MICROS_PER_MILLION", defaultOutput),
		envOr(prefix+"_PRICING_VERSION", defaultVersion)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "agent-router-eval: "+format+"\n", args...)
	os.Exit(1)
}
