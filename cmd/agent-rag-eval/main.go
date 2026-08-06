package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"twitter-clone/internal/module/agent/eval"
	agentModel "twitter-clone/internal/module/agent/model"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"
)

type providerStats struct {
	mu             sync.Mutex
	requests       int
	failedRequests int
}

func (s *providerStats) record(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	if err != nil {
		s.failedRequests++
	}
}

func (s *providerStats) report(name, provider, endpoint, model string) eval.ProviderReport {
	result := eval.ProviderReport{Name: name, Provider: provider, Endpoint: endpoint, Model: model}
	if s == nil {
		return result
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result.Requests = s.requests
	result.FailedRequests = s.failedRequests
	if result.Requests > 0 {
		result.FailureRate = float64(result.FailedRequests) / float64(result.Requests)
	}
	return result
}

type embeddingClient interface {
	GetEmbedding(context.Context, string, string) ([]float32, error)
}

type bm25Retriever struct {
	client *es.Client
	stats  *providerStats
}

func (r bm25Retriever) Name() string { return "bm25" }

func (r bm25Retriever) Retrieve(ctx context.Context, query string, limit int) ([]eval.Document, error) {
	docs, err := r.client.SearchTweets(ctx, query, 1, limit)
	r.stats.record(err)
	if err != nil {
		return nil, err
	}
	result := make([]eval.Document, 0, len(docs))
	for _, doc := range docs {
		result = append(result, eval.Document{ID: doc.ID, Text: doc.Content})
	}
	return result, nil
}

type vectorRetriever struct {
	client         *qdrant.Client
	aiClient       embeddingClient
	model          string
	collection     string
	embeddingStats *providerStats
	vectorStats    *providerStats
}

type rerankRetriever struct {
	source   eval.Retriever
	reranker ai.Reranker
	stats    *providerStats
}

func (r rerankRetriever) Name() string { return "rrf_rerank" }

func (r rerankRetriever) Retrieve(ctx context.Context, query string, limit int) ([]eval.Document, error) {
	docs, err := r.source.Retrieve(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	input := make([]ai.Document, 0, len(docs))
	for _, doc := range docs {
		input = append(input, ai.Document{ID: doc.ID, Text: doc.Text})
	}
	ranked, err := r.reranker.Rerank(ctx, query, input)
	r.stats.record(err)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Document.ID < ranked[j].Document.ID
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	result := make([]eval.Document, 0, len(ranked))
	for _, item := range ranked {
		result = append(result, eval.Document{ID: item.Document.ID, Text: item.Document.Text})
	}
	return result, nil
}

func (r vectorRetriever) Name() string { return "vector" }

func (r vectorRetriever) Retrieve(ctx context.Context, query string, limit int) ([]eval.Document, error) {
	vector, err := r.aiClient.GetEmbedding(ctx, query, r.model)
	r.embeddingStats.record(err)
	if err != nil {
		return nil, err
	}
	hits, err := r.client.Search(ctx, r.collection, vector, limit)
	r.vectorStats.record(err)
	if err != nil {
		return nil, err
	}
	result := make([]eval.Document, 0, len(hits))
	for _, hit := range hits {
		text, _ := hit.Payload["content"].(string)
		result = append(result, eval.Document{ID: hit.ID, Text: text})
	}
	return result, nil
}

func main() {
	datasetPath := flag.String("dataset", "internal/module/agent/eval/testdata/rag_cases.json", "evaluation dataset path")
	outPath := flag.String("out", "", "report output path; empty writes JSON to stdout")
	k := flag.Int("k", 10, "top-k results per strategy")
	strategiesFlag := flag.String("strategies", envOr("AGENT_RAG_EVAL_STRATEGIES", "bm25,vector,rrf,rrf_rerank"), "comma-separated strategies: bm25, vector, rrf, rrf_rerank")
	collection := flag.String("collection", envOr("AGENT_RAG_EVAL_COLLECTION", "tweets"), "Qdrant collection")
	allowLive := flag.Bool("allow-live", false, "allow connections to configured ES, Qdrant and model providers")
	caseTimeout := flag.Duration("case-timeout", envDuration("AGENT_RAG_EVAL_CASE_TIMEOUT", 15*time.Second), "timeout per retrieval case")
	overallTimeout := flag.Duration("timeout", envDuration("AGENT_RAG_EVAL_TIMEOUT", 10*time.Minute), "overall evaluation timeout")
	flag.Parse()

	if *k <= 0 {
		fatalf("k must be positive")
	}
	strategyNames, err := parseStrategies(*strategiesFlag)
	if err != nil {
		fatalf("invalid strategies: %v", err)
	}
	if !*allowLive {
		fatalf("RAG evaluation requires explicit --allow-live before connecting to external services")
	}
	if *caseTimeout <= 0 || *overallTimeout <= 0 {
		fatalf("timeouts must be positive")
	}

	esAddresses := splitCSV(envOr("ES_ADDRESSES", "http://localhost:9200"))
	esCloudID := strings.TrimSpace(os.Getenv("ES_CLOUD_ID"))
	requirements := strategyRequirements(strategyNames)
	needBM25 := requirements.needBM25
	needVector := requirements.needVector
	needReranker := requirements.needReranker
	needRRF := requirements.needRRF
	if needBM25 && esCloudID != "" {
		fatalf("ES_CLOUD_ID is not supported by the controlled RAG eval runner; use ES_ADDRESSES so the endpoint can be policy-validated")
	}
	qdrantURL := envOr("QDRANT_URL", "http://localhost:6333")
	embeddingProvider := envOr("AGENT_RAG_EVAL_EMBEDDING_PROVIDER", "lmstudio")
	embeddingURL := envOr("AGENT_RAG_EVAL_EMBEDDING_BASE_URL", envOr("LM_STUDIO_API_URL", "http://localhost:1234/v1"))
	embeddingModel := envOr("AGENT_RAG_EVAL_EMBEDDING_MODEL", envOr("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3"))
	rerankerType := envOr("RERANKER_TYPE", "local")
	rerankerURL, rerankerModel := rerankerConfig(rerankerType)
	policy := newRAGEvalEndpointPolicy()
	if needBM25 {
		for _, address := range esAddresses {
			if err := policy.Validate(address, "elasticsearch"); err != nil {
				fatalf("ES endpoint %q rejected: %v", address, err)
			}
		}
	}
	if needVector {
		if err := policy.Validate(qdrantURL, "qdrant"); err != nil {
			fatalf("Qdrant endpoint rejected: %v", err)
		}
		if err := policy.Validate(embeddingURL, embeddingProvider); err != nil {
			fatalf("embedding endpoint rejected: %v", err)
		}
	}
	if needReranker && !strings.EqualFold(rerankerType, "local") {
		if err := policy.Validate(rerankerURL, rerankerType); err != nil {
			fatalf("reranker endpoint rejected: %v", err)
		}
	}

	file, err := os.Open(*datasetPath)
	if err != nil {
		fatalf("open dataset: %v", err)
	}
	dataset, err := eval.LoadDataset(file)
	_ = file.Close()
	if err != nil {
		fatalf("load dataset: %v", err)
	}

	bm25Stats := &providerStats{}
	embeddingStats := &providerStats{}
	vectorStats := &providerStats{}
	rerankerStats := &providerStats{}
	var bm25 eval.Retriever
	if needBM25 {
		esClient, err := es.NewClient(es.Config{
			Addresses:  esAddresses,
			Username:   envOr("ES_USERNAME", ""),
			Password:   envOr("ES_PASSWORD", ""),
			CloudID:    esCloudID,
			APIKey:     envOr("ES_API_KEY", ""),
			HTTPClient: restrictedHTTPClient(policy, "elasticsearch", 5*time.Second),
		})
		if err != nil {
			fatalf("initialize Elasticsearch: %v", err)
		}
		bm25 = bm25Retriever{client: esClient, stats: bm25Stats}
	}

	var vector eval.Retriever
	if needVector {
		qdrantClient := qdrant.NewClientWithHTTPClient(qdrantURL, restrictedHTTPClient(policy, "qdrant", 3*time.Second))
		embeddingClient := ai.NewClientWithConfig(
			embeddingURL,
			readAPIKey("AGENT_RAG_EVAL_EMBEDDING_API_KEY", "LM_STUDIO_API_KEY", "DASHSCOPE_API_KEY"),
			restrictedHTTPClient(policy, embeddingProvider, 15*time.Second),
		)
		vector = vectorRetriever{
			client:         qdrantClient,
			aiClient:       embeddingClient,
			model:          embeddingModel,
			collection:     strings.TrimSpace(*collection),
			embeddingStats: embeddingStats,
			vectorStats:    vectorStats,
		}
	}

	var rrf *eval.RRFRetriever
	if needRRF {
		rrf = eval.NewRRFRetriever("rrf", bm25, vector)
	}
	var reranked eval.Retriever
	if needReranker {
		reranked = rerankRetriever{
			source: rrf,
			reranker: ai.NewRerankerWithHTTPClient(
				rerankerType,
				readAPIKey("RERANKER_API_KEY", "DASHSCOPE_API_KEY", "SILICONFLOW_API_KEY"),
				rerankerURL,
				rerankerModel,
				restrictedHTTPClient(policy, rerankerType, 1500*time.Millisecond),
			),
			stats: rerankerStats,
		}
	}

	strategies := make([]eval.Strategy, 0, len(strategyNames))
	for _, name := range strategyNames {
		switch name {
		case "bm25":
			strategies = append(strategies, eval.Strategy{Name: name, Retriever: bm25})
		case "vector":
			strategies = append(strategies, eval.Strategy{Name: name, Retriever: vector})
		case "rrf":
			strategies = append(strategies, eval.Strategy{Name: name, Retriever: rrf})
		case "rrf_rerank":
			strategies = append(strategies, eval.Strategy{Name: name, Retriever: reranked})
		}
	}
	providerReports := func() []eval.ProviderReport {
		result := make([]eval.ProviderReport, 0, 4)
		if needBM25 {
			result = append(result, bm25Stats.report("elasticsearch", "elasticsearch", strings.Join(esAddresses, ","), ""))
		}
		if needVector {
			result = append(result,
				vectorStats.report("qdrant", "qdrant", qdrantURL, strings.TrimSpace(*collection)),
				embeddingStats.report("embedding", embeddingProvider, embeddingURL, embeddingModel),
			)
		}
		if needReranker {
			result = append(result, rerankerStats.report("reranker", rerankerType, rerankerURL, rerankerModel))
		}
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), *overallTimeout)
	defer cancel()
	report, err := eval.Run(ctx, dataset, strategies, eval.RunnerConfig{
		K:                *k,
		DatasetVersion:   envOr("AGENT_RAG_EVAL_DATASET_VERSION", "rag_cases-v1"),
		EmbeddingModel:   embeddingModel,
		EmbeddingVersion: envOr("AGENT_EPISODIC_EMBEDDING_VERSION", "v1"),
		Environment:      envOr("APP_ENV", "local"),
		Seed:             0,
		CaseTimeout:      *caseTimeout,
		Providers:        providerReports(),
	})
	if err != nil {
		fatalf("run evaluation: %v", err)
	}
	// Provider counters are populated while the strategies execute, so refresh
	// the report after Run instead of freezing zero-valued metadata at startup.
	report.Providers = providerReports()

	if strings.TrimSpace(*outPath) == "" {
		if err := eval.WriteReport(os.Stdout, report); err != nil {
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
	if err := eval.WriteReport(out, report); err != nil {
		fatalf("write report: %v", err)
	}
	fmt.Printf("RAG evaluation report written to %s\n", *outPath)
}

func newRAGEvalEndpointPolicy() *agentModel.EndpointPolicy {
	hosts := splitCSV(envOr("AGENT_RAG_EVAL_ALLOWED_HOSTS", "localhost,127.0.0.1,::1"))
	return agentModel.NewEndpointPolicy(hosts...)
}

func restrictedHTTPClient(policy *agentModel.EndpointPolicy, provider string, timeout time.Duration) *http.Client {
	client := agentModel.NewRestrictedHTTPClient(policy, provider)
	client.Timeout = timeout
	return client
}

func rerankerConfig(rerankerType string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(rerankerType)) {
	case "dashscope":
		return envOr("RERANKER_API_URL", "https://dashscope.aliyuncs.com/api/v1/services/rerank/text/rerank"), envOr("RERANKER_MODEL", "gte-rerank")
	case "siliconflow":
		return envOr("RERANKER_API_URL", "https://api.siliconflow.cn/v1/rerank"), envOr("RERANKER_MODEL", "BAAI/bge-reranker-v2-m3")
	default:
		return "", ""
	}
}

func readAPIKey(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseStrategies(value string) ([]string, error) {
	allowed := []string{"bm25", "vector", "rrf", "rrf_rerank"}
	requested := make(map[string]struct{}, len(allowed))
	for _, item := range splitCSV(value) {
		name := strings.ToLower(item)
		if !hasStrategy(allowed, name) {
			return nil, fmt.Errorf("unsupported strategy %q", item)
		}
		requested[name] = struct{}{}
	}
	if len(requested) == 0 {
		return nil, fmt.Errorf("at least one strategy is required")
	}
	result := make([]string, 0, len(requested))
	for _, name := range allowed {
		if _, ok := requested[name]; ok {
			result = append(result, name)
		}
	}
	return result, nil
}

func hasStrategy(strategies []string, target string) bool {
	for _, name := range strategies {
		if name == target {
			return true
		}
	}
	return false
}

type strategyRequirementSet struct {
	needBM25     bool
	needVector   bool
	needRRF      bool
	needReranker bool
}

func strategyRequirements(strategies []string) strategyRequirementSet {
	result := strategyRequirementSet{
		needReranker: hasStrategy(strategies, "rrf_rerank"),
	}
	result.needRRF = hasStrategy(strategies, "rrf") || result.needReranker
	result.needBM25 = hasStrategy(strategies, "bm25") || result.needRRF
	result.needVector = hasStrategy(strategies, "vector") || result.needRRF
	return result
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		fatalf("invalid duration %s=%q: %v", key, value, err)
	}
	return parsed
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "agent-rag-eval: "+format+"\n", args...)
	os.Exit(1)
}
