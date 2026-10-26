package tool

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	agentModel "twitter-clone/internal/module/agent/model"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/pkg/ai"
)

type workflowGuardFake struct {
	calls, released int
	err             error
}

func (f *workflowGuardFake) AcquireCall(ctx context.Context, _ string, r *agentRuntime.ModelRequest) (context.Context, func(), error) {
	f.calls++
	if f.err != nil {
		return ctx, nil, f.err
	}
	r.MaxOutputTokens = 9
	return ctx, func() { f.released++ }, nil
}
func TestWorkflowSharedGuardDenyAndBoundedOutput(t *testing.T) {
	var calls, outputCap atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			MaxTokens int64 `json:"max_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		outputCap.Store(payload.MaxTokens)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer server.Close()
	guard := &workflowGuardFake{err: errors.New("shared denied")}
	tool := NewLLMChatToolWithOptions(&ai.Client{}, "model", WithLLMCallAdmission(guard), WithLLMEndpointPolicy(agentModel.NewEndpointPolicy("127.0.0.1")))
	input := map[string]interface{}{"prompt": "q", "model": "model", "base_url": server.URL + "/v1", "max_tokens": 100}
	_, err := tool.Execute(context.Background(), input)
	require.ErrorIs(t, err, guard.err)
	require.Zero(t, calls.Load())
	guard.err = nil
	_, err = tool.Execute(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, int64(9), outputCap.Load())
	require.Equal(t, 1, guard.released)
	input["base_url"] = "http://169.254.169.254/v1"
	_, err = tool.Execute(context.Background(), input)
	require.Error(t, err)
	require.Equal(t, 2, guard.calls)
	delete(input, "base_url")
	_, err = tool.Execute(context.Background(), input)
	require.ErrorContains(t, err, "explicit bounded endpoint")
	require.Equal(t, 2, guard.calls)
}
