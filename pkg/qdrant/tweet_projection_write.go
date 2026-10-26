package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
)

// Fixed Tweet writer: no Collection creation or recursive retries. The
// caller's bounded durable task owns retries and profile/schema governance.
func (c *Client) UpsertTweetProjection(ctx context.Context, id uint64, vector []float32, payload map[string]interface{}) error {
	if id == 0 || len(vector) == 0 || len(vector) > 8192 {
		return errors.New("invalid tweet vector request")
	}
	for _, number := range vector {
		if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
			return errors.New("invalid tweet vector value")
		}
	}
	copyPayload := map[string]interface{}{}
	for key, value := range payload {
		copyPayload[key] = value
	}
	copyPayload["tweet_id"] = fmt.Sprint(id)
	body, err := json.Marshal(map[string]interface{}{"points": []interface{}{map[string]interface{}{"id": ConvertSnowflakeToQdrantID(id), "vector": vector, "payload": copyPayload}}})
	if err != nil {
		return errors.New("invalid tweet projection payload")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.url+"/collections/tweets/points?wait=true", bytes.NewReader(body))
	if err != nil {
		return errors.New("tweet projection request unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return errors.New("tweet projection write unavailable")
	}
	defer resp.Body.Close()
	var receipt struct {
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&receipt) != nil || receipt.Result.Status != "completed" {
		return errors.New("tweet projection completion unavailable")
	}
	return nil
}
