package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"twitter-clone/pkg/qdrant"
)

const (
	sharedCollection        = "agent_episodic_memory"
	sharedPayloadSchema     = "shared_user_payload_v1"
	verificationSampleLimit = 100
)

type migrationStats struct {
	Scanned  int
	Migrated int
	Skipped  int
}

type verificationStats struct {
	UserID                uint64   `json:"user_id"`
	LegacyCollection      string   `json:"legacy_collection"`
	SharedCollection      string   `json:"shared_collection"`
	SourceValidPoints     int      `json:"source_valid_points"`
	SharedUserPoints      int      `json:"shared_user_points"`
	MissingPointCount     int      `json:"missing_point_count"`
	UnexpectedPointCount  int      `json:"unexpected_point_count"`
	DuplicateSourcePoints int      `json:"duplicate_source_points"`
	DuplicateSharedPoints int      `json:"duplicate_shared_points"`
	InvalidTenantPoints   int      `json:"invalid_tenant_points"`
	InvalidSchemaPoints   int      `json:"invalid_schema_points"`
	MissingPointIDs       []string `json:"missing_point_ids,omitempty"`
	UnexpectedPointIDs    []string `json:"unexpected_point_ids,omitempty"`
	Verified              bool     `json:"verified"`
}

type verificationReport struct {
	GeneratedAt      time.Time           `json:"generated_at"`
	SharedCollection string              `json:"shared_collection"`
	Users            []verificationStats `json:"users"`
	Verified         bool                `json:"verified"`
}

func main() {
	qdrantURL := flag.String("qdrant-url", envOrDefault("QDRANT_URL", "http://localhost:6333"), "Qdrant REST URL")
	userIDs := flag.String("user-ids", "", "comma-separated user IDs to migrate")
	batchSize := flag.Int("batch-size", 100, "points per scroll page")
	dryRun := flag.Bool("dry-run", false, "scan and validate without writing")
	deleteLegacy := flag.Bool("delete-legacy", false, "delete each legacy collection after a successful migration")
	verifyOnly := flag.Bool("verify-only", false, "read-only verify source IDs, tenant payloads and shared collection schema")
	reportPath := flag.String("report", "", "optional JSON report path for --verify-only")
	flag.Parse()

	users, err := parseUserIDs(*userIDs)
	if err != nil {
		log.Fatalf("invalid --user-ids: %v", err)
	}
	if len(users) == 0 {
		log.Fatal("--user-ids is required; the command intentionally does not enumerate tenant collections")
	}
	if *batchSize <= 0 || *batchSize > 1000 {
		log.Fatal("--batch-size must be between 1 and 1000")
	}
	if *deleteLegacy && *dryRun {
		log.Fatal("--delete-legacy cannot be combined with --dry-run")
	}
	if *verifyOnly && (*deleteLegacy || *dryRun) {
		log.Fatal("--verify-only cannot be combined with --delete-legacy or --dry-run")
	}
	if *reportPath != "" && !*verifyOnly {
		log.Fatal("--report requires --verify-only")
	}

	client := qdrant.NewClient(strings.TrimRight(strings.TrimSpace(*qdrantURL), "/"))
	ctx := context.Background()
	if *verifyOnly {
		report, err := verifyMigrations(ctx, client, users, *batchSize)
		if err != nil {
			log.Fatalf("verify migration: %v", err)
		}
		if *reportPath != "" {
			if err := writeVerificationReport(*reportPath, report); err != nil {
				log.Fatalf("write verification report: %v", err)
			}
		}
		for _, stats := range report.Users {
			log.Printf("verified user=%d source=%s source_valid=%d shared_user=%d missing=%d unexpected=%d invalid_tenant=%d invalid_schema=%d verified=%t", stats.UserID, stats.LegacyCollection, stats.SourceValidPoints, stats.SharedUserPoints, stats.MissingPointCount, stats.UnexpectedPointCount, stats.InvalidTenantPoints, stats.InvalidSchemaPoints, stats.Verified)
		}
		if !report.Verified {
			log.Fatal("migration verification failed; no legacy collection was deleted")
		}
		return
	}

	for _, userID := range users {
		legacy := fmt.Sprintf("episodic_user_%d", userID)
		stats, err := migrateCollection(ctx, client, legacy, sharedCollection, userID, *batchSize, *dryRun)
		if err != nil {
			log.Fatalf("migrate user %d from %s: %v", userID, legacy, err)
		}
		if *deleteLegacy {
			if err := client.DeleteCollection(ctx, legacy); err != nil {
				log.Fatalf("delete legacy collection %s: %v", legacy, err)
			}
		}
		log.Printf("migrated user=%d source=%s scanned=%d migrated=%d skipped=%d dry_run=%t", userID, legacy, stats.Scanned, stats.Migrated, stats.Skipped, *dryRun)
	}
}

func migrateCollection(ctx context.Context, client *qdrant.Client, source, target string, userID uint64, batchSize int, dryRun bool) (migrationStats, error) {
	var stats migrationStats
	var offset interface{}
	for {
		points, nextOffset, err := client.Scroll(ctx, source, batchSize, offset)
		if err != nil {
			return stats, err
		}
		if len(points) == 0 {
			return stats, nil
		}
		for _, point := range points {
			stats.Scanned++
			if strings.TrimSpace(point.ID) == "" || len(point.Vector) == 0 {
				stats.Skipped++
				continue
			}
			payload := clonePayload(point.Payload)
			payload["user_id"] = fmt.Sprintf("%d", userID)
			payload["collection_schema"] = sharedPayloadSchema
			payload["migrated_from_collection"] = source
			payload["migrated_at"] = time.Now().Unix()
			if !dryRun {
				legacyTweetID, _ := payload["tweet_id"].(string)
				if err := client.UpsertPointWithID(ctx, target, point.ID, point.Vector, payload, legacyTweetID); err != nil {
					return stats, fmt.Errorf("upsert point %s: %w", point.ID, err)
				}
			}
			stats.Migrated++
		}
		if nextOffset == nil || fmt.Sprint(nextOffset) == "" || fmt.Sprint(nextOffset) == fmt.Sprint(offset) {
			return stats, nil
		}
		offset = nextOffset
	}
}

