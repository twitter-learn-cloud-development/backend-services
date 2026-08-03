package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"
)

const (
	defaultSimilarityThreshold      = 0.65
	defaultMemoryBudgetTokens       = 1200
	defaultPersonaBudgetTokens      = 300
	DefaultEpisodicCollectionName   = "agent_episodic_memory"
	DefaultEpisodicEmbeddingVersion = "v1"
)

// EpisodicMemoryConfig keeps the physical collection and embedding identity
// explicit. New writes always target the shared collection; legacy reads are a
// bounded compatibility path for users that have not been migrated yet.
type EpisodicMemoryConfig struct {
	CollectionName       string
	EmbeddingVersion     string
	LegacyReadEnabled    bool
	LegacyCollectionName func(userID uint64) string
}

type ScoringConfig struct {
	SimilarityThreshold float64
	SimilarityWeight    float64
	TimeDecayWeight     float64
	FrequencyWeight     float64
	TimeDecayLambda     float64
	KeywordBonus        float64
}

func DefaultScoringConfig() ScoringConfig {
	return ScoringConfig{
		SimilarityThreshold: defaultSimilarityThreshold,
		SimilarityWeight:    0.60,
		TimeDecayWeight:     0.25,
		FrequencyWeight:     0.15,
		TimeDecayLambda:     0.00001,
		KeywordBonus:        0.15,
	}
}

func DefaultEpisodicMemoryConfig() EpisodicMemoryConfig {
	return EpisodicMemoryConfig{
		CollectionName:    DefaultEpisodicCollectionName,
		EmbeddingVersion:  DefaultEpisodicEmbeddingVersion,
		LegacyReadEnabled: true,
		LegacyCollectionName: func(userID uint64) string {
			return fmt.Sprintf("episodic_user_%d", userID)
		},
	}
}

