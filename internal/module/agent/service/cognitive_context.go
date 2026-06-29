package service

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"

	"twitter-clone/internal/module/agent/workflow/rag"
	"twitter-clone/pkg/logger"
)

const cognitiveContextBudgetChars = 3600

type cognitiveContextResult struct {
	Intent         rag.Intent
	RewrittenQuery string
	Persona        string
	ContextBlock   string
	ChunkCount     int
}

func (s *AgentService) SetCognitiveEngine(memoryManager *rag.MemoryManager, cascadeRouter *rag.CascadeRouter, embeddingModel string) {
	s.memoryManager = memoryManager
	s.cascadeRouter = cascadeRouter
	s.embeddingModel = embeddingModel
}

func (s *AgentService) buildCognitiveContext(ctx context.Context, userID uint64, query string) cognitiveContextResult {
	result := cognitiveContextResult{
		Intent:         rag.IntentGlobalKnowledge,
		RewrittenQuery: query,
	}
	if s.memoryManager == nil {
		return result
	}

	routeCtx, cancelRoute := context.WithTimeout(ctx, 900*time.Millisecond)
	if s.cascadeRouter != nil {
		intent, rewritten, err := s.cascadeRouter.Route(routeCtx, query, s.embeddingModel)
		if err == nil {
			result.Intent = intent
			if strings.TrimSpace(rewritten) != "" {
				result.RewrittenQuery = rewritten
			}
		}
	}
	cancelRoute()

	memoryCtx, cancelMemory := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancelMemory()

	persona, err := s.memoryManager.GetPersona(memoryCtx, userID)
	if err != nil {
		logger.Warn(ctx, "load user persona failed", zap.Error(err))
	}
	result.Persona = persona

	if result.Intent == rag.IntentDirectAction {
		result.ContextBlock = buildPersonaOnlyBlock(persona)
		return result
	}

	keywords := extractPersonaKeywords(persona)
	block, chunks, err := s.memoryManager.BuildContextBlock(
		memoryCtx,
		userID,
		result.RewrittenQuery,
		persona,
		keywords,
		cognitiveContextBudgetChars,
	)
	if err != nil {
		logger.Warn(ctx, "build cognitive context failed", zap.Error(err))
		return result
	}

	result.ContextBlock = block
	result.ChunkCount = len(chunks)
	return result
}

func (s *AgentService) decorateSystemPromptWithCognitiveContext(basePrompt string, cognitive cognitiveContextResult) string {
	if strings.TrimSpace(cognitive.ContextBlock) == "" {
		return basePrompt
	}

	return fmt.Sprintf(`%s

The following context is retrieved from long-term profile, episodic memory, and public knowledge.
Use it only when it is relevant to the user's request.
Do not invent facts that are not present in the context or conversation.
Do not expose internal routing, scoring, or retrieval implementation details.

Route intent: %s
Rewritten query: %s

%s`, basePrompt, cognitive.Intent, cognitive.RewrittenQuery, cognitive.ContextBlock)
}

func (s *AgentService) crystallizeTurnMemoryAsync(userID uint64, dialogueID primitive.ObjectID, userContent string, assistantContent string) {
	if s.memoryManager == nil || strings.TrimSpace(userContent) == "" || strings.TrimSpace(assistantContent) == "" {
		return
	}
	if len([]rune(userContent))+len([]rune(assistantContent)) < 80 {
		return
	}

	history := fmt.Sprintf("User: %s\nAssistant: %s", userContent, assistantContent)
	pointID := dialogueMemoryPointID(dialogueID, time.Now().UnixNano())

	go func() {
		baseCtx := s.serviceCtx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(baseCtx, 20*time.Second)
		defer cancel()
		if err := s.memoryManager.SaveEpisodicMemory(ctx, userID, pointID, history); err != nil {
			logger.Warn(ctx, "crystallize episodic memory failed", zap.Error(err))
		}
	}()
}

func buildPersonaOnlyBlock(persona string) string {
	persona = strings.TrimSpace(persona)
	if persona == "" {
		return ""
	}
	return "[Long-term persona]\n" + persona
}

func extractPersonaKeywords(persona string) []string {
	if strings.TrimSpace(persona) == "" {
		return nil
	}
	fields := strings.FieldsFunc(persona, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == '|'
	})
	keywords := make([]string, 0, len(fields))
	seen := make(map[string]struct{})
	for _, field := range fields {
		kw := strings.TrimSpace(field)
		if len([]rune(kw)) < 2 || len([]rune(kw)) > 32 {
			continue
		}
		lower := strings.ToLower(kw)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		keywords = append(keywords, kw)
		if len(keywords) >= 12 {
			break
		}
	}
	return keywords
}

func dialogueMemoryPointID(dialogueID primitive.ObjectID, salt int64) uint64 {
	idBytes := dialogueID[:]
	base := binary.BigEndian.Uint64(idBytes[4:])
	return base ^ uint64(salt)
}
