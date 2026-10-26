package main

import (
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCanalResumePolicyRejectsUnreviewedBootstrapAndHistoryGaps(t *testing.T) {
	require.Error(t, validateResumePolicy(mysql.Position{}, false, false, false))
	require.NoError(t, validateResumePolicy(mysql.Position{}, false, true, false))
	saved := mysql.Position{Name: "bin.1", Pos: 100}
	require.Error(t, validateResumePolicy(saved, true, false, false))
	require.NoError(t, validateResumePolicy(saved, true, false, true))
	require.NoError(t, validateResumePolicy(saved, false, false, false))
}
