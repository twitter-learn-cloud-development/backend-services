package extension

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	ContractVersionV1 = "agent.extension.v1"

	KindCapability = "capability"
	KindSkill      = "skill"
	KindMCPTool    = "mcp_tool"

	SourceBuiltIn     = "built_in"
	SourceWorkflow    = "workflow"
	SourceExternalMCP = "external_mcp"

	ScopePlatform = "platform"
	ScopeUser     = "user"
	ScopeProject  = "project"

	CategoryGeneral  = "general"
	CategoryWorkflow = "workflow"
	CategoryRead     = "read"
	CategoryWrite    = "write"
	CategoryRisky    = "risky"

	StatusAvailable = "available"
	StatusPlanned   = "planned"

	ApprovalNone      = "none"
	ApprovalRequired  = "required"
	ApprovalInherited = "inherited"

	HealthNotApplicable = "not_applicable"
	HealthUnknown       = "unknown"
	HealthHealthy       = "healthy"
	HealthDegraded      = "degraded"
	HealthUnhealthy     = "unhealthy"

	SourceStateReady    = "ready"
	SourceStateDisabled = "disabled"

	DefaultPageSize = 20
	MaxPageSize     = 50
	MaxCatalogItems = 256

	maxCursorBytes      = 1024
	maxSearchRunes      = 120
	maxEntryIDBytes     = 192
	maxEntryNameRunes   = 160
	maxDescriptionRunes = 512
)

var (
	ErrCatalogDisabled = errors.New("agent extension catalog is disabled")
	ErrInvalidQuery    = errors.New("invalid agent extension catalog query")
	ErrInvalidCursor   = errors.New("invalid agent extension catalog cursor")
	ErrInvalidEntry    = errors.New("invalid agent extension catalog entry")
)

// Query describes one bounded read from the tenant extension directory.
// Empty filters mean all values. Cursor values are bound to the normalized
// filters so a cursor cannot silently be reused for another result set.
type Query struct {
	Kind        string
	Category    string
	Scope       string
	Status      string
	Search      string
	AfterCursor string
	PageSize    int
}

type SkillReference struct {
	SkillID string
	Version string
}

type MCPReference struct {
	ConnectionID      string
	ServerID          string
	SnapshotID        string
	QualifiedToolName string
}

// Entry is a credential-free catalog projection. It is deliberately not an
// execution contract: Runtime, ToolExecutor, Policy, Budget and Approval remain
// authoritative when an extension is selected or invoked.
type Entry struct {
	ContractVersion string
	ID              string
	Kind            string
	Name            string
	DisplayName     string
	Description     string
	Version         string
	Source          string
	CapabilityID    string
	Category        string
	Scope           string
	Status          string
	ApprovalMode    string
	HealthStatus    string
	Skill           *SkillReference
	MCP             *MCPReference
}

type SourceStatus struct {
	Source     string
	State      string
	EntryCount int
}

type Page struct {
	ContractVersion string
	Entries         []Entry
	Sources         []SourceStatus
	NextCursor      string
	HasMore         bool
}

type catalogCursor struct {
	Version    int    `json:"v"`
	FilterHash string `json:"filter_sha256"`
	AfterKey   string `json:"after_key"`
}

