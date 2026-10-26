package admission

import (
	"github.com/stretchr/testify/require"
	"go/ast"
	"go/parser"
	"go/token"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

func TestSharedWiringDefaultsAndRoleDependencyBoundary(t *testing.T) {
	root := "../../../../"
	for _, path := range []string{"docker-compose.yaml", "docker-compose.deploy.yaml", "docker-compose.production.yaml"} {
		body, err := os.ReadFile(root + path)
		require.NoError(t, err)
		var compose struct {
			Services map[string]struct {
				Environment interface{} `yaml:"environment"`
				Healthcheck struct {
					Test []string `yaml:"test"`
				} `yaml:"healthcheck"`
			} `yaml:"services"`
		}
		require.NoError(t, yaml.Unmarshal(body, &compose))
		service := compose.Services["agent-service"]
		raw, err := yaml.Marshal(service.Environment)
		require.NoError(t, err)
		require.Contains(t, string(raw), "AGENT_SHARED_ADMISSION_ENABLED")
		require.Contains(t, string(raw), "${AGENT_SHARED_ADMISSION_ENABLED:-false}")
		require.Contains(t, string(raw), "AGENT_SHARED_COST_MICROS_PER_TOKEN")
		if path == "docker-compose.production.yaml" {
			require.Contains(t, strings.Join(service.Healthcheck.Test, " "), "worker")
			require.Contains(t, strings.Join(service.Healthcheck.Test, " "), "9191")
		}
	}
	source, err := parser.ParseFile(token.NewFileSet(), root+"cmd/agent-service/main.go", nil, 0)
	require.NoError(t, err)
	// MySQL construction and Persona injection must reside in the API-owned branch.
	totalDB, roleDB, roleMemory := 0, 0, 0
	count := func(n ast.Node, name string) int {
		found := 0
		ast.Inspect(n, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == name {
				found++
			}
			return true
		})
		return found
	}
	totalDB = count(source, "NewDB")
	ast.Inspect(source, func(node ast.Node) bool {
		conditional, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		call, ok := conditional.Cond.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "NeedsPersona" {
			roleDB += count(conditional.Body, "NewDB")
		}
		if selector.Sel.Name == "StartsAPI" {
			roleMemory += count(conditional.Body, "NewMemoryManager")
		}
		return true
	})
	require.Equal(t, 1, totalDB)
	require.Equal(t, totalDB, roleDB)
	require.Equal(t, 1, roleMemory)
	memory, err := os.ReadFile(root + "internal/module/agent/workflow/rag/memory.go")
	require.NoError(t, err)
	require.NotContains(t, string(memory), "gorm.io")
	require.Contains(t, string(memory), "domain.AgentPersonaStore")
	rules, err := os.ReadFile(root + "deploy/prometheus/rules/twitter-clone-agent-admission.yml")
	require.NoError(t, err)
	var yamlRules map[string]interface{}
	require.NoError(t, yaml.Unmarshal(rules, &yamlRules))
	require.Contains(t, string(rules), "lease_lost")
	require.NotContains(t, string(rules), "user_id")
}