func verifyMigrations(ctx context.Context, client *qdrant.Client, users []uint64, batchSize int) (verificationReport, error) {
	report := verificationReport{
		GeneratedAt:      time.Now().UTC(),
		SharedCollection: sharedCollection,
		Users:            make([]verificationStats, 0, len(users)),
		Verified:         true,
	}
	for _, userID := range users {
		stats, err := verifyUserMigration(ctx, client, userID, batchSize)
		if err != nil {
			return report, fmt.Errorf("user %d: %w", userID, err)
		}
		report.Users = append(report.Users, stats)
		report.Verified = report.Verified && stats.Verified
	}
	return report, nil
}

func verifyUserMigration(ctx context.Context, client *qdrant.Client, userID uint64, batchSize int) (verificationStats, error) {
	legacy := fmt.Sprintf("episodic_user_%d", userID)
	stats := verificationStats{
		UserID:           userID,
		LegacyCollection: legacy,
		SharedCollection: sharedCollection,
	}

	sourceIDs := make(map[string]struct{})
	_, err := scanCollection(ctx, client, legacy, batchSize, nil, func(point qdrant.Point) error {
		if strings.TrimSpace(point.ID) == "" || len(point.Vector) == 0 {
			return nil
		}
		if _, exists := sourceIDs[point.ID]; exists {
			stats.DuplicateSourcePoints++
			return nil
		}
		sourceIDs[point.ID] = struct{}{}
		return nil
	})
	if err != nil {
		return stats, fmt.Errorf("scan source collection %s: %w", legacy, err)
	}
	stats.SourceValidPoints = len(sourceIDs)

	sharedIDs := make(map[string]struct{})
	_, err = scanCollection(ctx, client, sharedCollection, batchSize, userPayloadFilter(userID), func(point qdrant.Point) error {
		if _, exists := sharedIDs[point.ID]; exists {
			stats.DuplicateSharedPoints++
		}
		sharedIDs[point.ID] = struct{}{}
		if tenant, ok := point.Payload["user_id"].(string); !ok || tenant != fmt.Sprintf("%d", userID) {
			stats.InvalidTenantPoints++
		}
		if schema, ok := point.Payload["collection_schema"].(string); !ok || schema != sharedPayloadSchema {
			stats.InvalidSchemaPoints++
		}
		return nil
	})
	if err != nil {
		return stats, fmt.Errorf("scan shared collection %s: %w", sharedCollection, err)
	}
	stats.SharedUserPoints = len(sharedIDs)
	for pointID := range sourceIDs {
		if _, exists := sharedIDs[pointID]; exists {
			continue
		}
		stats.MissingPointCount++
		appendMismatch(&stats.MissingPointIDs, pointID)
	}
	for pointID := range sharedIDs {
		if _, exists := sourceIDs[pointID]; exists {
			continue
		}
		stats.UnexpectedPointCount++
		appendMismatch(&stats.UnexpectedPointIDs, pointID)
	}
	sort.Strings(stats.MissingPointIDs)
	sort.Strings(stats.UnexpectedPointIDs)
	stats.Verified = stats.MissingPointCount == 0 &&
		stats.UnexpectedPointCount == 0 &&
		stats.DuplicateSourcePoints == 0 &&
		stats.DuplicateSharedPoints == 0 &&
		stats.InvalidTenantPoints == 0 &&
		stats.InvalidSchemaPoints == 0
	return stats, nil
}

func scanCollection(ctx context.Context, client *qdrant.Client, collection string, batchSize int, filter map[string]interface{}, visit func(qdrant.Point) error) (int, error) {
	var offset interface{}
	count := 0
	for {
		points, nextOffset, err := client.ScrollWithFilter(ctx, collection, batchSize, offset, filter)
		if err != nil {
			return count, err
		}
		for _, point := range points {
			count++
			if visit != nil {
				if err := visit(point); err != nil {
					return count, err
				}
			}
		}
		if len(points) == 0 || nextOffset == nil || fmt.Sprint(nextOffset) == "" || fmt.Sprint(nextOffset) == fmt.Sprint(offset) {
			return count, nil
		}
		offset = nextOffset
	}
}

func userPayloadFilter(userID uint64) map[string]interface{} {
	return map[string]interface{}{
		"must": []interface{}{
			map[string]interface{}{
				"key":   "user_id",
				"match": map[string]interface{}{"value": fmt.Sprintf("%d", userID)},
			},
		},
	}
}

func appendMismatch(sample *[]string, pointID string) {
	if len(*sample) < verificationSampleLimit {
		*sample = append(*sample, pointID)
	}
}

func writeVerificationReport(path string, report verificationReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create report directory %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func clonePayload(payload map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{}, len(payload)+4)
	for key, value := range payload {
		clone[key] = value
	}
	return clone
}

func parseUserIDs(raw string) ([]uint64, error) {
	parts := strings.Split(raw, ",")
	seen := make(map[uint64]struct{}, len(parts))
	users := make([]uint64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("%q is not a positive uint64", part)
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		users = append(users, id)
	}
	return users, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
