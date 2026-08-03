package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var _ externalmcp.Store = (*MongoAgentRepository)(nil)
var _ externalmcp.ProjectStore = (*MongoAgentRepository)(nil)

func (r *MongoAgentRepository) CreateMCPConnection(ctx context.Context, connection *externalmcp.Connection) error {
	if r == nil || r.mcpConnectionColl == nil {
		return errors.New("external MCP connection collection is unavailable")
	}
	if connection == nil {
		return errors.New("external MCP connection is required")
	}
	if _, err := r.mcpConnectionColl.InsertOne(ctx, connection); err != nil {
		return fmt.Errorf("insert external MCP connection failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) UpdateMCPConnection(
	ctx context.Context,
	connection *externalmcp.Connection,
	expectedRevision int64,
) error {
	if r == nil || r.mcpConnectionColl == nil {
		return errors.New("external MCP connection collection is unavailable")
	}
	if connection == nil {
		return errors.New("external MCP connection is required")
	}
	now := time.Now().UTC()
	result, err := r.mcpConnectionColl.UpdateOne(ctx, bson.M{
		"_id": connection.ID, "user_id": connection.UserID,
		"status": externalmcp.ConnectionStatusActive, "revision": expectedRevision,
	}, bson.M{
		"$set": bson.M{
			"name": connection.Name, "transport": connection.Transport, "endpoint": connection.Endpoint,
			"auth_type": connection.AuthType, "credential_source": connection.CredentialSource,
			"managed_credential_ref":     connection.ManagedCredentialRef,
			"managed_credential_version": connection.ManagedCredentialVersion,
			"has_secret":                 connection.HasSecret,
			"encryption_key_id":          connection.EncryptionKeyID, "secret_nonce": connection.SecretNonce,
			"encrypted_credential": connection.EncryptedCredential,
			"credential_version":   connection.CredentialVersion,
			"latest_snapshot_id":   connection.LatestSnapshotID,
			"pending_snapshot_id":  connection.PendingSnapshotID,
			"active_snapshot_id":   connection.ActiveSnapshotID,
			"discovery_status":     connection.DiscoveryStatus,
			"last_error_code":      connection.LastErrorCode,
			"last_checked_at":      connection.LastCheckedAt,
			"tool_policies":        connection.ToolPolicies,
			"first_activated_at":   connection.FirstActivatedAt,
			"updated_at":           now,
		},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return fmt.Errorf("update external MCP connection failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return externalmcp.ErrRevisionConflict
	}
	connection.Revision = expectedRevision + 1
	connection.UpdatedAt = now
	return nil
}

func (r *MongoAgentRepository) ListMCPConnections(
	ctx context.Context,
	userID uint64,
	page, pageSize int,
) ([]*externalmcp.Connection, int64, error) {
	if r == nil || r.mcpConnectionColl == nil {
		return nil, 0, errors.New("external MCP connection collection is unavailable")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter := personalMCPConnectionFilter(userID)
	total, err := r.mcpConnectionColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count external MCP connections failed: %w", err)
	}
	cursor, err := r.mcpConnectionColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}}).
		SetSkip(int64((page-1)*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find external MCP connections failed: %w", err)
	}
	defer cursor.Close(ctx)
	var connections []*externalmcp.Connection
	if err := cursor.All(ctx, &connections); err != nil {
		return nil, 0, fmt.Errorf("decode external MCP connections failed: %w", err)
	}
	return connections, total, nil
}

func (r *MongoAgentRepository) ListMCPConnectionsByAccess(
	ctx context.Context,
	userID uint64,
	projectIDs []string,
	page, pageSize int,
) ([]*externalmcp.Connection, int64, error) {
	if r == nil || r.mcpConnectionColl == nil {
		return nil, 0, errors.New("external MCP connection collection is unavailable")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter := accessibleMCPConnectionFilter(userID, projectIDs)
	total, err := r.mcpConnectionColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count accessible external MCP connections failed: %w", err)
	}
	cursor, err := r.mcpConnectionColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}, {Key: "_id", Value: 1}}).
		SetSkip(int64((page-1)*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find accessible external MCP connections failed: %w", err)
	}
	defer cursor.Close(ctx)
	var connections []*externalmcp.Connection
	if err := cursor.All(ctx, &connections); err != nil {
		return nil, 0, fmt.Errorf("decode accessible external MCP connections failed: %w", err)
	}
	return connections, total, nil
}

