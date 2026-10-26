package es

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"twitter-clone/internal/domain"
)

// ReadTweetProjection is one explicit-ID metadata read, never a full index scan.
func (c *Client) ReadTweetProjection(ctx context.Context, id uint64) (domain.IndexProjectionSnapshot, error) {
	if id == 0 {
		return domain.IndexProjectionSnapshot{}, errors.New("invalid projection identity")
	}
	resp, err := c.Get(TweetIndex, fmt.Sprint(id)).SourceIncludes_("projection_digest", "projection_version").Perform(ctx)
	if err != nil {
		return domain.IndexProjectionSnapshot{}, errors.New("text projection metadata unavailable")
	}
	defer resp.Body.Close()
	var record struct {
		ID     string `json:"_id"`
		Found  *bool  `json:"found"`
		Source struct {
			Digest  string `json:"projection_digest"`
			Version string `json:"projection_version"`
		} `json:"_source"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&record) != nil {
		return domain.IndexProjectionSnapshot{}, errors.New("invalid text metadata receipt")
	}
	if resp.StatusCode == http.StatusNotFound && (record.Error.Type == "index_not_found_exception" || (record.ID == fmt.Sprint(id) && record.Found != nil && !*record.Found)) {
		return domain.IndexProjectionSnapshot{}, nil
	}
	if resp.StatusCode != http.StatusOK || record.Found == nil || !*record.Found || record.ID != fmt.Sprint(id) {
		return domain.IndexProjectionSnapshot{}, errors.New("text metadata lookup rejected")
	}
	return domain.IndexProjectionSnapshot{Present: true, Copies: 1, Digest: record.Source.Digest, Version: record.Source.Version}, nil
}