type UserPersona struct {
	UserID    uint64 `gorm:"primaryKey;column:user_id"`
	Persona   string `gorm:"column:persona;type:text"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

func (UserPersona) TableName() string {
	return "agent_personas"
}

type MemoryManager struct {
	db           *gorm.DB
	esClient     *es.Client
	qdrantClient *qdrant.Client
	aiClient     *ai.Client
	chatModel    string
	embedModel   string
	tokenCounter agentRuntime.TokenCounter
	episodic     EpisodicMemoryConfig
	scoring      ScoringConfig
}

type MemoryManagerOption func(*MemoryManager)

func WithEpisodicMemoryConfig(config EpisodicMemoryConfig) MemoryManagerOption {
	return func(manager *MemoryManager) {
		defaults := DefaultEpisodicMemoryConfig()
		if strings.TrimSpace(config.CollectionName) != "" {
			defaults.CollectionName = strings.TrimSpace(config.CollectionName)
		}
		if strings.TrimSpace(config.EmbeddingVersion) != "" {
			defaults.EmbeddingVersion = strings.TrimSpace(config.EmbeddingVersion)
		}
		defaults.LegacyReadEnabled = config.LegacyReadEnabled
		if config.LegacyCollectionName != nil {
			defaults.LegacyCollectionName = config.LegacyCollectionName
		}
		manager.episodic = defaults
	}
}

func WithScoringConfig(config ScoringConfig) MemoryManagerOption {
	return func(manager *MemoryManager) {
		defaults := DefaultScoringConfig()
		if config.SimilarityThreshold > 0 {
			defaults.SimilarityThreshold = config.SimilarityThreshold
		}
		if config.SimilarityWeight > 0 {
			defaults.SimilarityWeight = config.SimilarityWeight
		}
		if config.TimeDecayWeight > 0 {
			defaults.TimeDecayWeight = config.TimeDecayWeight
		}
		if config.FrequencyWeight > 0 {
			defaults.FrequencyWeight = config.FrequencyWeight
		}
		if config.TimeDecayLambda > 0 {
			defaults.TimeDecayLambda = config.TimeDecayLambda
		}
		if config.KeywordBonus > 0 {
			defaults.KeywordBonus = config.KeywordBonus
		}
		manager.scoring = defaults
	}
}

func NewMemoryManager(
	db *gorm.DB,
	esClient *es.Client,
	qdrantClient *qdrant.Client,
	aiClient *ai.Client,
	chatModel string,
	embedModel string,
	options ...MemoryManagerOption,
) *MemoryManager {
	if db != nil {
		_ = db.AutoMigrate(&UserPersona{})
	}

	manager := &MemoryManager{
		db:           db,
		esClient:     esClient,
		qdrantClient: qdrantClient,
		aiClient:     aiClient,
		chatModel:    chatModel,
		embedModel:   embedModel,
		tokenCounter: agentRuntime.NewHeuristicTokenCounter(),
		episodic:     DefaultEpisodicMemoryConfig(),
		scoring:      DefaultScoringConfig(),
	}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	return manager
}

func (m *MemoryManager) SetTokenCounter(counter agentRuntime.TokenCounter) {
	if m == nil || counter == nil {
		return
	}
	m.tokenCounter = counter
}

func (m *MemoryManager) GetPersona(ctx context.Context, userID uint64) (string, error) {
	if m == nil || m.db == nil {
		return "", nil
	}

	var up UserPersona
	err := m.db.WithContext(ctx).First(&up, "user_id = ?", userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return up.Persona, nil
}

func (m *MemoryManager) SavePersona(ctx context.Context, userID uint64, persona string) error {
	if m == nil || m.db == nil {
		return errors.New("memory persona db is not initialized")
	}
	up := UserPersona{
		UserID:    userID,
		Persona:   strings.TrimSpace(persona),
		UpdatedAt: time.Now().Unix(),
	}
	return m.db.WithContext(ctx).Save(&up).Error
}

type SessionSummaryRequest struct {
	UserID          uint64
	PointID         uint64
	SourceDialogue  string
	SummaryVersion  int
	DialogueHistory string
}

type EpisodicSummary struct {
	MemoryType  string   `json:"memory_type"`
	Summary     string   `json:"summary"`
	Facts       []string `json:"facts"`
	Preferences []string `json:"preferences"`
	Decisions   []string `json:"decisions"`
	Followups   []string `json:"followups"`
}

func (m *MemoryManager) SaveEpisodicMemory(ctx context.Context, userID uint64, sessionID uint64, dialogHistory string) error {
	return m.SaveSessionSummary(ctx, SessionSummaryRequest{
		UserID:          userID,
		PointID:         sessionID,
		SourceDialogue:  fmt.Sprintf("legacy:%d", sessionID),
		SummaryVersion:  1,
		DialogueHistory: dialogHistory,
	})
}

func (m *MemoryManager) SaveSessionSummary(ctx context.Context, request SessionSummaryRequest) error {
	if m == nil || m.aiClient == nil || m.qdrantClient == nil {
		return errors.New("episodic memory dependencies are not initialized")
	}
	if strings.TrimSpace(request.DialogueHistory) == "" {
		return nil
	}
	if request.SummaryVersion <= 0 {
		request.SummaryVersion = 1
	}

	systemPrompt := `You are a long-term memory distiller. Extract only durable, reusable information from the dialogue.
Return one JSON object with exactly these fields:
{"memory_type":"episodic","summary":"concise searchable summary","facts":[],"preferences":[],"decisions":[],"followups":[]}
Do not invent information. Omit transient small talk. Use empty arrays when a category has no durable information.`
	rawSummary, err := m.aiClient.GetChatCompletion(ctx, systemPrompt, request.DialogueHistory, m.chatModel)
	if err != nil {
		return fmt.Errorf("distill dialog history failed: %w", err)
	}
	structured := parseEpisodicSummary(rawSummary)
	searchable := renderEpisodicSummary(structured)
	if searchable == "" {
		return nil
	}

	vector, err := m.aiClient.GetEmbedding(ctx, searchable, m.embedModel)
	if err != nil {
		return fmt.Errorf("generate episodic memory embedding failed: %w", err)
	}

	collectionName := m.episodic.CollectionName
	payload := map[string]interface{}{
		"summary":             searchable,
		"memory_type":         structured.MemoryType,
		"facts":               structured.Facts,
		"preferences":         structured.Preferences,
		"decisions":           structured.Decisions,
		"followups":           structured.Followups,
		"source_dialogue":     request.SourceDialogue,
		"summary_version":     request.SummaryVersion,
		"created_at":          time.Now().Unix(),
		"user_id":             fmt.Sprintf("%d", request.UserID),
		"source":              "episodic",
		"collection_schema":   "shared_user_payload_v1",
		"embedding_model":     m.embedModel,
		"embedding_dimension": len(vector),
		"embedding_version":   m.episodic.EmbeddingVersion,
	}
	if err := m.qdrantClient.UpsertPoint(ctx, collectionName, request.PointID, vector, payload); err != nil {
		return fmt.Errorf("upsert episodic memory failed: %w", err)
	}
	return nil
}

func parseEpisodicSummary(raw string) EpisodicSummary {
	raw = strings.TrimSpace(raw)
	result := EpisodicSummary{MemoryType: "episodic"}
	if raw == "" {
		return result
	}
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= start {
			if err := json.Unmarshal([]byte(raw[start:end+1]), &result); err == nil {
				result.MemoryType = "episodic"
				result.Summary = strings.TrimSpace(result.Summary)
				result.Facts = normalizeSummaryItems(result.Facts)
				result.Preferences = normalizeSummaryItems(result.Preferences)
				result.Decisions = normalizeSummaryItems(result.Decisions)
				result.Followups = normalizeSummaryItems(result.Followups)
				return result
			}
		}
	}
	result.Summary = raw
	return result
}

func normalizeSummaryItems(items []string) []string {
	normalized := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func renderEpisodicSummary(summary EpisodicSummary) string {
	var builder strings.Builder
	if text := strings.TrimSpace(summary.Summary); text != "" {
		builder.WriteString(text)
	}
	sections := []struct {
		name  string
		items []string
	}{
		{name: "Facts", items: summary.Facts},
		{name: "Preferences", items: summary.Preferences},
		{name: "Decisions", items: summary.Decisions},
		{name: "Follow-ups", items: summary.Followups},
	}
	for _, section := range sections {
		if len(section.items) == 0 {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(section.name)
		builder.WriteString(": ")
		builder.WriteString(strings.Join(section.items, "; "))
	}
	return strings.TrimSpace(builder.String())
}

type MemoryChunk struct {
	ID         string
	Content    string
	Timestamp  int64
	Score      float64
	Similarity float64
	Source     string
	Breakdown  ScoreBreakdown
}

type ScoreBreakdown struct {
	Similarity float64
	TimeDecay  float64
	Frequency  float64
	FinalScore float64
}

func episodicUserFilter(userID uint64) map[string]interface{} {
	return map[string]interface{}{
		"must": []interface{}{
			map[string]interface{}{
				"key": "user_id",
				"match": map[string]interface{}{
					"value": fmt.Sprintf("%d", userID),
				},
			},
		},
	}
}

func (m *MemoryManager) SearchEpisodicMemory(ctx context.Context, userID uint64, query string, limit int) ([]MemoryChunk, error) {
	if m == nil || m.qdrantClient == nil || m.aiClient == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	vector, err := m.aiClient.GetEmbedding(ctx, query, m.embedModel)
	if err != nil {
		return nil, err
	}

	sharedResults, sharedErr := m.qdrantClient.SearchWithFilter(ctx, m.episodic.CollectionName, vector, limit, episodicUserFilter(userID))
	results := sharedResults
	var legacyErr error
	if m.episodic.LegacyReadEnabled && (sharedErr != nil || len(results) < limit) {
		legacyCollection := m.episodic.LegacyCollectionName
		if legacyCollection == nil {
			legacyCollection = DefaultEpisodicMemoryConfig().LegacyCollectionName
		}
		legacyResults, err := m.qdrantClient.Search(ctx, legacyCollection(userID), vector, limit-len(results))
		legacyErr = err
		if err == nil {
			results = append(results, legacyResults...)
		}
	}
	if sharedErr != nil && (!m.episodic.LegacyReadEnabled || legacyErr != nil) {
		return nil, fmt.Errorf("search episodic memory shared collection failed: %w", sharedErr)
	}

	chunks := make([]MemoryChunk, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, r := range results {
		if _, exists := seen[r.ID]; exists {
			continue
		}
		seen[r.ID] = struct{}{}
		content, _ := r.Payload["summary"].(string)
		if strings.TrimSpace(content) == "" {
			continue
		}
		chunks = append(chunks, MemoryChunk{
			ID:         r.ID,
			Content:    content,
			Timestamp:  payloadUnix(r.Payload["created_at"]),
			Score:      r.Score,
			Similarity: r.Score,
			Source:     "episodic",
		})
	}
	return chunks, nil
}

func (m *MemoryManager) SearchHybridKnowledge(ctx context.Context, query string, limit int) ([]MemoryChunk, error) {
	if m == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	type result struct {
		source string
		esDocs []es.TweetDocument
		qDocs  []qdrant.SearchResult
	}

	var wg sync.WaitGroup
	resultCh := make(chan result, 2)

	if m.esClient != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			docs, err := m.esClient.SearchTweets(ctx, query, 1, limit*2)
			if err == nil {
				resultCh <- result{source: "es", esDocs: docs}
			}
		}()
	}

	if m.qdrantClient != nil && m.aiClient != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vector, err := m.aiClient.GetEmbedding(ctx, query, m.embedModel)
			if err != nil {
				return
			}
			docs, err := m.qdrantClient.Search(ctx, "tweets", vector, limit*2)
			if err == nil {
				resultCh <- result{source: "qdrant", qDocs: docs}
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	rrfScores := make(map[string]float64)
	docLookup := make(map[string]MemoryChunk)
	for res := range resultCh {
		switch res.source {
		case "es":
			for rank, doc := range res.esDocs {
				id := doc.ID
				rrfScores[id] += rrf(rank)
				docLookup[id] = MemoryChunk{
					ID:        id,
					Content:   doc.Content,
					Timestamp: doc.CreatedAt,
					Source:    "global_es",
				}
			}
		case "qdrant":
			for rank, doc := range res.qDocs {
				id := doc.ID
				rrfScores[id] += rrf(rank)
				content, _ := doc.Payload["content"].(string)
				if strings.TrimSpace(content) == "" {
					content, _ = doc.Payload["summary"].(string)
				}
				existing := docLookup[id]
				if existing.ID == "" {
					existing.ID = id
				}
				if existing.Content == "" {
					existing.Content = content
				}
				if existing.Timestamp == 0 {
					existing.Timestamp = payloadUnix(doc.Payload["created_at"])
				}
				existing.Similarity = doc.Score
				existing.Source = "global_hybrid"
				docLookup[id] = existing
			}
		}
	}

	merged := make([]MemoryChunk, 0, len(rrfScores))
	for id, score := range rrfScores {
		chunk := docLookup[id]
		if strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		chunk.Score = score
		merged = append(merged, chunk)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Score != merged[j].Score {
			return merged[i].Score > merged[j].Score
		}
		if merged[i].ID != merged[j].ID {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].Content < merged[j].Content
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func (m *MemoryManager) RetrieveAndScore(ctx context.Context, userID uint64, query string, personaKeywords []string) ([]string, error) {
	chunks, err := m.RetrieveScoredChunks(ctx, userID, query, personaKeywords, 10)
	if err != nil {
		return nil, err
	}
	results := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		results = append(results, chunk.Content)
	}
	return results, nil
}

func (m *MemoryManager) RetrieveScoredChunks(ctx context.Context, userID uint64, query string, personaKeywords []string, limit int) ([]MemoryChunk, error) {
	if m == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	l2Chunks, _ := m.SearchEpisodicMemory(ctx, userID, query, limit/2)
	l3Chunks, _ := m.SearchHybridKnowledge(ctx, query, limit/2)
	allChunks := append(l2Chunks, l3Chunks...)
	if len(allChunks) == 0 {
		return nil, nil
	}

	now := time.Now().Unix()
	scoring := m.scoring
	if scoring.SimilarityThreshold <= 0 {
		scoring = DefaultScoringConfig()
	}
	scored := make([]MemoryChunk, 0, len(allChunks))
	for _, chunk := range allChunks {
		scoredChunk, accepted := scoreMemoryChunk(chunk, now, personaKeywords, scoring)
		if !accepted {
			continue
		}
		scored = append(scored, scoredChunk)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].ID != scored[j].ID {
			return scored[i].ID < scored[j].ID
		}
		return scored[i].Content < scored[j].Content
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

func scoreMemoryChunk(chunk MemoryChunk, now int64, personaKeywords []string, scoring ScoringConfig) (MemoryChunk, bool) {
	sim := normalizedSimilarity(chunk)
	if (chunk.Source == "episodic" || chunk.Similarity > 0) && sim < scoring.SimilarityThreshold {
		return chunk, false
	}

	timeDiff := float64(now - chunk.Timestamp)
	if chunk.Timestamp == 0 || timeDiff < 0 {
		timeDiff = 0
	}
	timeDecay := math.Exp(-scoring.TimeDecayLambda * timeDiff)

	freqWeight := 0.0
	contentLower := strings.ToLower(chunk.Content)
	for _, keyword := range personaKeywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword != "" && strings.Contains(contentLower, keyword) {
			freqWeight += scoring.KeywordBonus
		}
	}

	chunk.Breakdown = ScoreBreakdown{
		Similarity: sim,
		TimeDecay:  timeDecay,
		Frequency:  freqWeight,
	}
	chunk.Score = (scoring.SimilarityWeight * sim) +
		(scoring.TimeDecayWeight * timeDecay) +
		(scoring.FrequencyWeight * freqWeight)
	chunk.Breakdown.FinalScore = chunk.Score
	return chunk, true
}

func (m *MemoryManager) BuildContextBlock(ctx context.Context, userID uint64, query string, persona string, personaKeywords []string, budgetTokens int) (string, []MemoryChunk, error) {
	if budgetTokens <= 0 {
		budgetTokens = defaultMemoryBudgetTokens
	}
	chunks, err := m.RetrieveScoredChunks(ctx, userID, query, personaKeywords, 8)
	if err != nil {
		return "", nil, err
	}

	counter := m.tokenCounter
	if counter == nil {
		counter = agentRuntime.NewHeuristicTokenCounter()
	}
	var builder strings.Builder
	usedTokens := 0
	if strings.TrimSpace(persona) != "" {
		header := "[Long-term persona]\n"
		personaBudget := minInt(defaultPersonaBudgetTokens, budgetTokens-counter.CountText(header))
		personaText := truncateToTokenBudget(persona, personaBudget, counter)
		block := header + personaText + "\n\n"
		if personaText != "" && counter.CountText(block) <= budgetTokens {
			builder.WriteString(block)
			usedTokens = counter.CountText(block)
		}
	}

	included := make([]MemoryChunk, 0, len(chunks))
	if len(chunks) > 0 {
		header := "[Relevant memory and knowledge]\n"
		headerTokens := counter.CountText(header)
		wroteHeader := false
		for i, chunk := range chunks {
			next := fmt.Sprintf("%d. (%s score=%.3f) %s\n", i+1, chunk.Source, chunk.Score, strings.TrimSpace(chunk.Content))
			required := counter.CountText(next)
			if !wroteHeader {
				required += headerTokens
			}
			if usedTokens+required > budgetTokens {
				continue
			}
			if !wroteHeader {
				builder.WriteString(header)
				usedTokens += headerTokens
				wroteHeader = true
			}
			builder.WriteString(next)
			usedTokens += counter.CountText(next)
			included = append(included, chunk)
		}
	}

	return strings.TrimSpace(builder.String()), included, nil
}

func rrf(rank int) float64 {
	return 1.0 / (60.0 + float64(rank+1))
}

func normalizedSimilarity(chunk MemoryChunk) float64 {
	if chunk.Similarity > 0 {
		return clamp01(chunk.Similarity)
	}
	if strings.HasPrefix(chunk.Source, "global_") {
		return clamp01(chunk.Score * 60)
	}
	return clamp01(chunk.Score)
}

func payloadUnix(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case string:
		var parsed int64
		_, _ = fmt.Sscanf(t, "%d", &parsed)
		return parsed
	default:
		return 0
	}
}

func truncateToTokenBudget(s string, maxTokens int, counter agentRuntime.TokenCounter) string {
	s = strings.TrimSpace(s)
	if s == "" || maxTokens <= 0 {
		return ""
	}
	if counter.CountText(s) <= maxTokens {
		return s
	}
	runes := []rune(s)
	low, high := 0, len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		candidate := string(runes[:mid]) + "..."
		if counter.CountText(candidate) <= maxTokens {
			low = mid
		} else {
			high = mid - 1
		}
	}
	if low == 0 {
		return ""
	}
	return string(runes[:low]) + "..."
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
