package tool

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDelegatedToolExecutesInjectedBoundary(t *testing.T) {
	delegated := NewDelegatedTool(
		"ReadOnlyMCP",
		"test",
		`{"type":"object"}`,
		func(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"result": inputs["query"]}, nil
		},
	)

	output, err := delegated.Execute(context.Background(), map[string]interface{}{"query": "cloud native"})

	require.NoError(t, err)
	require.Equal(t, "cloud native", output["result"])
}
