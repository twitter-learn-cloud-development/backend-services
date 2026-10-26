# Agent Data Retention Worker Runbook

## User-deletion protocol status (2026-09-25)

A successful read-only preflight now stores an `agent-data-deletion-protocol/v1` snapshot. The fixed order is write fence, shared-relationship detach, MinIO tool-result deletion, Qdrant memory deletion, Tweet Service deletion request, Agent Mongo owned-data deletion, cross-store zero-residual verification and finalization.

This snapshot is currently an audit and implementation contract, not an executable job. The immutable capability registry exists, and an unregistered Mongo owned-data adapter now provides a fixed exclusive-data manifest, transactional governance recheck, persistent idempotency receipt and per-collection zero-residual readback. Production still registers no deletion providers, so `plan.ready=false` and `execution_supported=false`. The adapter has no Worker or CLI entry and has not been exercised against a real Mongo replica set. The CLI has no physical-delete action. Operators must not edit Mongo documents, capability/plan digests, audit events or checkpoints to bypass this gate.

The domain protocol defines revision/CAS semantics, a maximum 15-minute stage lease, expired-lease takeover, strict dependencies, success receipt digests and bounded failure codes. Mongo checkpoint CAS and a control-plane-only Claim/Complete coordinator are implemented with request/checkpoint revisions, plan/capability binding and the existing Retention Subject Lease. Claim/success/failure audit events form a bounded hash chain and are appended atomically with checkpoint transitions; once execution starts, preflight cannot replace the protocol or erase its audit. No worker, storage adapter or cross-store post-state checker is registered, so this control path remains unreachable from the CLI and cannot delete data.

The destructive Adapter contract is `agent-data-deletion-adapter/v1`. A request binds a stable operation key to the request, stage, subject digest, scope digest, plan and capability snapshot. Before mutation, an implementation must reject a held, expired, tampered or wrong-subject governance decision. A successful result must replay the same immutable receipt for the same operation key and expose an independently readable post-state snapshot with zero residual resources. These validators and the in-memory conformance fixture are not a production adapter, signature service or worker.

## Purpose

The Agent data retention worker removes expired application data in bounded,
observable batches. It is disabled and dry-run by default. Destructive
execution must remain disabled until dry-run results, backup recovery, Legal
Hold operations and user-deletion procedures have been reviewed.

## Covered resources

| Resource | Default retention | Execute-mode action |
| --- | ---: | --- |
| Dialogues and messages | 180 days | Delete the dialogue and related messages |
| Orphan dialogue messages | 24-hour safety grace | Delete messages whose parent dialogue no longer exists |
| Run, step, LLM and tool traces | 30 days | Delete expired trace documents |
| Terminal workflow and Agent checkpoints | 7 days | Clear checkpoint and resume payloads |
| Suspended workflow and Agent checkpoints | 30 days | Clear checkpoint data and fail the expired run |
| Inline and archived tool-result bodies | 30 days | Delete the archived object first, then purge body/reference while retaining digest, length and idempotency evidence |

Agent evaluation reports and WORM evidence are excluded. Object Lock may
extend their storage lifetime beyond the application retention windows.

## Safety model

- A Mongo-backed lease limits a sweep to one active instance.
- Each candidate is claimed with an expiring token before mutation.
- Every execute-mode candidate must have a user owner. The worker acquires a
  short user-level lease before claiming it; an active Legal Hold prevents the
  lease, while applying a Hold fails with `subject is busy` until an in-flight
  deletion finishes. A successful Hold therefore excludes later deletion.
- Scans and mutations are bounded by batch size and operation timeout.
- Dry-run discovers candidates without claiming or mutating them.
- Archived tool results are deleted from object storage before their Mongo
  reference is cleared. A failed object deletion preserves the reference and
  releases the claim for retry.
- Metrics use bounded resource and outcome labels; no user or run identifier is
  used as a Prometheus label.

## Configuration

| Variable | Default |
| --- | --- |
| `AGENT_DATA_RETENTION_ENABLED` | `false` |
| `AGENT_DATA_RETENTION_DRY_RUN` | `true` |
| `AGENT_DATA_RETENTION_INTERVAL` | `6h` |
| `AGENT_DATA_RETENTION_LEASE_DURATION` | `15m` |
| `AGENT_DATA_RETENTION_OPERATION_TIMEOUT` | `2m` |
| `AGENT_DATA_RETENTION_BATCH_SIZE` | `100` |
| `AGENT_DIALOGUE_RETENTION` | `4320h` |
| `AGENT_ORPHAN_MESSAGE_GRACE_PERIOD` | `24h` |
| `AGENT_TRACE_RETENTION` | `720h` |
| `AGENT_TERMINAL_CHECKPOINT_RETENTION` | `168h` |
| `AGENT_SUSPENDED_CHECKPOINT_RETENTION` | `720h` |
| `AGENT_TOOL_RESULT_RETENTION` | `720h` |

The suspended checkpoint window must not be shorter than the terminal window.
Only `worker` and `all` Agent process roles start the worker.

## Legal Hold operations

