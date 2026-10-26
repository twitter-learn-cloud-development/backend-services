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

	"twitter-clone/internal/domain"
)

func (c *Client) ReadTweetProjection(ctx context.Context, id uint64) (domain.IndexProjectionSnapshot, error) {
	if id == 0 {
		return domain.IndexProjectionSnapshot{}, errors.New("invalid projection identity")
	}
	canonical := ConvertSnowflakeToQdrantID(id)
	body, _ := json.Marshal(map[string]interface{}{"ids": []string{canonical}, "with_vector": false, "with_payload": []string{"tweet_id", "projection_digest", "projection_version", "embedding_profile"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/collections/tweets/points", bytes.NewReader(body))
	if err != nil {
		return domain.IndexProjectionSnapshot{}, errors.New("vector metadata request unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return domain.IndexProjectionSnapshot{}, errors.New("vector metadata unavailable")
	}
	defer resp.Body.Close()
	var receipt struct {
		Result []struct {
			ID      string                 `json:"id"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
		Status json.RawMessage `json:"status"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&receipt) != nil {
		return domain.IndexProjectionSnapshot{}, errors.New("invalid vector metadata receipt")
	}
	if resp.StatusCode == 404 {
		var status struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(receipt.Status, &status) == nil && strings.Contains(status.Error, "Collection") && strings.Contains(status.Error, "tweets") && strings.Contains(status.Error, "doesn't exist") {
			return domain.IndexProjectionSnapshot{}, nil
		}
	}
	if resp.StatusCode != 200 || len(receipt.Result) > 1 {
		return domain.IndexProjectionSnapshot{}, errors.New("vector metadata lookup rejected")
	}
	count, err := c.tweetProjectionCount(ctx, id)
	if err != nil {
		return domain.IndexProjectionSnapshot{}, errors.New("vector alias count unavailable")
	}
	snapshot := domain.IndexProjectionSnapshot{Present: len(receipt.Result) == 1 || count > 0, Copies: count}
	if len(receipt.Result) == 1 {
		point := receipt.Result[0]
		if point.ID != canonical {
			return domain.IndexProjectionSnapshot{}, errors.New("vector identity mismatch")
		}
		// A canonical point without this payload must also count as a residue.
		if point.Payload["tweet_id"] != fmt.Sprint(id) {
			snapshot.Copies++
		}
		snapshot.Digest, _ = point.Payload["projection_digest"].(string)
		snapshot.Version, _ = point.Payload["projection_version"].(string)
		snapshot.EmbeddingProfile, _ = point.Payload["embedding_profile"].(string)
	}
	return snapshot, nil
}

func (c *Client) tweetProjectionCount(ctx context.Context, id uint64) (int64, error) {
	body, _ := json.Marshal(map[string]interface{}{"filter": map[string]interface{}{"must": []interface{}{map[string]interface{}{"key": "tweet_id", "match": map[string]interface{}{"value": fmt.Sprint(id)}}}}, "exact": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/collections/tweets/points/count", bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("vector count request unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, errors.New("vector count unavailable")
	}
	defer resp.Body.Close()
	var receipt struct {
		Result struct {
			Count *int64 `json:"count"`
		} `json:"result"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&receipt) != nil || receipt.Result.Count == nil || *receipt.Result.Count < 0 {
		return 0, errors.New("invalid vector count receipt")
	}
	return *receipt.Result.Count, nil
}
