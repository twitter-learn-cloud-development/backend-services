package schema

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestAccessPlanRestrictsWritesAndIdentifiers(t *testing.T) {
	for _, role := range []string{"user-service", "tweet-service", "follow-service", "messenger-service", "notification-service", "consumer", "canal-relay", "agent-service"} {
		plan, err := BuildAccessPlan(role, "twitter", "svc_account", "10.20.%")
		require.NoError(t, err)
		for _, sql := range plan.SQL {
			require.NotContains(t, sql, "ALL")
			require.NotContains(t, sql, "CREATE")
			require.NotContains(t, sql, "ALTER")
			require.NotContains(t, sql, "DROP")
			require.NotContains(t, sql, "GRANT OPTION")
		}
		for _, item := range plan.Access {
			if (role == "tweet-service" && item.Table == "follows") || (role == "consumer" && (item.Table == "tweets" || item.Table == "follows")) {
				require.Equal(t, []string{"SELECT"}, item.Privileges)
			}
			if role == "agent-service" {
				require.Equal(t, "agent_personas", item.Table)
			}
		}
	}
	for _, target := range []struct{ db, user, host string }{{"twitter;DROP", "svc", "%"}, {"twitter", "root", "%"}, {"twitter", "svc", "' OR 1=1"}, {"twitter", "svc", ""}} {
		_, err := BuildAccessPlan("user-service", target.db, target.user, target.host)
		require.Error(t, err)
	}
	plan, err := BuildAccessPlan("tweet-service", "twitter", "tweet_service", "10.0.%")
	require.NoError(t, err)
	for _, sql := range plan.SQL {
		require.False(t, strings.Contains(sql, "`twitter`.*"))
	}
}
