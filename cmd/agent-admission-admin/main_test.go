package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	"twitter-clone/internal/module/agent/admission"
)

type adminFake struct {
	initialized, read int
	err               error
}

func (f *adminFake) PolicyDigest() string             { return admission.ConfigDigest(admission.DefaultConfig()) }
func (f *adminFake) Ready(context.Context) error      { f.read++; return f.err }
func (f *adminFake) Initialize(context.Context) error { f.initialized++; return f.err }

func TestAdminOfflineAndApprovalBeforeConnection(t *testing.T) {
	lookup := func(string) (string, bool) { return "", false }
	connected := 0
	open := func(context.Context, admission.Config) (controller, func(), error) {
		connected++
		return nil, nil, errors.New("must stay offline")
	}
	var output bytes.Buffer
	require.NoError(t, run(nil, &output, lookup, open))
	var report map[string]interface{}
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	require.Equal(t, "offline", report["mode"])
	require.Equal(t, admission.ConfigDigest(admission.DefaultConfig()), report["policy_sha256"])
	for _, args := range [][]string{{"--initialize"}, {"--allow-live", "--initialize", "--approved-policy=wrong", "--operator=ops", "--reason=rollout"}, {"--allow-live", "--initialize", "--approved-policy=" + report["policy_sha256"].(string), "--operator= ", "--reason=rollout"}} {
		require.Error(t, run(args, &output, lookup, open))
	}
	require.Zero(t, connected)
}

func TestAdminLiveUsesBoundedReadOrExplicitInitialization(t *testing.T) {
	for _, initialize := range []bool{false, true} {
		fake := &adminFake{}
		closed := false
		open := func(ctx context.Context, _ admission.Config) (controller, func(), error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
			return fake, func() { closed = true }, nil
		}
		args := []string{"--allow-live"}
		if initialize {
			args = append(args, "--initialize", "--approved-policy="+fake.PolicyDigest(), "--operator=ops", "--reason=reviewed")
		}
		var output bytes.Buffer
		require.NoError(t, run(args, &output, func(string) (string, bool) { return "", false }, open))
		require.True(t, closed)
		if initialize {
			require.Equal(t, 1, fake.initialized)
			require.Zero(t, fake.read)
		} else {
			require.Equal(t, 1, fake.read)
			require.Zero(t, fake.initialized)
		}
		require.NotContains(t, output.String(), "reviewed")
		fake.err = admission.ErrUnavailable
		require.ErrorIs(t, run(args, &output, func(string) (string, bool) { return "", false }, open), admission.ErrUnavailable)
	}
}
