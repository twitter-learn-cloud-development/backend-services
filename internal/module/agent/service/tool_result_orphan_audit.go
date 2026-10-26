package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	DefaultToolResultOrphanGracePeriod = 24 * time.Hour
	DefaultToolResultOrphanAuditLimit  = 100
)

type ToolResultObjectInventoryItem struct {
	Key           string
	KeyDigest     string
	TenantHash    string
	ObjectDigest  string
	LastModified  time.Time
	Length        int64
	ValidOwnedKey bool
}

type ToolResultObjectInventory interface {
	ListToolResultObjects(context.Context, string, int) ([]ToolResultObjectInventoryItem, string, error)
}

type ToolResultReferenceLookup interface {
	ToolResultReferenceExists(context.Context, string) (bool, error)
}

type ToolResultOrphanAuditConfig struct {
	GracePeriod time.Duration
	Limit       int
}

func DefaultToolResultOrphanAuditConfig() ToolResultOrphanAuditConfig {
	return ToolResultOrphanAuditConfig{
		GracePeriod: DefaultToolResultOrphanGracePeriod,
		Limit:       DefaultToolResultOrphanAuditLimit,
	}
}

func (c ToolResultOrphanAuditConfig) Validate() error {
	if c.GracePeriod <= 0 {
		return errors.New("tool result orphan grace period must be positive")
	}
	if c.Limit <= 0 || c.Limit > 1000 {
		return errors.New("tool result orphan audit limit must be between 1 and 1000")
	}
	return nil
}

type ToolResultOrphanCandidate struct {
	KeyDigest    string    `json:"key_digest"`
	TenantHash   string    `json:"tenant_hash"`
	ObjectDigest string    `json:"object_digest"`
	LastModified time.Time `json:"last_modified"`
	Length       int64     `json:"length"`
}

type ToolResultOrphanAuditReport struct {
	SchemaVersion string                      `json:"schema_version"`
	ReadOnly      bool                        `json:"read_only"`
	Scanned       int                         `json:"scanned"`
	Recent        int                         `json:"recent"`
	Referenced    int                         `json:"referenced"`
	Invalid       int                         `json:"invalid"`
	LookupFailed  int                         `json:"lookup_failed"`
	Candidates    []ToolResultOrphanCandidate `json:"candidates"`
	NextCursor    string                      `json:"next_cursor,omitempty"`
}

type ToolResultOrphanAuditor struct {
	inventory  ToolResultObjectInventory
	references ToolResultReferenceLookup
	config     ToolResultOrphanAuditConfig
}

func NewToolResultOrphanAuditor(
	inventory ToolResultObjectInventory,
	references ToolResultReferenceLookup,
	config ToolResultOrphanAuditConfig,
) (*ToolResultOrphanAuditor, error) {
	if inventory == nil || references == nil {
		return nil, errors.New("tool result orphan audit dependencies are required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &ToolResultOrphanAuditor{inventory: inventory, references: references, config: config}, nil
}

func (a *ToolResultOrphanAuditor) Audit(
	ctx context.Context,
	now time.Time,
	cursor string,
) (ToolResultOrphanAuditReport, error) {
	report := ToolResultOrphanAuditReport{
		SchemaVersion: "agent-tool-result-orphan-audit/v1",
		ReadOnly:      true,
		Candidates:    make([]ToolResultOrphanCandidate, 0),
	}
	if a == nil {
		return report, errors.New("tool result orphan auditor is nil")
	}
	if now.IsZero() {
		return report, errors.New("tool result orphan audit time is required")
	}
	items, nextCursor, err := a.inventory.ListToolResultObjects(ctx, cursor, a.config.Limit)
	if err != nil {
		return report, fmt.Errorf("list tool result objects: %w", err)
	}
	report.NextCursor = nextCursor
	cutoff := now.Add(-a.config.GracePeriod)
	var auditErr error
	for _, item := range items {
		report.Scanned++
		if !item.ValidOwnedKey || item.LastModified.IsZero() || item.Length <= 0 {
			report.Invalid++
			continue
		}
		if item.LastModified.After(cutoff) {
			report.Recent++
			continue
		}
		exists, lookupErr := a.references.ToolResultReferenceExists(ctx, item.Key)
		if lookupErr != nil {
			report.LookupFailed++
			auditErr = errors.Join(auditErr, fmt.Errorf("lookup object reference %s: %w", item.KeyDigest, lookupErr))
			continue
		}
		if exists {
			report.Referenced++
			continue
		}
		report.Candidates = append(report.Candidates, ToolResultOrphanCandidate{
			KeyDigest: item.KeyDigest, TenantHash: item.TenantHash,
			ObjectDigest: item.ObjectDigest, LastModified: item.LastModified, Length: item.Length,
		})
	}
	return report, auditErr
}
