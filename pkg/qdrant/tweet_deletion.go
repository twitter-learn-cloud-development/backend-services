package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DeleteTweet is fixed to the public tweets collection. Remove the canonical
// UUID and payload-addressed legacy aliases; never delete the collection.
func (c *Client) DeleteTweet(ctx context.Context, id uint64) error {
	if id == 0 {
		return errors.New("invalid tweet deletion identity")
	}
	requests := []map[string]interface{}{
		{"points": []string{ConvertSnowflakeToQdrantID(id)}},
		{"filter": map[string]interface{}{"must": []interface{}{
			map[string]interface{}{"key": "tweet_id", "match": map[string]interface{}{"value": fmt.Sprintf("%d", id)}},
		}}},
	}
	for _, body := range requests {
		data, err := json.Marshal(body)
		if err != nil {
			return errors.New("invalid tweet deletion request")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/collections/tweets/points/delete?wait=true", bytes.NewReader(data))
		if err != nil {
			return errors.New("tweet deletion request unavailable")
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.client.Do(req)
		if err != nil {
			return errors.New("tweet vector deletion unavailable")
		}
		var receipt struct {
			Status json.RawMessage `json:"status"`
			Result struct {
				Status string `json:"status"`
			} `json:"result"`
		}
		readErr := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&receipt)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound && readErr == nil {
			var missing struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(receipt.Status, &missing) == nil && strings.Contains(missing.Error, "Collection") && strings.Contains(missing.Error, "tweets") && strings.Contains(missing.Error, "doesn't exist") {
				return nil
			}
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New("tweet vector deletion rejected")
		}
		if readErr != nil || receipt.Result.Status != "completed" {
			return errors.New("tweet vector deletion receipt unavailable")
		}
	}
	return nil
}
