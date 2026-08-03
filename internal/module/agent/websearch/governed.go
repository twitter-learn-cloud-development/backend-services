package websearch

import (
	"context"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
)

type GovernedProvider struct {
	next       Provider
	governor   AccessGovernor
	costMicros int64
}

func NewGovernedProvider(next Provider, governor AccessGovernor, costMicros int64) Provider {
	if next == nil || governor == nil {
		return next
	}
	if costMicros < 0 {
		costMicros = 0
	}
	return &GovernedProvider{next: next, governor: governor, costMicros: costMicros}
}

func (provider *GovernedProvider) Name() string {
	if provider == nil || provider.next == nil {
		return ""
	}
	return provider.next.Name()
}

func (provider *GovernedProvider) Search(
	ctx context.Context,
	request Request,
) (agentEvidence.WebSearchResult, error) {
	if provider == nil || provider.next == nil || provider.governor == nil {
		return agentEvidence.WebSearchResult{}, ErrUnavailable
	}
	if err := provider.governor.Admit(ctx, AdmissionRequest{
		Subject:    request.Subject,
		Operation:  AccessOperationSearch,
		CostMicros: provider.costMicros,
	}); err != nil {
		return agentEvidence.WebSearchResult{}, err
	}
	return provider.next.Search(ctx, request)
}

type GovernedPageReader struct {
	next       PageReader
	governor   AccessGovernor
	costMicros int64
}

func NewGovernedPageReader(next PageReader, governor AccessGovernor, costMicros int64) PageReader {
	if next == nil || governor == nil {
		return next
	}
	if costMicros < 0 {
		costMicros = 0
	}
	return &GovernedPageReader{next: next, governor: governor, costMicros: costMicros}
}

func (reader *GovernedPageReader) Read(
	ctx context.Context,
	request PageRequest,
) (agentEvidence.WebPageResult, error) {
	if reader == nil || reader.next == nil || reader.governor == nil {
		return agentEvidence.WebPageResult{}, ErrPageUnavailable
	}
	if err := reader.governor.Admit(ctx, AdmissionRequest{
		Subject:    request.Subject,
		Operation:  AccessOperationPageRead,
		CostMicros: reader.costMicros,
	}); err != nil {
		return agentEvidence.WebPageResult{}, err
	}
	return reader.next.Read(ctx, request)
}
