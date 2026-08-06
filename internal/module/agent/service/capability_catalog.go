package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type AgentCapabilityStatus string

const (
	AgentCapabilityAvailable AgentCapabilityStatus = "available"
	AgentCapabilityPlanned   AgentCapabilityStatus = "planned"
)

var ErrCapabilityUnavailable = errors.New("agent capability is unavailable")

// AgentCapabilityDefinition describes a stable product capability. It does not
// grant tool access; profiles and the governed tool executor remain the
// authorization boundary.
type AgentCapabilityDefinition struct {
	ID           string
	Version      string
	Description  string
	Status       AgentCapabilityStatus
	Dependencies []string
}

// AgentCapabilityRoute maps an exact capability set to one execution profile.
// OrderedCapabilityIDs preserves semantic execution order for compositions.
type AgentCapabilityRoute struct {
	CapabilityIDs        []string
	OrderedCapabilityIDs []string
	ExecutionProfile     string
}

type AgentCapabilityCatalog interface {
	List(context.Context) ([]AgentCapabilityDefinition, error)
	ResolvePlan(context.Context, []string) (AgentCapabilityPlan, error)
}

type ImmutableAgentCapabilityCatalog struct {
	definitions map[string]AgentCapabilityDefinition
	routes      map[string]AgentCapabilityRoute
	orderedIDs  []string
}

type BuiltInAgentCapabilityCatalogOption func(*builtInAgentCapabilityCatalogConfig)

type builtInAgentCapabilityCatalogConfig struct {
	webSearchAvailable   bool
	externalMCPAvailable bool
	workflowAvailable    bool
	skillAvailable       bool
}

func WithAvailableExternalMCPCapability() BuiltInAgentCapabilityCatalogOption {
	return func(config *builtInAgentCapabilityCatalogConfig) {
		config.externalMCPAvailable = true
	}
}

func WithAvailableWorkflowCapability() BuiltInAgentCapabilityCatalogOption {
	return func(config *builtInAgentCapabilityCatalogConfig) {
		config.workflowAvailable = true
	}
}

func WithAvailableSkillCapability() BuiltInAgentCapabilityCatalogOption {
	return func(config *builtInAgentCapabilityCatalogConfig) {
		config.skillAvailable = true
	}
}

// WithAvailableWebSearchCapability enables routes only when a real provider
// was successfully configured. The default catalog keeps the capability
// visible as planned and fails closed.
func WithAvailableWebSearchCapability() BuiltInAgentCapabilityCatalogOption {
	return func(config *builtInAgentCapabilityCatalogConfig) {
		config.webSearchAvailable = true
	}
}

func NewAgentCapabilityCatalog(
	definitions []AgentCapabilityDefinition,
	routes []AgentCapabilityRoute,
) (*ImmutableAgentCapabilityCatalog, error) {
	catalog := &ImmutableAgentCapabilityCatalog{
		definitions: make(map[string]AgentCapabilityDefinition, len(definitions)),
		routes:      make(map[string]AgentCapabilityRoute, len(routes)),
	}
	for _, definition := range definitions {
		normalized, err := normalizeCapabilityDefinition(definition)
		if err != nil {
			return nil, err
		}
		if _, exists := catalog.definitions[normalized.ID]; exists {
			return nil, fmt.Errorf("duplicate agent capability %q", normalized.ID)
		}
		catalog.definitions[normalized.ID] = normalized
		catalog.orderedIDs = append(catalog.orderedIDs, normalized.ID)
	}
	sort.Strings(catalog.orderedIDs)

	if err := validateCapabilityDependencies(catalog.definitions); err != nil {
		return nil, err
	}
	for _, route := range routes {
		normalized, key, err := normalizeCapabilityRoute(route, catalog.definitions)
		if err != nil {
			return nil, err
		}
		if _, exists := catalog.routes[key]; exists {
			return nil, fmt.Errorf("duplicate agent capability route for %q", key)
		}
		catalog.routes[key] = normalized
	}
	return catalog, nil
}

