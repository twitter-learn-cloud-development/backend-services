package repository

import (
	"testing"

	"github.com/stretchr/testify/require"

	"twitter-clone/internal/domain"
)

func TestBuildFollowerPageUsesRelationIDAsCursor(t *testing.T) {
	page := buildFollowerPage([]domain.Follow{
		{ID: 300, FollowerID: 10},
		{ID: 200, FollowerID: 99},
		{ID: 100, FollowerID: 5},
	}, 2)

	require.Equal(t, []uint64{10, 99}, page.FollowerIDs)
	require.True(t, page.HasMore)
	require.Equal(t, uint64(200), page.NextCursor)
	require.NotEqual(t, page.FollowerIDs[len(page.FollowerIDs)-1], page.NextCursor)
}
