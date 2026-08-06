package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/dsl"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

var ErrPlaintextWorkflowCredential = errors.New("plaintext workflow credential is forbidden")

func validateWorkflowSecurity(workflow *dsl.WorkflowDSL) error {
	if workflow == nil {
		return nil
	}
	for _, node := range workflow.Nodes {
		if len(node.Properties) == 0 {
			continue
		}
		var properties any
		if err := json.Unmarshal(node.Properties, &properties); err != nil {
			return fmt.Errorf("decode node %s properties: %w", node.ID, err)
		}
		if path, found := findPlaintextCredential(properties, "properties"); found {
			return fmt.Errorf("%w: node %s field %s; use provider_config_id or credential_ref", ErrPlaintextWorkflowCredential, node.ID, path)
		}
		if err := validateProviderConfigReferences(properties, "properties"); err != nil {
			return fmt.Errorf("node %s: %w", node.ID, err)
		}
	}
	return nil
}

func validateWorkflowInputSecurity(inputs map[string]interface{}) error {
	if path, found := findPlaintextCredential(inputs, "input"); found {
		return fmt.Errorf("%w: field %s; use provider_config_id or credential_ref", ErrPlaintextWorkflowCredential, path)
	}
	return validateProviderConfigReferences(inputs, "input")
}

func validateProviderConfigReferences(value any, path string) error {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			childPath := path + "." + key
			normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(key)))
			if normalized == "providerconfigid" {
				text, ok := child.(string)
				if !ok || strings.TrimSpace(text) == "" {
					continue
				}
				if _, err := primitive.ObjectIDFromHex(strings.TrimSpace(text)); err != nil {
					return fmt.Errorf("field %s must be a valid provider config id", childPath)
				}
			}
			if err := validateProviderConfigReferences(child, childPath); err != nil {
				return err
			}
		}
	case []interface{}:
		for index, child := range typed {
			if err := validateProviderConfigReferences(child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func findPlaintextCredential(value any, path string) (string, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			childPath := path + "." + key
			if isPlaintextCredentialKey(key) && hasCredentialValue(child) {
				return childPath, true
			}
			if foundPath, found := findPlaintextCredential(child, childPath); found {
				return foundPath, true
			}
		}
	case []interface{}:
		for index, child := range typed {
			if foundPath, found := findPlaintextCredential(child, fmt.Sprintf("%s[%d]", path, index)); found {
				return foundPath, true
			}
		}
	}
	return "", false
}

func isPlaintextCredentialKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	return normalized == "apikey"
}

func hasCredentialValue(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

func redactWorkflowDefinition(workflow *repository.WorkflowDefinition) (*repository.WorkflowDefinition, error) {
	if workflow == nil {
		return nil, nil
	}
	cloned := *workflow
	redacted, err := redactWorkflowDSLSecrets(workflow.DSLJSON)
	if err != nil {
		return nil, err
	}
	cloned.DSLJSON = redacted
	return &cloned, nil
}

func redactWorkflowRevision(revision *repository.WorkflowRevision) (*repository.WorkflowRevision, error) {
	if revision == nil {
		return nil, nil
	}
	cloned := *revision
	redacted, err := redactWorkflowDSLSecrets(revision.DSLJSON)
	if err != nil {
		return nil, err
	}
	cloned.DSLJSON = redacted
	return &cloned, nil
}

func redactWorkflowDSLSecrets(raw string) (string, error) {
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", fmt.Errorf("redact workflow DSL: %w", err)
	}
	redactPlaintextCredentials(payload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode redacted workflow DSL: %w", err)
	}
	return string(encoded), nil
}

func redactPlaintextCredentials(value any) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			if isPlaintextCredentialKey(key) {
				delete(typed, key)
				continue
			}
			redactPlaintextCredentials(child)
		}
	case []interface{}:
		for _, child := range typed {
			redactPlaintextCredentials(child)
		}
	}
}