Mutation commands are preview-only unless `--execute` is present. Operator
identity is normalized and persisted only as a SHA-256 digest. Reason codes
must be lowercase bounded identifiers such as `legal_case_2026_01`.

```powershell
go run ./cmd/agent-retention-hold --action apply --user-id 42 --reason-code legal_case_2026_01 --operator security@example.com
go run ./cmd/agent-retention-hold --action apply --user-id 42 --reason-code legal_case_2026_01 --operator security@example.com --execute
go run ./cmd/agent-retention-hold --action list
go run ./cmd/agent-retention-hold --action release --user-id 42 --operator security@example.com --execute
```

Use `--expires-in 168h` for an expiring Hold; omit it for an indefinite Hold.
If apply reports `data retention subject is busy`, retry after the bounded
retention operation timeout. Do not treat a failed apply as an active Hold.

## User deletion request preflight

The internal `DataDeletionRequestService` persists one active request per
user and uses CAS leases for repeatable preflight. It shares the existing
user-level retention subject lease, so an active Legal Hold becomes
`blocked_legal_hold` and an overlapping retention operation becomes
`blocked_subject_busy`.

Preflight is read-only with respect to application data and always records
`execution_supported=false`. Its Mongo inventory is split into:

- `inventory`: directly user-owned deletion candidates, including dialogue,
  Workflow state, Provider, MCP, template, publication, run, trace and Tool
  governance records;
- `related_inventory`: shared project/MCP, role and marketplace ownership
  relationships that cannot be deleted as ordinary personal assets;
- `retained_inventory`: product, attribution, deletion, Profile and
  marketplace governance evidence that requires a separate retention policy.

Qdrant episodic memory, MinIO Tool Result objects and Tweet Service published
content remain explicit unsupported external resources. Use the bounded
operator command instead of modifying the request collection manually:

```powershell
# Default is plan-only for every mutation.
go run ./cmd/agent-data-deletion-request --action request --user-id 42 --reason-code user_request --actor privacy@example.com
go run ./cmd/agent-data-deletion-request --action request --user-id 42 --reason-code user_request --actor privacy@example.com --execute
go run ./cmd/agent-data-deletion-request --action get --request-id '<object_id>'
go run ./cmd/agent-data-deletion-request --action preflight --request-id '<object_id>' --execute
go run ./cmd/agent-data-deletion-request --action cancel --request-id '<object_id>' --execute
```

`--execute` only mutates deletion-request control state. The command has no
physical delete action and is not a user-facing authorization boundary.

## MinIO orphan audit

`agent-tool-result-orphan-audit` is read-only. It lists at most the requested
batch, ignores objects newer than the grace period, and checks each older key
against Mongo `output_reference.key`. The report contains key digests and
object metadata, never Tool Result bodies. A lookup error is reported as a
failure and cannot become an orphan candidate.

```powershell
go run ./cmd/agent-tool-result-orphan-audit --limit 100 --grace-period 24h
go run ./cmd/agent-tool-result-orphan-audit --limit 100 --grace-period 24h --cursor '<next_cursor>'
```

The cursor is an opaque hashed object key. Retain each JSON report with the
staging acceptance evidence. This command intentionally has no delete flag:
existing object keys do not carry a reversible user ID, so deletion cannot yet
share the user-level Legal Hold lock without a compatible migration.

## Staged rollout

1. Deploy with the worker enabled and dry-run retained.
2. Observe at least two complete cycles. Review candidate counts by resource,
   cycle failures and duration; sample the source records and verify cutoffs.
3. Confirm backups and restore instructions. Apply an expiring staging Hold,
   prove its candidates report `legal_hold`, then release it. Assign an owner
   for user-requested deletion. Stop when any owner or procedure is undefined.
4. Run the bounded MinIO orphan audit through an empty cursor page, review all
   lookup failures and candidate counts, then approve a lifecycle or compatible
   guarded deletion design. The audit itself does not delete objects.
5. Set dry-run to false in staging. Verify Mongo mutation and object deletion
   together, including one simulated object-store failure.
6. Promote the reviewed retention windows to production. Start with the
   default batch size and raise it only after observing storage load.

## Metrics

- `agent_data_retention_items_total{resource,outcome}`
- `agent_data_retention_cycles_total{outcome}`
- `agent_data_retention_cycle_duration_seconds`

## Rollback

Set `AGENT_DATA_RETENTION_ENABLED=false` and redeploy the Agent worker. This
stops future sweeps but cannot restore deleted data. Restore only through the
approved backup procedure; do not recreate partial records manually.

## Known boundaries

- User-requested deletion has a durable request/preflight state machine and a
  default plan-only operator CLI, but physical deletion and a user API are not
  implemented. Preflight shares the
  scheduled sweep's subject lease; permanent deletion priority remains a
  required execution-stage invariant.
- Dialogue deletion spans the dialogue and message collections. The parent is
  deleted first so new writes fail. A bounded orphan-message reconciliation
  pass removes residual messages only after the configured safety grace.
- The worker deletes archived objects still referenced by Mongo. The separate
  audit discovers already-orphaned candidates but does not delete them because
  legacy keys cannot yet acquire the same user-level Legal Hold guard safely.
