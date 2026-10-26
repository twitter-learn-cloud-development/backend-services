package main

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPublicationMaintenanceDryRunAndAuthorizationGuards(t *testing.T) {
	t.Setenv("PUBLICATION_MYSQL_DSN", "PRIVATE_CREDENTIAL_DO_NOT_READ")
	var out bytes.Buffer
	for _, mode := range []string{"inspect", "replay", "retention-plan"} {
		out.Reset()
		require.NoError(t, run([]string{"--event-ids=17", "--mode=" + mode}, &out))
		require.Contains(t, out.String(), `"dry_run":true`)
		require.Contains(t, out.String(), `"deletion_supported":false`)
		require.NotContains(t, out.String(), "PRIVATE_CREDENTIAL")
	}
	out.Reset()
	require.ErrorContains(t, run([]string{"--allow-live", "--mode=replay", "--event-ids=17"}, &out), "authorization")
	require.Empty(t, out.String())
	require.Error(t, run([]string{"--event-ids=17", "--mode=delete"}, &out))
}
