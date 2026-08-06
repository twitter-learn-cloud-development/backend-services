package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"twitter-clone/internal/module/agent/profile"
)

func TestDecodeProfileCatalogChangeStrictContract(t *testing.T) {
	payload := `{"schema":"agent_profile_catalog_change_v1","operation_id":"op-1","profile_id":"assist.draft","version_revision":2,"occurred_at_unix_millis":1784512800000}`
	event, err := decodeProfileCatalogChange([]byte(payload))
	if err != nil {
		t.Fatalf("decodeProfileCatalogChange() error = %v", err)
	}
	if event.Schema != profile.CatalogChangeSchemaV1 || event.OperationID != "op-1" || event.VersionRevision != 2 {
		t.Fatalf("event = %+v", event)
	}
}

func TestRedisProfileCatalogChangeBusPublishesValidatedEvent(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	bus, err := NewRedisProfileCatalogChangeBus(client, "agent.profile.test")
	if err != nil {
		t.Fatalf("NewRedisProfileCatalogChangeBus() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	subscription, err := bus.SubscribeCatalogChanges(ctx)
	if err != nil {
		t.Fatalf("SubscribeCatalogChanges() error = %v", err)
	}
	defer subscription.Close()
	want := profile.CatalogChangeEvent{
		Schema:               profile.CatalogChangeSchemaV1,
		OperationID:          "operation-1",
		ProfileID:            "assist.draft",
		ReleaseRevision:      3,
		OccurredAtUnixMillis: time.Now().UnixMilli(),
	}
	if err := bus.PublishCatalogChange(ctx, want); err != nil {
		t.Fatalf("PublishCatalogChange() error = %v", err)
	}
	select {
	case got := <-subscription.Events():
		if got.OperationID != want.OperationID || got.ReleaseRevision != want.ReleaseRevision {
			t.Fatalf("event = %+v, want %+v", got, want)
		}
	case err := <-subscription.Errors():
		t.Fatalf("subscription error = %v", err)
	case <-ctx.Done():
		t.Fatalf("event was not delivered: %v", ctx.Err())
	}
}

func TestDecodeProfileCatalogChangeRejectsUnknownAndInvalidPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "unknown field", payload: `{"schema":"agent_profile_catalog_change_v1","operation_id":"op-1","profile_id":"p","occurred_at_unix_millis":1,"snapshot_json":"secret"}`, want: "unknown field"},
		{name: "schema", payload: `{"schema":"v2","operation_id":"op-1","profile_id":"p","occurred_at_unix_millis":1}`, want: "unsupported"},
		{name: "trailing", payload: `{"schema":"agent_profile_catalog_change_v1","operation_id":"op-1","profile_id":"p","occurred_at_unix_millis":1}{}`, want: "trailing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeProfileCatalogChange([]byte(test.payload))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}