func (r *MongoAgentRepository) GetMCPConnection(
	ctx context.Context,
	id string,
	userID uint64,
) (*externalmcp.Connection, error) {
	if r == nil || r.mcpConnectionColl == nil {
		return nil, errors.New("external MCP connection collection is unavailable")
	}
	var connection externalmcp.Connection
	if err := r.mcpConnectionColl.FindOne(ctx, bson.M{"_id": id, "user_id": userID}).Decode(&connection); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, externalmcp.ErrConnectionNotFound
		}
		return nil, fmt.Errorf("find external MCP connection failed: %w", err)
	}
	return &connection, nil
}

func (r *MongoAgentRepository) GetMCPConnectionByID(
	ctx context.Context,
	id string,
) (*externalmcp.Connection, error) {
	if r == nil || r.mcpConnectionColl == nil {
		return nil, errors.New("external MCP connection collection is unavailable")
	}
	var connection externalmcp.Connection
	if err := r.mcpConnectionColl.FindOne(ctx, bson.M{"_id": id}).Decode(&connection); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, externalmcp.ErrConnectionNotFound
		}
		return nil, fmt.Errorf("find external MCP connection by id failed: %w", err)
	}
	return &connection, nil
}

func (r *MongoAgentRepository) GetMCPConnectionByServerID(
	ctx context.Context,
	serverID string,
	userID uint64,
) (*externalmcp.Connection, error) {
	if r == nil || r.mcpConnectionColl == nil {
		return nil, errors.New("external MCP connection collection is unavailable")
	}
	var connection externalmcp.Connection
	if err := r.mcpConnectionColl.FindOne(ctx, bson.M{
		"server_id": serverID, "user_id": userID,
	}).Decode(&connection); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, externalmcp.ErrConnectionNotFound
		}
		return nil, fmt.Errorf("find external MCP connection by server id failed: %w", err)
	}
	return &connection, nil
}

func (r *MongoAgentRepository) GetMCPConnectionByServerIDUnscoped(
	ctx context.Context,
	serverID string,
) (*externalmcp.Connection, error) {
	if r == nil || r.mcpConnectionColl == nil {
		return nil, errors.New("external MCP connection collection is unavailable")
	}
	var connection externalmcp.Connection
	if err := r.mcpConnectionColl.FindOne(ctx, bson.M{"server_id": serverID}).Decode(&connection); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, externalmcp.ErrConnectionNotFound
		}
		return nil, fmt.Errorf("find external MCP connection by global server id failed: %w", err)
	}
	return &connection, nil
}

