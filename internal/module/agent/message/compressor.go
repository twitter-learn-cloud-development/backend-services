package message

import (
	"strings"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const truncationMarker = "\n...[compressed]...\n"

type TokenAwareTruncator struct {
	counter agentRuntime.TokenCounter
}

func NewTokenAwareTruncator(counter agentRuntime.TokenCounter) *TokenAwareTruncator {
	if counter == nil {
		counter = agentRuntime.NewHeuristicTokenCounter()
	}
	return &TokenAwareTruncator{counter: counter}
}

func (c *TokenAwareTruncator) Compress(content string, maxTokens int) string {
	content = strings.TrimSpace(content)
	if maxTokens <= 0 || content == "" {
		return ""
	}
	if c.counter.CountText(content) <= maxTokens {
		return content
	}
	if c.counter.CountText(truncationMarker) >= maxTokens {
		return ""
	}
	runes := []rune(content)
	low, high := 0, len(runes)
	best := ""
	for low <= high {
		kept := (low + high) / 2
		prefixLength := (kept + 1) / 2
		suffixLength := kept / 2
		candidate := string(runes[:prefixLength]) + truncationMarker + string(runes[len(runes)-suffixLength:])
		if c.counter.CountText(candidate) <= maxTokens {
			best = candidate
			low = kept + 1
		} else {
			high = kept - 1
		}
	}
	return best
}
