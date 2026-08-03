package profile

import (
	"context"
	"errors"
	"sync/atomic"
)

// AtomicResolver keeps request reads lock-free while allowing a fully
// validated immutable Catalog to replace the previous application snapshot.
// A request loads exactly one Catalog pointer and cannot observe a partial
// release update.
type AtomicResolver struct {
	current atomic.Pointer[Catalog]
}

func NewAtomicResolver(initial *Catalog) (*AtomicResolver, error) {
	if initial == nil {
		return nil, errors.New("initial agent profile catalog is required")
	}
	resolver := &AtomicResolver{}
	resolver.current.Store(initial)
	return resolver, nil
}

func (r *AtomicResolver) Resolve(ctx context.Context, profileID string, subject SelectionSubject) (AgentProfile, error) {
	if r == nil {
		return AgentProfile{}, errors.New("atomic agent profile resolver is nil")
	}
	catalog := r.current.Load()
	if catalog == nil {
		return AgentProfile{}, errors.New("agent profile catalog snapshot is unavailable")
	}
	return catalog.Resolve(ctx, profileID, subject)
}

func (r *AtomicResolver) ResolveVersion(ctx context.Context, profileID, version string) (AgentProfile, error) {
	if r == nil {
		return AgentProfile{}, errors.New("atomic agent profile resolver is nil")
	}
	catalog := r.current.Load()
	if catalog == nil {
		return AgentProfile{}, errors.New("agent profile catalog snapshot is unavailable")
	}
	return catalog.ResolveVersion(ctx, profileID, version)
}

func (r *AtomicResolver) ResolveProfileSet(
	ctx context.Context,
	anchorID string,
	memberIDs []string,
	subject SelectionSubject,
) (ProfileSet, error) {
	if r == nil {
		return ProfileSet{}, errors.New("atomic agent profile resolver is nil")
	}
	catalog := r.current.Load()
	if catalog == nil {
		return ProfileSet{}, errors.New("agent profile catalog snapshot is unavailable")
	}
	return catalog.ResolveProfileSet(ctx, anchorID, memberIDs, subject)
}

func (r *AtomicResolver) ResolveProfileSetVersion(
	ctx context.Context,
	anchorID string,
	memberIDs []string,
	version string,
) (ProfileSet, error) {
	if r == nil {
		return ProfileSet{}, errors.New("atomic agent profile resolver is nil")
	}
	catalog := r.current.Load()
	if catalog == nil {
		return ProfileSet{}, errors.New("agent profile catalog snapshot is unavailable")
	}
	return catalog.ResolveProfileSetVersion(ctx, anchorID, memberIDs, version)
}

func (r *AtomicResolver) Replace(next *Catalog) error {
	if r == nil {
		return errors.New("atomic agent profile resolver is nil")
	}
	if next == nil {
		return errors.New("next agent profile catalog is required")
	}
	r.current.Store(next)
	return nil
}