func NewBuiltInAgentCapabilityCatalog(
	options ...BuiltInAgentCapabilityCatalogOption,
) (*ImmutableAgentCapabilityCatalog, error) {
	config := builtInAgentCapabilityCatalogConfig{}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	webSearchStatus := AgentCapabilityPlanned
	if config.webSearchAvailable {
		webSearchStatus = AgentCapabilityAvailable
	}
	externalMCPStatus := AgentCapabilityPlanned
	if config.externalMCPAvailable {
		externalMCPStatus = AgentCapabilityAvailable
	}
	workflowStatus := AgentCapabilityPlanned
	if config.workflowAvailable {
		workflowStatus = AgentCapabilityAvailable
	}
	skillStatus := AgentCapabilityPlanned
	if config.skillAvailable {
		skillStatus = AgentCapabilityAvailable
	}
	definitions := []AgentCapabilityDefinition{
		{
			ID: CapabilityConversationReply, Version: "v1",
			Description: "Continue a dialogue and answer without requiring tools.",
			Status:      AgentCapabilityAvailable,
		},
		{
			ID: CapabilityPlatformSearch, Version: "v1",
			Description: "Search authenticated first-party platform content.",
			Status:      AgentCapabilityAvailable,
		},
		{
			ID: CapabilityWebSearch, Version: "v1",
			Description: "Search the public web through a governed provider adapter.",
			Status:      webSearchStatus,
		},
		{
			ID: CapabilityContentDraft, Version: "v1",
			Description: "Create or revise publishable content drafts without publishing.",
			Status:      AgentCapabilityAvailable,
		},
		{
			ID: CapabilityExternalMCP, Version: "v1",
			Description: "Use explicitly enabled read-only tools from user-owned external MCP connections.",
			Status:      externalMCPStatus,
		},
		{
			ID: CapabilityWorkflowRun, Version: "v1",
			Description: "Run explicitly published immutable workflows under current tool, approval and continuation policy.",
			Status:      workflowStatus,
		},
		{
			ID: CapabilitySkillRun, Version: "v1",
			Description: "Run one explicitly selected immutable user Skill version.",
			Status:      skillStatus,
		},
	}
	routes := []AgentCapabilityRoute{
		{
			CapabilityIDs:        []string{CapabilityConversationReply},
			OrderedCapabilityIDs: []string{CapabilityConversationReply},
			ExecutionProfile:     ExecutionProfileRuntimeChat,
		},
		{
			CapabilityIDs:        []string{CapabilityPlatformSearch},
			OrderedCapabilityIDs: []string{CapabilityPlatformSearch},
			ExecutionProfile:     ExecutionProfileRuntimePlatformSearch,
		},
		{
			CapabilityIDs:        []string{CapabilityContentDraft},
			OrderedCapabilityIDs: []string{CapabilityContentDraft},
			ExecutionProfile:     ExecutionProfileRuntimeDraft,
		},
		{
			CapabilityIDs:        []string{CapabilityPlatformSearch, CapabilityContentDraft},
			OrderedCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
			ExecutionProfile:     ExecutionProfileRuntimeResearchDraft,
		},
	}
	if config.webSearchAvailable {
		routes = append(routes,
			AgentCapabilityRoute{
				CapabilityIDs:        []string{CapabilityWebSearch},
				OrderedCapabilityIDs: []string{CapabilityWebSearch},
				ExecutionProfile:     ExecutionProfileRuntimeWebSearch,
			},
			AgentCapabilityRoute{
				CapabilityIDs:        []string{CapabilityWebSearch, CapabilityContentDraft},
				OrderedCapabilityIDs: []string{CapabilityWebSearch, CapabilityContentDraft},
				ExecutionProfile:     ExecutionProfileRuntimeWebDraft,
			},
		)
	}
	if config.externalMCPAvailable {
		routes = append(routes, AgentCapabilityRoute{
			CapabilityIDs:        []string{CapabilityExternalMCP},
			OrderedCapabilityIDs: []string{CapabilityExternalMCP},
			ExecutionProfile:     ExecutionProfileRuntimeExternalMCP,
		})
	}
	if config.workflowAvailable {
		routes = append(routes, AgentCapabilityRoute{
			CapabilityIDs:        []string{CapabilityWorkflowRun},
			OrderedCapabilityIDs: []string{CapabilityWorkflowRun},
			ExecutionProfile:     ExecutionProfileRuntimeWorkflow,
		})
	}
	if config.skillAvailable {
		routes = append(routes, AgentCapabilityRoute{
			CapabilityIDs:        []string{CapabilitySkillRun},
			OrderedCapabilityIDs: []string{CapabilitySkillRun},
			ExecutionProfile:     ExecutionProfileRuntimeSkill,
		})
	}
	return NewAgentCapabilityCatalog(definitions, routes)
}

