package schema

import (
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

func TestRuntimeEntrypointsKeepDDLAndCredentialsSeparate(t *testing.T) {
	roles := map[string]string{"user-service": "USER_SERVICE", "tweet-service": "TWEET_SERVICE", "follow-service": "FOLLOW_SERVICE", "messenger-service": "MESSENGER_SERVICE", "notification-service": "NOTIFICATION_SERVICE", "consumer": "CONSUMER", "agent-service": "AGENT_SERVICE", "canal-relay": "CANAL_RELAY"}
	body, err := os.ReadFile("../../../docker-compose.production.yaml")
	require.NoError(t, err)
	var compose struct {
		Services map[string]struct {
			Environment map[string]interface{} `yaml:"environment"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(body, &compose))
	for role, prefix := range roles {
		body, err := os.ReadFile("../../../cmd/" + role + "/main.go")
		require.NoError(t, err)
		require.Contains(t, string(body), `persistence.ServiceDBConfig("`+role+`")`)
		require.Contains(t, string(body), `schema.CheckRuntime(`)
		require.NotContains(t, string(body), "AutoMigrate(")
		env := compose.Services[role].Environment
		require.Equal(t, "production", env["APP_ENV"])
		require.Equal(t, "true", env["DB_REQUIRE_SERVICE_CREDENTIALS"])
		require.Contains(t, env, prefix+"_DB_USER")
		require.Contains(t, env, prefix+"_DB_PASSWORD")
	}
	memory, err := os.ReadFile("../../module/agent/workflow/rag/memory.go")
	require.NoError(t, err)
	require.NotContains(t, string(memory), "AutoMigrate(")
	docker, err := os.ReadFile("../../../deploy/docker/schema-migrate.dockerfile")
	require.NoError(t, err)
	require.Contains(t, string(docker), "./cmd/schema-migrate")
	require.False(t, strings.Contains(string(docker), "./cmd/schema-migrate/main.go"))
	job, err := os.ReadFile("../../../deploy/compose/schema-migrate.yaml")
	require.NoError(t, err)
	var parsed map[string]interface{}
	require.NoError(t, yaml.Unmarshal(job, &parsed))
	require.NotContains(t, string(job), "--execute")
	require.NotContains(t, string(job), "--allow-live")
}
