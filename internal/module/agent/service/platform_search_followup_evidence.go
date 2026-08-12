package service

import (
	"encoding/json"
	"strings"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func runtimeHasPlatformTweetDetailEvidence(
	result agentRuntime.RunResult,
	expectedTweetID string,
) bool {
	expectedTweetID = strings.TrimSpace(expectedTweetID)
	if expectedTweetID == "" {
		return false
	}
	for _, step := range result.Steps {
		for _, action := range step.Actions {
			if action.Type != agentRuntime.ActionToolCall || action.Name != "get_tweets_by_ids" ||
				!platformTweetDetailActionSelects(action, expectedTweetID) {
				continue
			}
			for _, observation := range step.Observations {
				if observation.ActionID != action.ID || observation.IsError {
					continue
				}
				detail, ok := decodePlatformTweetDetailEvidence(observation.StructuredContent)
				if !ok {
					continue
				}
				for _, item := range detail.Items {
					if strings.TrimSpace(item.TweetID) == expectedTweetID &&
						strings.TrimSpace(item.Content) != "" {
						return true
					}
				}
			}
		}
	}
	return false
}

func platformTweetDetailActionSelects(action agentRuntime.Action, expectedTweetID string) bool {
	var arguments struct {
		TweetIDs string `json:"tweet_ids"`
	}
	if err := json.Unmarshal(action.Arguments, &arguments); err != nil {
		return false
	}
	values := strings.Split(arguments.TweetIDs, ",")
	return len(values) == 1 && strings.TrimSpace(values[0]) == expectedTweetID
}