func (r *MongoAgentRepository) RevokeMCPConnection(
	ctx context.Context,
	id string,
	userID uint64,
	expectedRevision int64,
) error {
	if r == nil || r.mcpConnectionColl == nil {
		return errors.New("external MCP connection collection is unavailable")
	}
	result, err := r.mcpConnectionColl.UpdateOne(ctx, bson.M{
		"_id": id, "user_id": userID,
		"status": externalmcp.ConnectionStatusActive, "revision": expectedRevision,
	}, bson.M{
		"$set": bson.M{
			"status": externalmcp.ConnectionStatusRevoked, "has_secret": false,
			"discovery_status": externalmcp.DiscoveryStatusUnchecked,
			"health_status":    externalmcp.HealthStatusUnknown, "health_failure_count": int64(0),
			"updated_at": time.Now().UTC(),
		},
		"$unset": bson.M{
			"encryption_key_id": "", "secret_nonce": "", "encrypted_credential": "",
			"managed_credential_ref": "", "managed_credential_version": "",
			"latest_snapshot_id": "", "pending_snapshot_id": "", "active_snapshot_id": "",
			"tool_policies": "", "health_error_code": "", "last_health_checked_at": "",
			"last_healthy_at": "", "next_health_check_at": "", "health_lease_owner": "",
			"health_lease_until": "",
		},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return fmt.Errorf("revoke external MCP connection failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return externalmcp.ErrRevisionConflict
	}
	return nil
}

func (r *MongoAgentRepository) ResetMCPConnectionHealth(
	ctx context.Context,
	connectionID string,
	userID uint64,
	nextCheckAt time.Time,
) error {
	if r == nil || r.mcpConnectionColl == nil {
		return errors.New("external MCP connection collection is unavailable")
	}
	result, err := r.mcpConnectionColl.UpdateOne(ctx, bson.M{
		"_id": connectionID, "user_id": userID, "status": externalmcp.ConnectionStatusActive,
	}, bson.M{
		"$set": bson.M{
			"health_status":        externalmcp.HealthStatusUnknown,
			"health_failure_count": int64(0),
			"next_health_check_at": nextCheckAt,
		},
		"$unset": bson.M{
			"health_error_code": "", "last_health_checked_at": "", "last_healthy_at": "",
			"health_lease_owner": "", "health_lease_until": "",
		},
	})
	if err != nil {
		return fmt.Errorf("reset external MCP connection health failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return externalmcp.ErrConnectionNotFound
	}
	return nil
}

func (r *MongoAgentRepository) ClaimMCPConnectionsForHealth(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseUntil time.Time,
	limit int,
) ([]*externalmcp.Connection, error) {
	if r == nil || r.mcpConnectionColl == nil {
		return nil, errors.New("external MCP connection collection is unavailable")
	}
	if owner == "" {
		return nil, errors.New("external MCP health owner is required")
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	filter := bson.M{
		"status": externalmcp.ConnectionStatusActive,
		"$and": bson.A{
			bson.M{"$or": bson.A{
				bson.M{"next_health_check_at": bson.M{"$exists": false}},
				bson.M{"next_health_check_at": bson.M{"$lte": now}},
			}},
			bson.M{"$or": bson.A{
				bson.M{"health_lease_until": bson.M{"$exists": false}},
				bson.M{"health_lease_until": bson.M{"$lte": now}},
			}},
		},
	}
	claimed := make([]*externalmcp.Connection, 0, limit)
	for len(claimed) < limit {
		var connection externalmcp.Connection
		err := r.mcpConnectionColl.FindOneAndUpdate(
			ctx,
			filter,
			bson.M{"$set": bson.M{
				"health_lease_owner": owner,
				"health_lease_until": leaseUntil,
			}},
			options.FindOneAndUpdate().
				SetSort(bson.D{{Key: "next_health_check_at", Value: 1}, {Key: "_id", Value: 1}}).
				SetReturnDocument(options.After),
		).Decode(&connection)
		if errors.Is(err, mongo.ErrNoDocuments) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("claim external MCP health check failed: %w", err)
		}
		claimed = append(claimed, &connection)
	}
	return claimed, nil
}

func (r *MongoAgentRepository) CompleteMCPConnectionHealth(
	ctx context.Context,
	completion externalmcp.HealthCheckCompletion,
) error {
	if r == nil || r.mcpConnectionColl == nil {
		return errors.New("external MCP connection collection is unavailable")
	}
	filter := bson.M{
		"_id": completion.ConnectionID, "user_id": completion.UserID,
		"status":             externalmcp.ConnectionStatusActive,
		"health_lease_owner": completion.LeaseOwner,
	}
	set := bson.M{"next_health_check_at": completion.NextHealthCheckAt}
	unset := bson.M{"health_lease_owner": "", "health_lease_until": ""}
	switch completion.Outcome {
	case externalmcp.HealthOutcomeHealthy:
		set["health_status"] = externalmcp.HealthStatusHealthy
		set["health_failure_count"] = int64(0)
		set["last_health_checked_at"] = completion.CheckedAt
		set["last_healthy_at"] = completion.LastHealthyAt
		unset["health_error_code"] = ""
	case externalmcp.HealthOutcomeFailed:
		set["health_status"] = completion.HealthStatus
		set["health_error_code"] = completion.ErrorCode
		set["health_failure_count"] = completion.FailureCount
		set["last_health_checked_at"] = completion.CheckedAt
	case externalmcp.HealthOutcomeSkipped:
	default:
		return errors.New("external MCP health outcome is invalid")
	}
	result, err := r.mcpConnectionColl.UpdateOne(ctx, filter, bson.M{"$set": set, "$unset": unset})
	if err != nil {
		return fmt.Errorf("complete external MCP health check failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return externalmcp.ErrHealthLeaseLost
	}
	return nil
}

func (r *MongoAgentRepository) SaveMCPToolSnapshot(
	ctx context.Context,
	snapshot *externalmcp.ToolSchemaSnapshot,
) (*externalmcp.ToolSchemaSnapshot, error) {
	if r == nil || r.mcpToolSnapshotColl == nil {
		return nil, errors.New("external MCP snapshot collection is unavailable")
	}
	if snapshot == nil {
		return nil, errors.New("external MCP schema snapshot is required")
	}
	if _, err := r.mcpToolSnapshotColl.InsertOne(ctx, snapshot); err != nil {
		if !mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("insert external MCP schema snapshot failed: %w", err)
		}
		var existing externalmcp.ToolSchemaSnapshot
		findErr := r.mcpToolSnapshotColl.FindOne(ctx, bson.M{
			"user_id": snapshot.UserID, "connection_id": snapshot.ConnectionID,
			"schema_hash": snapshot.SchemaHash,
		}).Decode(&existing)
		if findErr != nil {
			return nil, fmt.Errorf("find existing external MCP schema snapshot failed: %w", findErr)
		}
		return &existing, nil
	}
	return snapshot, nil
}

func (r *MongoAgentRepository) GetMCPToolSnapshot(
	ctx context.Context,
	id, connectionID string,
	userID uint64,
) (*externalmcp.ToolSchemaSnapshot, error) {
	if r == nil || r.mcpToolSnapshotColl == nil {
		return nil, errors.New("external MCP snapshot collection is unavailable")
	}
	var snapshot externalmcp.ToolSchemaSnapshot
	if err := r.mcpToolSnapshotColl.FindOne(ctx, bson.M{
		"_id": id, "connection_id": connectionID, "user_id": userID,
	}).Decode(&snapshot); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, externalmcp.ErrSnapshotNotFound
		}
		return nil, fmt.Errorf("find external MCP schema snapshot failed: %w", err)
	}
	return &snapshot, nil
}

func (r *MongoAgentRepository) ListMCPExecutionBindings(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]externalmcp.ExecutionBinding, error) {
	return r.listMCPExecutionBindings(ctx, personalMCPConnectionFilter(userID), limit)
}

