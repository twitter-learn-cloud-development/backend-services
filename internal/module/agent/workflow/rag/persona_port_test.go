package rag

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type personaFake struct {
	user  uint64
	value string
	err   error
}

func (f *personaFake) Get(ctx context.Context, user uint64) (string, error) {
	f.user = user
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return f.value, f.err
}
func (f *personaFake) Put(ctx context.Context, user uint64, value string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.user = user
	f.value = value
	return f.err
}
func TestPersonaPortPreservesOwnerFailureAndCancellation(t *testing.T) {
	f := &personaFake{value: "Go"}
	manager := NewMemoryManager(f, nil, nil, nil, "", "")
	value, err := manager.GetPersona(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "Go", value)
	require.Equal(t, uint64(7), f.user)
	require.NoError(t, manager.SavePersona(context.Background(), 8, "new"))
	require.Equal(t, uint64(8), f.user)
	f.err = errors.New("unavailable")
	_, err = manager.GetPersona(context.Background(), 8)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, manager.SavePersona(ctx, 8, "discard"), context.Canceled)
}