func (c *ImmutableAgentCapabilityCatalog) List(ctx context.Context) ([]AgentCapabilityDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("agent capability catalog is not configured")
	}
	result := make([]AgentCapabilityDefinition, 0, len(c.orderedIDs))
	for _, id := range c.orderedIDs {
		result = append(result, cloneCapabilityDefinition(c.definitions[id]))
	}
	return result, nil
}

func (c *ImmutableAgentCapabilityCatalog) ResolvePlan(
	ctx context.Context,
	capabilityIDs []string,
) (AgentCapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return AgentCapabilityPlan{}, err
	}
	if c == nil {
		return AgentCapabilityPlan{}, errors.New("agent capability catalog is not configured")
	}
	requested := uniqueCapabilityIDs(capabilityIDs)
	if len(requested) == 0 {
		return AgentCapabilityPlan{}, fmt.Errorf("%w: at least one capability is required", ErrInvalidUnifiedAgentRequest)
	}
	for _, id := range requested {
		definition, exists := c.definitions[id]
		if !exists {
			return AgentCapabilityPlan{}, fmt.Errorf("%w: %q", ErrUnsupportedCapability, id)
		}
		if definition.Status != AgentCapabilityAvailable {
			return AgentCapabilityPlan{}, fmt.Errorf("%w: %q is %s", ErrCapabilityUnavailable, id, definition.Status)
		}
	}

	route, exists := c.routes[capabilitySetKey(requested)]
	if !exists {
		return AgentCapabilityPlan{}, fmt.Errorf(
			"%w: no execution route for %s",
			ErrCapabilityCompositionPending,
			strings.Join(requested, ", "),
		)
	}
	return AgentCapabilityPlan{
		ExecutionProfile: route.ExecutionProfile,
		CapabilityIDs:    append([]string(nil), route.OrderedCapabilityIDs...),
	}, nil
}

func normalizeCapabilityDefinition(definition AgentCapabilityDefinition) (AgentCapabilityDefinition, error) {
	definition.ID = strings.TrimSpace(definition.ID)
	definition.Version = strings.TrimSpace(definition.Version)
	definition.Description = strings.TrimSpace(definition.Description)
	definition.Dependencies = uniqueCapabilityIDs(definition.Dependencies)
	if definition.ID == "" {
		return AgentCapabilityDefinition{}, errors.New("agent capability ID is required")
	}
	if definition.Version == "" {
		return AgentCapabilityDefinition{}, fmt.Errorf("agent capability %q version is required", definition.ID)
	}
	switch definition.Status {
	case AgentCapabilityAvailable, AgentCapabilityPlanned:
	default:
		return AgentCapabilityDefinition{}, fmt.Errorf(
			"agent capability %q has invalid status %q",
			definition.ID,
			definition.Status,
		)
	}
	return cloneCapabilityDefinition(definition), nil
}

