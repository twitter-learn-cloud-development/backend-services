package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"twitter-clone/internal/module/agent/marketplace"
)

// ListAgentMarketplaceExtensions returns only releases that still verify
// against a currently trusted publisher key. Installation remains unavailable
// until a separate approval and dependency-resolution control plane exists.
func (s *AgentService) ListAgentMarketplaceExtensions(
	ctx context.Context,
	userID uint64,
	query marketplace.Query,
) (marketplace.Page, error) {
	if s == nil || !s.extensionMarketplaceEnabled {
		return marketplace.Page{}, marketplace.ErrCatalogDisabled
	}
	if err := ctx.Err(); err != nil {
		return marketplace.Page{}, err
	}
	if userID == 0 {
		return marketplace.Page{}, fmt.Errorf("%w: user_id is required", ErrInvalidUnifiedAgentRequest)
	}
	if s.extensionMarketplaceStore == nil {
		return marketplace.Page{}, errors.New("extension marketplace store is unavailable")
	}
	request, err := marketplace.PrepareList(query, s.extensionMarketplaceLimit)
	if err != nil {
		return marketplace.Page{}, err
	}
	releases, hasMore, err := s.extensionMarketplaceStore.ListPublished(ctx, request)
	if err != nil {
		return marketplace.Page{}, fmt.Errorf("list extension marketplace releases: %w", err)
	}
	if len(releases) > request.PageSize {
		return marketplace.Page{}, fmt.Errorf("extension marketplace store returned more than %d releases", request.PageSize)
	}
	publisherIDs := make([]string, 0, len(releases))
	seenPublishers := make(map[string]struct{}, len(releases))
	for _, release := range releases {
		publisherID := strings.ToLower(strings.TrimSpace(release.Manifest.PublisherID))
		if publisherID == "" {
			return marketplace.Page{}, marketplace.ErrInvalidRelease
		}
		if _, exists := seenPublishers[publisherID]; !exists {
			seenPublishers[publisherID] = struct{}{}
			publisherIDs = append(publisherIDs, publisherID)
		}
	}
	publishers, err := s.extensionMarketplaceStore.GetPublishers(ctx, publisherIDs)
	if err != nil {
		return marketplace.Page{}, fmt.Errorf("list extension marketplace publishers: %w", err)
	}
	listings := make([]marketplace.Listing, 0, len(releases))
	for _, release := range releases {
		publisherID := strings.ToLower(strings.TrimSpace(release.Manifest.PublisherID))
		publisher, exists := publishers[publisherID]
		if !exists {
			return marketplace.Page{}, fmt.Errorf("%w: publisher %q is missing", marketplace.ErrSignatureVerification, publisherID)
		}
		listing, verifyErr := marketplace.VerifyRelease(publisher, release)
		if verifyErr != nil {
			return marketplace.Page{}, fmt.Errorf("verify extension marketplace release %q: %w", release.ReleaseID, verifyErr)
		}
		if request.Kind != "" && listing.Kind != request.Kind {
			return marketplace.Page{}, fmt.Errorf("%w: store returned a mismatched kind", marketplace.ErrInvalidRelease)
		}
		if request.PublisherID != "" && listing.Publisher.PublisherID != request.PublisherID {
			return marketplace.Page{}, fmt.Errorf("%w: store returned a mismatched publisher", marketplace.ErrInvalidRelease)
		}
		listings = append(listings, listing)
	}
	return marketplace.BuildPage(request, listings, hasMore)
}