// BuildPage validates and paginates a bounded snapshot assembled by the
// service layer. Source adapters must already have enforced tenant access.
func BuildPage(entries []Entry, sources []SourceStatus, query Query, configuredPageSize int) (Page, error) {
	normalized, err := normalizeQuery(query, configuredPageSize)
	if err != nil {
		return Page{}, err
	}
	if len(entries) > MaxCatalogItems {
		return Page{}, fmt.Errorf("%w: entries exceed %d", ErrInvalidEntry, MaxCatalogItems)
	}

	filtered := make([]Entry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, candidate := range entries {
		entry := cloneEntry(candidate)
		if err := validateEntry(entry); err != nil {
			return Page{}, err
		}
		if _, exists := seen[entry.ID]; exists {
			return Page{}, fmt.Errorf("%w: duplicate entry id %q", ErrInvalidEntry, entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if matchesQuery(entry, normalized) {
			filtered = append(filtered, entry)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return entrySortKey(filtered[i]) < entrySortKey(filtered[j])
	})

	filterHash := queryFilterHash(normalized)
	afterKey := ""
	if normalized.AfterCursor != "" {
		cursor, decodeErr := decodeCursor(normalized.AfterCursor)
		if decodeErr != nil {
			return Page{}, decodeErr
		}
		if cursor.FilterHash != filterHash {
			return Page{}, fmt.Errorf("%w: cursor filters do not match request", ErrInvalidCursor)
		}
		afterKey = cursor.AfterKey
	}

	start := sort.Search(len(filtered), func(index int) bool {
		return entrySortKey(filtered[index]) > afterKey
	})
	end := start + normalized.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	pageEntries := cloneEntries(filtered[start:end])
	hasMore := end < len(filtered)
	nextCursor := ""
	if hasMore && len(pageEntries) > 0 {
		nextCursor, err = encodeCursor(catalogCursor{
			Version: 1, FilterHash: filterHash,
			AfterKey: entrySortKey(pageEntries[len(pageEntries)-1]),
		})
		if err != nil {
			return Page{}, err
		}
	}

	return Page{
		ContractVersion: ContractVersionV1,
		Entries:         pageEntries,
		Sources:         normalizeSources(sources),
		NextCursor:      nextCursor,
		HasMore:         hasMore,
	}, nil
}

func normalizeQuery(query Query, configuredPageSize int) (Query, error) {
	query.Kind = strings.ToLower(strings.TrimSpace(query.Kind))
	query.Category = strings.ToLower(strings.TrimSpace(query.Category))
	query.Scope = strings.ToLower(strings.TrimSpace(query.Scope))
	query.Status = strings.ToLower(strings.TrimSpace(query.Status))
	query.Search = strings.ToLower(strings.TrimSpace(query.Search))
	query.AfterCursor = strings.TrimSpace(query.AfterCursor)

	if !oneOfOrEmpty(query.Kind, KindCapability, KindSkill, KindMCPTool) {
		return Query{}, fmt.Errorf("%w: unsupported kind %q", ErrInvalidQuery, query.Kind)
	}
	if !oneOfOrEmpty(query.Category, CategoryGeneral, CategoryWorkflow, CategoryRead, CategoryWrite, CategoryRisky) {
		return Query{}, fmt.Errorf("%w: unsupported category %q", ErrInvalidQuery, query.Category)
	}
	if !oneOfOrEmpty(query.Scope, ScopePlatform, ScopeUser, ScopeProject) {
		return Query{}, fmt.Errorf("%w: unsupported scope %q", ErrInvalidQuery, query.Scope)
	}
	if !oneOfOrEmpty(query.Status, StatusAvailable, StatusPlanned) {
		return Query{}, fmt.Errorf("%w: unsupported status %q", ErrInvalidQuery, query.Status)
	}
	if utf8.RuneCountInString(query.Search) > maxSearchRunes {
		return Query{}, fmt.Errorf("%w: search exceeds %d characters", ErrInvalidQuery, maxSearchRunes)
	}
	if len(query.AfterCursor) > maxCursorBytes {
		return Query{}, fmt.Errorf("%w: cursor exceeds %d bytes", ErrInvalidCursor, maxCursorBytes)
	}
	if configuredPageSize < 1 || configuredPageSize > MaxPageSize {
		configuredPageSize = DefaultPageSize
	}
	if query.PageSize == 0 {
		query.PageSize = configuredPageSize
	}
	if query.PageSize < 1 || query.PageSize > configuredPageSize || query.PageSize > MaxPageSize {
		return Query{}, fmt.Errorf("%w: page_size must be within 1..%d", ErrInvalidQuery, configuredPageSize)
	}
	return query, nil
}

func validateEntry(entry Entry) error {
	if entry.ContractVersion != ContractVersionV1 {
		return fmt.Errorf("%w: unsupported contract version %q", ErrInvalidEntry, entry.ContractVersion)
	}
	if strings.TrimSpace(entry.ID) == "" || len(entry.ID) > maxEntryIDBytes {
		return fmt.Errorf("%w: invalid entry id", ErrInvalidEntry)
	}
	if !oneOf(entry.Kind, KindCapability, KindSkill, KindMCPTool) {
		return fmt.Errorf("%w: unsupported kind %q", ErrInvalidEntry, entry.Kind)
	}
	if err := validateText("name", entry.Name, maxEntryNameRunes); err != nil {
		return err
	}
	if err := validateText("display_name", entry.DisplayName, maxEntryNameRunes); err != nil {
		return err
	}
	if utf8.RuneCountInString(strings.TrimSpace(entry.Description)) > maxDescriptionRunes {
		return fmt.Errorf("%w: description exceeds %d characters", ErrInvalidEntry, maxDescriptionRunes)
	}
	if strings.TrimSpace(entry.Version) == "" || strings.TrimSpace(entry.Source) == "" || strings.TrimSpace(entry.CapabilityID) == "" {
		return fmt.Errorf("%w: version, source and capability_id are required", ErrInvalidEntry)
	}
	if !oneOf(entry.Category, CategoryGeneral, CategoryWorkflow, CategoryRead, CategoryWrite, CategoryRisky) {
		return fmt.Errorf("%w: unsupported category %q", ErrInvalidEntry, entry.Category)
	}
	if !oneOf(entry.Scope, ScopePlatform, ScopeUser, ScopeProject) {
		return fmt.Errorf("%w: unsupported scope %q", ErrInvalidEntry, entry.Scope)
	}
	if !oneOf(entry.Status, StatusAvailable, StatusPlanned) {
		return fmt.Errorf("%w: unsupported status %q", ErrInvalidEntry, entry.Status)
	}
	if !oneOf(entry.ApprovalMode, ApprovalNone, ApprovalRequired, ApprovalInherited) {
		return fmt.Errorf("%w: unsupported approval mode %q", ErrInvalidEntry, entry.ApprovalMode)
	}
	if !oneOf(entry.HealthStatus, HealthNotApplicable, HealthUnknown, HealthHealthy, HealthDegraded, HealthUnhealthy) {
		return fmt.Errorf("%w: unsupported health status %q", ErrInvalidEntry, entry.HealthStatus)
	}
	switch entry.Kind {
	case KindCapability:
		if entry.Skill != nil || entry.MCP != nil {
			return fmt.Errorf("%w: capability entry cannot contain source references", ErrInvalidEntry)
		}
	case KindSkill:
		if entry.Skill == nil || strings.TrimSpace(entry.Skill.SkillID) == "" || strings.TrimSpace(entry.Skill.Version) == "" || entry.MCP != nil {
			return fmt.Errorf("%w: skill reference is incomplete", ErrInvalidEntry)
		}
	case KindMCPTool:
		if entry.MCP == nil || strings.TrimSpace(entry.MCP.ConnectionID) == "" ||
			strings.TrimSpace(entry.MCP.ServerID) == "" || strings.TrimSpace(entry.MCP.SnapshotID) == "" ||
			strings.TrimSpace(entry.MCP.QualifiedToolName) == "" || entry.Skill != nil {
			return fmt.Errorf("%w: MCP reference is incomplete", ErrInvalidEntry)
		}
	}
	return nil
}

func matchesQuery(entry Entry, query Query) bool {
	if query.Kind != "" && entry.Kind != query.Kind {
		return false
	}
	if query.Category != "" && entry.Category != query.Category {
		return false
	}
	if query.Scope != "" && entry.Scope != query.Scope {
		return false
	}
	if query.Status != "" && entry.Status != query.Status {
		return false
	}
	if query.Search == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		entry.Name, entry.DisplayName, entry.Description, entry.CapabilityID,
	}, "\n"))
	return strings.Contains(haystack, query.Search)
}