func normalizeCapabilityRoute(
	route AgentCapabilityRoute,
	definitions map[string]AgentCapabilityDefinition,
) (AgentCapabilityRoute, string, error) {
	route.CapabilityIDs = uniqueCapabilityIDs(route.CapabilityIDs)
	route.OrderedCapabilityIDs = uniqueCapabilityIDs(route.OrderedCapabilityIDs)
	route.ExecutionProfile = strings.TrimSpace(route.ExecutionProfile)
	if len(route.CapabilityIDs) == 0 || route.ExecutionProfile == "" {
		return AgentCapabilityRoute{}, "", errors.New("agent capability route requires capabilities and an execution profile")
	}
	if len(route.OrderedCapabilityIDs) == 0 {
		route.OrderedCapabilityIDs = append([]string(nil), route.CapabilityIDs...)
	}
	if capabilitySetKey(route.CapabilityIDs) != capabilitySetKey(route.OrderedCapabilityIDs) {
		return AgentCapabilityRoute{}, "", fmt.Errorf(
			"agent capability route %q ordering must contain the same capabilities",
			route.ExecutionProfile,
		)
	}
	for _, id := range route.CapabilityIDs {
		definition, exists := definitions[id]
		if !exists {
			return AgentCapabilityRoute{}, "", fmt.Errorf(
				"agent capability route %q references unknown capability %q",
				route.ExecutionProfile,
				id,
			)
		}
		if definition.Status != AgentCapabilityAvailable {
			return AgentCapabilityRoute{}, "", fmt.Errorf(
				"agent capability route %q references unavailable capability %q",
				route.ExecutionProfile,
				id,
			)
		}
	}
	positions := make(map[string]int, len(route.OrderedCapabilityIDs))
	for index, id := range route.OrderedCapabilityIDs {
		positions[id] = index
	}
	for _, id := range route.OrderedCapabilityIDs {
		for _, dependency := range definitions[id].Dependencies {
			dependencyIndex, included := positions[dependency]
			if !included {
				return AgentCapabilityRoute{}, "", fmt.Errorf(
					"agent capability route %q omits dependency %q required by %q",
					route.ExecutionProfile,
					dependency,
					id,
				)
			}
			if dependencyIndex >= positions[id] {
				return AgentCapabilityRoute{}, "", fmt.Errorf(
					"agent capability route %q must order dependency %q before %q",
					route.ExecutionProfile,
					dependency,
					id,
				)
			}
		}
	}
	return AgentCapabilityRoute{
		CapabilityIDs:        append([]string(nil), route.CapabilityIDs...),
		OrderedCapabilityIDs: append([]string(nil), route.OrderedCapabilityIDs...),
		ExecutionProfile:     route.ExecutionProfile,
	}, capabilitySetKey(route.CapabilityIDs), nil
}

func validateCapabilityDependencies(definitions map[string]AgentCapabilityDefinition) error {
	for id, definition := range definitions {
		for _, dependency := range definition.Dependencies {
			if dependency == id {
				return fmt.Errorf("agent capability %q depends on itself", id)
			}
			if _, exists := definitions[dependency]; !exists {
				return fmt.Errorf("agent capability %q depends on unknown capability %q", id, dependency)
			}
		}
	}

	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(definitions))
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case visiting:
			return fmt.Errorf("agent capability dependency cycle includes %q", id)
		case visited:
			return nil
		}
		state[id] = visiting
		for _, dependency := range definitions[id].Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = visited
		return nil
	}
	for id := range definitions {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func capabilitySetKey(capabilityIDs []string) string {
	normalized := uniqueCapabilityIDs(capabilityIDs)
	sort.Strings(normalized)
	return strings.Join(normalized, "\x1f")
}

func cloneCapabilityDefinition(definition AgentCapabilityDefinition) AgentCapabilityDefinition {
	definition.Dependencies = append([]string(nil), definition.Dependencies...)
	return definition
}
