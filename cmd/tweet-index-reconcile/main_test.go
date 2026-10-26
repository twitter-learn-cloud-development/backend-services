package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReconcileDryRunDoesNotReadCredentialsOrConnect(t *testing.T) {
	t.Setenv("PROJECTION_MYSQL_DSN", "DO_NOT_READ_PRIVATE_CREDENTIAL")
	var out bytes.Buffer
	for _, mode := range []string{"audit", "repair"} {
		out.Reset()
		require.NoError(t, run([]string{"--tweet-ids=9007199254740997", "--embedding-model=fixture", "--mode=" + mode}, &out))
		require.Contains(t, out.String(), `"dry_run":true`)
		require.NotContains(t, out.String(), "PRIVATE_CREDENTIAL")
		require.NotContains(t, out.String(), "9007199254740997")
	}
}
func TestReconcileGuardsWriteAndExplicitBoundedIDs(t *testing.T) {
	for _, ids := range []string{"", "0", "1,1", "-1", "1.2", strings.Repeat("1,", 100) + "2"} {
		_, err := parseIDs(ids)
		require.Error(t, err)
	}
	var out bytes.Buffer
	require.ErrorContains(t, run([]string{"--allow-live", "--mode=repair", "--tweet-ids=17", "--embedding-model=fixture"}, &out), "authorization")
	require.Empty(t, out.String())
	for _, url := range []string{"https://user:secret@example.org", "file:///tmp", "https://example.org/?key=secret", "https://example.org/path"} {
		require.Error(t, endpoint(url))
	}
}
