package rag

import (
	"context"
	"strconv"

	"twitter-clone/internal/module/agent/retrieval"
)

// Candidate rank/similarity may come from an index; content and timestamps
// must come from the current public authority before scoring/context assembly.
func (m *MemoryManager) hydratePublicKnowledge(ctx context.Context, candidates []MemoryChunk) ([]MemoryChunk, error) {
	if len(candidates) == 0 {
		return []MemoryChunk{}, nil
	}
	if m.publicTweets == nil {
		return nil, retrieval.ErrPublicTweetsUnavailable
	}
	ids := make([]string, 0, len(candidates))
	byID := make(map[string]MemoryChunk, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
		byID[candidate.ID] = candidate
	}
	tweets, err := m.publicTweets.ReadPublicTweets(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]MemoryChunk, 0, len(tweets))
	for _, tweet := range tweets {
		chunk := byID[strconv.FormatUint(tweet.Id, 10)]
		chunk.Content = tweet.Content
		chunk.Timestamp = tweet.CreatedAt / 1000
		result = append(result, chunk)
	}
	return result, nil
}
