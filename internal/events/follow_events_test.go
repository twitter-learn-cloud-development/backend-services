package events

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFollowEventVersionIdentityAndLegacyJSONCompatibility(t *testing.T) {
	for _, following := range []bool{true, false} {
		e := FollowRelationEvent{FollowerID: 17, FolloweeID: 42, RelationID: 9007199254740997, SchemaVersion: FollowRelationSchemaVersion, EventKey: FollowRelationEventKey(9007199254740997, following), OccurredAtUnixMS: 123}
		require.NoError(t, e.Validate(following))
		body, err := json.Marshal(e)
		require.NoError(t, err)
		var old UserFollowedEvent
		require.NoError(t, json.Unmarshal(body, &old))
		require.Equal(t, e.FollowerID, old.FollowerID)
		require.Equal(t, e.FolloweeID, old.FolloweeID)
		for _, field := range []string{"identity", "version", "key", "time", "self"} {
			invalid := e
			switch field {
			case "identity":
				invalid.RelationID = 0
			case "version":
				invalid.SchemaVersion = "future"
			case "key":
				invalid.EventKey = "wrong"
			case "time":
				invalid.OccurredAtUnixMS = 0
			case "self":
				invalid.FolloweeID = invalid.FollowerID
			}
			require.Error(t, invalid.Validate(following))
		}
	}
}