func entrySortKey(entry Entry) string {
	return strings.Join([]string{
		kindSortPrefix(entry.Kind),
		strings.ToLower(strings.TrimSpace(entry.DisplayName)),
		strings.ToLower(strings.TrimSpace(entry.Name)),
		strings.TrimSpace(entry.Version),
		strings.TrimSpace(entry.ID),
	}, "\x00")
}

func kindSortPrefix(kind string) string {
	switch kind {
	case KindCapability:
		return "0"
	case KindSkill:
		return "1"
	case KindMCPTool:
		return "2"
	default:
		return "9"
	}
}

func queryFilterHash(query Query) string {
	payload := strings.Join([]string{query.Kind, query.Category, query.Scope, query.Status, query.Search}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

func encodeCursor(cursor catalogCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode extension catalog cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(raw string) (catalogCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return catalogCursor{}, fmt.Errorf("%w: malformed cursor", ErrInvalidCursor)
	}
	var cursor catalogCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Version != 1 ||
		strings.TrimSpace(cursor.FilterHash) == "" || strings.TrimSpace(cursor.AfterKey) == "" {
		return catalogCursor{}, fmt.Errorf("%w: malformed cursor", ErrInvalidCursor)
	}
	return cursor, nil
}

func normalizeSources(sources []SourceStatus) []SourceStatus {
	result := append([]SourceStatus(nil), sources...)
	for index := range result {
		result[index].Source = strings.TrimSpace(result[index].Source)
		if result[index].State != SourceStateReady {
			result[index].State = SourceStateDisabled
		}
		if result[index].EntryCount < 0 {
			result[index].EntryCount = 0
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Source < result[j].Source })
	return result
}

func cloneEntries(entries []Entry) []Entry {
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, cloneEntry(entry))
	}
	return result
}

func cloneEntry(entry Entry) Entry {
	clone := entry
	if entry.Skill != nil {
		reference := *entry.Skill
		clone.Skill = &reference
	}
	if entry.MCP != nil {
		reference := *entry.MCP
		clone.MCP = &reference
	}
	return clone
}

func oneOfOrEmpty(value string, candidates ...string) bool {
	return value == "" || oneOf(value, candidates...)
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateText(label, value string, maxRunes int) error {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%w: %s must contain 1..%d characters", ErrInvalidEntry, label, maxRunes)
	}
	return nil
}
