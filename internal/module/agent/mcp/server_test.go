package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateInternalAddress(t *testing.T) {
	require.NoError(t, validateInternalAddress("127.0.0.1:9200"))
	require.NoError(t, validateInternalAddress("localhost:9200"))
	require.NoError(t, validateInternalAddress("[::1]:9200"))
	require.Error(t, validateInternalAddress("0.0.0.0:9200"))
	require.Error(t, validateInternalAddress(":9200"))
}
