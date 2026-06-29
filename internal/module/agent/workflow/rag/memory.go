package rag

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"
)

const (
	defaultSimilarityThreshold = 0.65
	defaultMemoryBudgetChars   = 3600
)

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
}

func NewMemoryManager(
	db *gorm.DB,
	esClient *es.Client,
	qdrantClient *qdrant.Client,
	aiClient *ai.Client,
	chatModel string,
	embedModel string,
) *MemoryManager {
	if db != nil {
		_ = db.AutoMigrate(&UserPersona{})
	}

	return &MemoryManager{
		db:           db,
		esClient:     esClient,
		qdrantClient: qdrantClient,
		aiClient:     aiClient,
		chatModel:    chatModel,
		embedModel:   embedModel,
	}
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

func (m *MemoryManager) SaveEpisodicMemory(ctx context.Context, userID uint64, sessionID uint64, dialogHistory string) error {
	if m == nil || m.aiClient == nil || m.qdrantClient == nil {
		return errors.New("episodic memory dependencies are not initialized")
	}
	if strings.TrimSpace(dialogHistory) == "" {
		return nil
	}

	systemPrompt := "You are a long-term memory distiller. Summarize the dialogue into structured memory, preserving user preferences, technical topics, key conclusions, and follow-up items. Output only the summary body."
	summary, err := m.aiClient.GetChatCompletion(ctx, systemPrompt, dialogHistory, m.chatModel)
	if err != nil {
		return fmt.Errorf("distill dialog history failed: %w", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}

	vector, err := m.aiClient.GetEmbedding(ctx, summary, m.embedModel)
	if err != nil {
		return fmt.Errorf("generate episodic memory embedding failed: %w", err)
	}

	collectionName := fmt.Sprintf("episodic_user_%d", userID)
	payload := map[string]interface{}{
		"summary":    summary,
		"created_at": time.Now().Unix(),
		"user_id":    fmt.Sprintf("%d", userID),
		"source":     "episodic",
	}
	if err := m.qdrantClient.UpsertPoint(ctx, collectionName, sessionID, vector, payload); err != nil {
		return fmt.Errorf("upsert episodic memory failed: %w", err)
	}
	return nil
}

type MemoryChunk struct {
	Content    string
	Timestamp  int64
	Score      float64
	Similarity float64
	Source     string
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

	results, err := m.qdrantClient.Search(ctx, fmt.Sprintf("episodic_user_%d", userID), vector, limit)
	if err != nil {
		return nil, nil
	}

	chunks := make([]MemoryChunk, 0, len(results))
	for _, r := range results {
		content, _ := r.Payload["summary"].(string)
		if strings.TrimSpace(content) == "" {
			continue
		}
		chunks = append(chunks, MemoryChunk{
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
		return merged[i].Score > merged[j].Score
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
	const lambda = 0.00001
	scored := make([]MemoryChunk, 0, len(allChunks))
	for _, chunk := range allChunks {
		sim := normalizedSimilarity(chunk)
		if (chunk.Source == "episodic" || chunk.Similarity > 0) && sim < defaultSimilarityThreshold {
			continue
		}

		timeDiff := float64(now - chunk.Timestamp)
		if chunk.Timestamp == 0 || timeDiff < 0 {
			timeDiff = 0
		}
		timeDecay := math.Exp(-lambda * timeDiff)

		freqWeight := 0.0
		contentLower := strings.ToLower(chunk.Content)
		for _, keyword := range personaKeywords {
			kw := strings.ToLower(strings.TrimSpace(keyword))
			if kw != "" && strings.Contains(contentLower, kw) {
				freqWeight += 0.15
			}
		}

		chunk.Score = (0.6 * sim) + (0.25 * timeDecay) + (0.15 * freqWeight)
		scored = append(scored, chunk)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored, nil
}

func (m *MemoryManager) BuildContextBlock(ctx context.Context, userID uint64, query string, persona string, personaKeywords []string, budgetChars int) (string, []MemoryChunk, error) {
	if budgetChars <= 0 {
		budgetChars = defaultMemoryBudgetChars
	}
	chunks, err := m.RetrieveScoredChunks(ctx, userID, query, personaKeywords, 8)
	if err != nil {
		return "", nil, err
	}

	var builder strings.Builder
	if strings.TrimSpace(persona) != "" {
		builder.WriteString("[Long-term persona]\n")
		builder.WriteString(truncate(persona, minInt(900, budgetChars)))
		builder.WriteString("\n\n")
	}

	if len(chunks) > 0 {
		builder.WriteString("[Relevant memory and knowledge]\n")
		for i, chunk := range chunks {
			next := fmt.Sprintf("%d. (%s score=%.3f) %s\n", i+1, chunk.Source, chunk.Score, strings.TrimSpace(chunk.Content))
			if builder.Len()+len([]rune(next)) > budgetChars {
				break
			}
			builder.WriteString(next)
		}
	}

	return strings.TrimSpace(builder.String()), chunks, nil
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

func truncate(s string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(s))
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
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
