package repository

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestProviderConfigRepositoryFailsClosedWithoutCollection(t *testing.T) {
	repository := &MongoAgentRepository{}
	config := &ProviderConfig{ID: primitive.NewObjectID(), UserID: 7}

	if err := repository.CreateProviderConfig(context.Background(), config); err == nil {
		t.Fatal("CreateProviderConfig() unexpectedly succeeded")
	}
	if err := repository.UpdateProviderConfig(context.Background(), config, 1); err == nil {
		t.Fatal("UpdateProviderConfig() unexpectedly succeeded")
	}
	if _, _, err := repository.ListProviderConfigs(context.Background(), 7, 1, 20); err == nil {
		t.Fatal("ListProviderConfigs() unexpectedly succeeded")
	}
	if _, err := repository.GetProviderConfig(context.Background(), config.ID, 7); err == nil {
		t.Fatal("GetProviderConfig() unexpectedly succeeded")
	}
	if err := repository.RevokeProviderConfig(context.Background(), config.ID, 7, 1); err == nil {
		t.Fatal("RevokeProviderConfig() unexpectedly succeeded")
	}
}
