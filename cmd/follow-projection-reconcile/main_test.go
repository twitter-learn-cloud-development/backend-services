package main

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestFollowMaintenanceDryRunDoesNotReadCredentialsOrConnect(t *testing.T) {
	t.Setenv("FOLLOW_PROJECTION_MYSQL_DSN", "invalid private password")
	t.Setenv("FOLLOW_PROJECTION_REDIS_ADDR", "private.invalid:1")
	var out bytes.Buffer
	require.NoError(t, run([]string{"--pairs=9007199254740997:42", "--mode=repair"}, &out))
	require.Contains(t, out.String(), `"dry_run":true`)
	require.NotContains(t, out.String(), "private")
	require.NotContains(t, out.String(), "9007199254740997")
}
func TestFollowMaintenanceInvalidScopeAndMissingApprovalFailBeforeNetwork(t *testing.T) {
	for _, args := range [][]string{{}, {"--pairs=1:1"}, {"--pairs=0:2"}, {"--author-ids=0"}, {"--author-ids=1,1"}, {"--pairs=1:2", "--author-ids=2"}, {"--author-ids=1", "--mode=delete"}, {"--author-ids=1", "--mode=repair", "--allow-live"}} {
		var out bytes.Buffer
		require.Error(t, run(args, &out))
	}
	_, err := scopes(strings.Repeat("1,", 100)+"1", "")
	require.Error(t, err)
}