func (r *MongoAgentRepository) ListMCPExecutionBindingsByAccess(
	ctx context.Context,
	userID uint64,
	projectIDs []string,
	limit int,
) ([]externalmcp.ExecutionBinding, error) {
	return r.listMCPExecutionBindings(ctx, accessibleMCPConnectionFilter(userID, projectIDs), limit)
}

func (r *MongoAgentRepository) listMCPExecutionBindings(
	ctx context.Context,
	accessFilter bson.M,
	limit int,
) ([]externalmcp.ExecutionBinding, error) {
	if r == nil || r.mcpConnectionColl == nil || r.mcpToolSnapshotColl == nil {
		return nil, errors.New("external MCP execution collections are unavailable")
	}
	if limit < 1 || limit > 20 {
		limit = 20
	}
	connectionCursor, err := r.mcpConnectionColl.Find(ctx, bson.M{"$and": bson.A{
		accessFilter,
		bson.M{
			"status":             externalmcp.ConnectionStatusActive,
			"discovery_status":   externalmcp.DiscoveryStatusReady,
			"active_snapshot_id": bson.M{"$ne": ""},
			"tool_policies":      bson.M{"$elemMatch": bson.M{"enabled": true}},
		},
	}}, options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}, {Key: "_id", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("find executable external MCP connections failed: %w", err)
	}
	defer connectionCursor.Close(ctx)
	var connections []externalmcp.Connection
	if err := connectionCursor.All(ctx, &connections); err != nil {
		return nil, fmt.Errorf("decode executable external MCP connections failed: %w", err)
	}
	if len(connections) == 0 {
		return nil, nil
	}

	snapshotIDs := make([]string, 0, len(connections))
	for _, connection := range connections {
		snapshotIDs = append(snapshotIDs, connection.ActiveSnapshotID)
	}
	snapshotCursor, err := r.mcpToolSnapshotColl.Find(ctx, bson.M{"_id": bson.M{"$in": snapshotIDs}})
	if err != nil {
		return nil, fmt.Errorf("find executable external MCP snapshots failed: %w", err)
	}
	defer snapshotCursor.Close(ctx)
	var snapshots []externalmcp.ToolSchemaSnapshot
	if err := snapshotCursor.All(ctx, &snapshots); err != nil {
		return nil, fmt.Errorf("decode executable external MCP snapshots failed: %w", err)
	}
	byID := make(map[string]externalmcp.ToolSchemaSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		byID[snapshot.ID] = snapshot
	}
	bindings := make([]externalmcp.ExecutionBinding, 0, len(connections))
	for _, connection := range connections {
		snapshot, ok := byID[connection.ActiveSnapshotID]
		if !ok || snapshot.ConnectionID != connection.ID || snapshot.UserID != connection.UserID {
			continue
		}
		bindings = append(bindings, externalmcp.ExecutionBinding{
			Connection: connection,
			Snapshot:   snapshot,
		})
	}
	return bindings, nil
}

func personalMCPConnectionFilter(userID uint64) bson.M {
	return bson.M{
		"user_id": userID,
		"scope":   bson.M{"$ne": externalmcp.ScopeProject},
	}
}

func accessibleMCPConnectionFilter(userID uint64, projectIDs []string) bson.M {
	clauses := bson.A{personalMCPConnectionFilter(userID)}
	if len(projectIDs) > 0 {
		clauses = append(clauses, bson.M{
			"scope": externalmcp.ScopeProject, "project_id": bson.M{"$in": projectIDs},
		})
	}
	return bson.M{"$or": clauses}
}
