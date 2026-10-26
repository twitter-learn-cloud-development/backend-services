# Production Backup And Restore Runbook

This runbook is for the single-host public beta. It defines the backup set and
restore order; it does not claim that a restore drill has already passed.

## Recovery Objectives

- Authoritative user and Agent state: target RPO 24 hours, target RTO 4 hours.
- Redis projections and search/vector indexes: rebuildable, target RTO 8 hours.
- Observability data: best effort; it must not block product-state recovery.

## Required Backup Set

One snapshot directory is complete only when it contains:

| Artifact | Source | Method |
|---|---|---|
| `mysql.sql.gz` | MySQL | consistent logical dump with routines, events and GTID metadata |
| `mongo.archive.gz` | MongoDB | authenticated `mongodump --archive --gzip` |
| `redis.rdb` | Redis | authenticated RDB snapshot after `BGSAVE` completion |
| `qdrant.snapshot` | Qdrant | full-storage snapshot API from the pinned Qdrant version |
| `elasticsearch.snapshot-manifest.json` | Elasticsearch | repository snapshot name, UUID, index list and state |
| `minio.inventory.json` | MinIO | version-aware object inventory after bucket replication/export |
| `deployment-metadata.json` | deployment | image digests, schema versions, feature flags and UTC cut time; no secret values |

Generate and verify the integrity manifest:

```powershell
./scripts/production-backup-manifest.ps1 -Mode Create -BackupDirectory D:\twitter-backups\2026-09-24 -SnapshotID 2026-09-24T120000Z
./scripts/production-backup-manifest.ps1 -Mode Verify -BackupDirectory D:\twitter-backups\2026-09-24
```

The backup directory and encryption key must be outside the repository. Encrypt
the complete directory with the operator's backup KMS/key before off-host copy.
Never put database passwords or provider credentials in
`deployment-metadata.json`.

## Restore Order

1. Provision a clean isolated host with the same pinned image set. Do not attach
   public DNS or enable Agent write routes.
2. Verify `backup-manifest.json`; abort on any missing, empty, size-mismatched
   or hash-mismatched artifact.
3. Restore MySQL, then MongoDB and MinIO. Run schema compatibility checks before
   starting application services.
4. Restore Redis only when the RDB version matches. Otherwise start empty and
   rebuild projections from authoritative stores/events.
5. Restore Qdrant and Elasticsearch snapshots. If either restore fails, keep
   semantic/search routes disabled and rebuild asynchronously.
6. Start internal services, then Gateway and Frontend behind loopback-only
   bindings. Run health, login, dialogue, workflow resume and tweet-read checks.
7. Enable the TLS edge, then restore read-only Agent canaries. Write routes are
   enabled last after idempotency and approval checks.

## Mandatory Drill Evidence

Record the manifest hash, source and target image digests, start/end time,
commands, failed steps, state checks and operator. The drill passes only when:

- a known user, tweet and dialogue are readable;
- one suspended Agent run resumes without repeating a successful side effect;
- object references resolve and search/vector projections are either restored
  or explicitly rebuilding;
- the previous image/environment snapshot can be redeployed without deleting
  restored volumes.

Until this drill is executed on a clean staging host, the L4 backup/restore
acceptance item remains pending.
