package main

import (
	"bytes"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReleaseCLIPlanMatrixAndRejectAmbiguousModes(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, run("../..", nil, &out))
	require.Contains(t, out.String(), "schema-migrate")
	out.Reset()
	require.NoError(t, run("../..", []string{"--matrix"}, &out))
	var matrix map[string]interface{}
	require.NoError(t, json.Unmarshal(out.Bytes(), &matrix))
	require.Len(t, matrix, 1)
	require.Contains(t, matrix, "include")
	for _, args := range [][]string{{"--assemble", "--record-image=gateway"}, {"--require-published"}, {"--verify-manifest=../outside"}, {"--record-check=go_test", "--source-sha=bad"}, {"--validate-bases", "--published", "--builder-image=golang:latest", "--runtime-image=alpine:latest"}, {"unexpected"}, {"--matrix", "--out=../outside"}} {
		require.Error(t, run("../..", args, &out))
	}
}
