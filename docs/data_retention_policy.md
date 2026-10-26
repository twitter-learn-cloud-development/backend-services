# Production Data Retention Policy

## User-deletion execution stop line (2026-09-25)

- Read-only inventory may create a versioned deletion-protocol snapshot, but the product does not yet provide physical user deletion.
- The protocol requires a subject write fence before relationship removal and keeps MinIO references until object deletion has a verifiable receipt.
- A missing capability, Legal Hold interlock or post-state verifier for Mongo, MinIO, Qdrant or Tweet Service keeps the plan non-executable.
- Model text, a FinalAnswer, worker logs or one storage success response cannot prove deletion; final completion requires cross-store zero-residual evidence and stage receipt digests.

This policy covers the public beta. Legal hold and user-deletion requests take
precedence over normal expiration.

| Data | Default retention | Enforcement/status |
|---|---:|---|
| Dialogue messages | 180 days after last activity | bounded application worker implemented; residual orphan messages are reconciled after a 24-hour safety grace |
| Agent execution trace | 30 days | bounded application worker and retention scan indexes implemented; disabled by default |
| Completed encrypted checkpoints | 7 days | worker clears terminal payloads after 7 days and expires suspended/approval payloads after 30 days; disabled by default |
| Inline/MinIO tool results | 30 days | worker deletes referenced objects before clearing Mongo payload/reference; bounded read-only audit inventories already-orphaned candidates, while guarded deletion remains required |
| Agent eval reports | 180 days | private versioned bucket; WORM compliance retention may extend deletion |
| Prometheus metrics | 15 days | production Compose `PROMETHEUS_RETENTION` |
| Loki logs | 7 days | Loki compactor retention in `deploy/loki/loki-config.yaml` |
| Jaeger traces | 7 days | Badger TTL via `JAEGER_RETENTION` |
| Alertmanager state | 5 days | `ALERTMANAGER_RETENTION` |
| Docker local logs | 5 files x 10-20 MiB per container | production Compose logging limits |
| Public media | account/tweet lifecycle | deletion workflow required; backups follow the authoritative object lifecycle |

Logs, metrics, traces and backup manifests must not contain prompts, message
bodies, credentials or raw Tool results. User/run/tweet identifiers must not be
Prometheus or Loki labels. Backups are kept for 30 days off-host unless a
security incident or legal hold requires a different period.

The observability limits above are implemented by deployment configuration.
The Agent application worker, global lease, candidate claims, user-level Legal
Hold exclusion, orphan-message reconciliation, metrics and deployment flags
are implemented, but active enforcement is not: its default is disabled and
dry-run. A paginated read-only MinIO orphan audit is available, but
user deletion currently stops at a durable, read-only preflight request. It
records Legal Hold or subject-busy blockers and separates proven user-owned
resources, shared relationships and governance-retained evidence. Qdrant,
MinIO and Tweet Service data remain explicitly outside the counted Mongo
scope. A default plan-only operator CLI manages request control state, but no
user API or physical deletion exists. Deletion execution,
Legal-Hold-compatible deletion for already orphaned keys and
staging deletion/recovery evidence remain production prerequisites. Rollout and rollback are documented in
`docs/data_retention_worker_runbook.md`.
