package consumer

import (
	"encoding/json"
	"errors"
	"fmt"

	"twitter-clone/internal/events"
)

func decodeTimelineTweetCreatedEvent(body []byte) (events.TweetCreatedEvent, error) {
	var event events.TweetCreatedEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return events.TweetCreatedEvent{}, fmt.Errorf("decode tweet created event: %w", err)
	}
	if event.TweetID == 0 || event.AuthorID == 0 {
		return events.TweetCreatedEvent{}, errors.New("tweet_id and author_id are required")
	}
	return event, nil
}

func decodeTimelineTweetDeletedEvent(body []byte) (events.TweetDeletedEvent, error) {
	var event events.TweetDeletedEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return events.TweetDeletedEvent{}, fmt.Errorf("decode tweet deleted event: %w", err)
	}
	if event.TweetID == 0 || event.AuthorID == 0 {
		return events.TweetDeletedEvent{}, errors.New("tweet_id and author_id are required")
	}
	return event, nil
}
