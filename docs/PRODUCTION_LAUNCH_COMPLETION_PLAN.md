# Production Launch Completion Plan

## Local demo preflight, not live acceptance (2026-09-26)

Later on September 26, the local Agent, Gateway and Frontend containers were replaced one at a time with source-built `20260926-2` images after targeted Go and Web build checks. The previous images have `rollback-20260926` tags. Container image IDs matched the candidates, restart counts stayed at zero, Agent metrics was reachable inside its container, Gateway `/health` and Frontend `/` plus `/agent` returned HTTP 200. The Frontend image was assembled offline from the locally verified `web/dist`; it was not built by the normal dependency-installing Dockerfile. After separate authorization, Tweet Service was also rebuilt and switched to a source-built `20260926-2` image with a rollback tag; its container had zero restarts and one passing Consul instance. None of this constitutes authenticated task, model, MCP, approval/resume or publishing evidence; default-off gates remain off and the L3/L5 live checks are open.

At the initial read-only preflight, the local Compose stack and Web/Gateway HTTP endpoints were reachable. Current-source Helm lint/template, Web build, the four L3/L4 offline Go test packages, and the 26-check record self-test passed. The then-running Agent/Gateway/Frontend images dated from August 12 and had no source bind mount; they did not represent the current workspace. Recoverable Runs and External MCP were off in the Agent container, while the Planning-Execute main flag was unset and defaulted off. No model, MCP, approval, publish or clean-host journey has been exercised. Pinned release digests, backup/rollback execution evidence, and explicit authorization for paid/side-effecting checks are still required before recording any L3/L5 live pass.

## L4 Agent workflow transition-write increment (2026-09-26)

Workflow compensation claim/complete/suspend/fail/reject and workflow run cancellation/final commit now acquire the subject write count before Mongo mutation. ID-only compensation transitions resolve the immutable owner first, then include that owner in the update filter; a fence installed between read and acquire rejects the write. Proven no-match/no-write outcomes release the count, while ambiguous database errors retain it. Other Agent owned-data/background writers, positive-count reconciliation and real Mongo race/fault evidence remain open; production deletion remains disabled.

## L4 Agent workflow derived-write increment (2026-09-26)

State-event append, periodic snapshot upsert, compensation-plan upsert, and workflow-run state cursor updates now acquire the subject write count before Mongo persistence. Event and compensation batches reject mixed owners and incomplete identities before acquiring the count. Compensation execution transitions, run cancellation/final commit, and other direct/background Agent writers remain outside this increment. Positive-count reconciliation and real Mongo race/fault evidence are still missing; production deletion stays disabled.

## L4 Agent workflow write serialization increment (2026-09-26)

Workflow definition create/update, workflow run create/update, and legacy workflow revision materialization now acquire the per-subject Mongo write count before persistence. Confirmed success releases the count; errors retain it conservatively. Event, snapshot, compensation, run-control, and other owned-data writers still need the same audit. No live Mongo race/fault evidence or count reconciliation exists, so production deletion remains disabled.

## L4 Agent dialogue write serialization increment (2026-09-26)

The Mongo retention subject now counts in-flight writes. Fence installation requires a zero count, and dialogue creation plus single/batch message writes acquire the count before changing owned data. Confirmed writes release it; ambiguous Mongo failures keep it positive so deletion stops for reconciliation. This works with the current standalone development MongoDB, while true integration/fault-injection evidence is still missing. Every other Agent owned-data writer must use the same protocol before production deletion can be enabled; there is no operator reconciliation/reset action yet.

## L4 write-fence increment (2026-09-26)

An unregistered composite adapter now gathers Agent Mongo and Tweet MySQL fence evidence and returns a two-authority v2 receipt. The Agent service obtains a Mongo-backed subject-write check for its main user write entry points, including project creation, membership addition and compensation retry. Partial fence installation is intentionally left in place for idempotent retry. These checks are currently pre-write reads and do not serialize all Agent writes with fence installation. Direct repository/background writers remain to be audited and guarded. Production deletion remains disabled; no Worker, registry or user API is enabled by this increment.

## L4 data-deletion protocol increment (2026-09-25)

The versioned staged protocol, immutable capability snapshot, preflight snapshot, Mongo checkpoint/audit CAS and Claim/Complete coordinator are implemented, but this is not an executable production deletion feature. Capability drift is rejected and bounded hash-chained stage audit is appended atomically with each checkpoint transition. Storage-independent adapters cover Mongo owned data, safe shared-relationship detach, MinIO, Qdrant and the protected Tweet Service deletion RPC. An unregistered recovery worker now owns the eight-stage sequence: it reissues short-lived Legal Hold evidence before each destructive call, verifies each immutable receipt against an authoritative post-state, re-observes all five stores for zero residuals, and atomically commits the final checkpoint, audit event and completed request state. Structured write-fence and finalization receipts bind the subject digest, plan, capabilities and prior evidence. An unregistered Agent-side Mongo Fence Authority now persists the fence identity under the retention subject, installs it with Legal Hold revision CAS, supports same-request re-entry with fresh short-lived governance, and exposes a fail-closed write authorization check. The production registry remains intentionally empty because Agent write entry points do not all consume that authority and Tweet Service does not yet own and honor a local fence. The worker is not wired to background scheduling, CLI or the composition root. Release readiness remains blocked until cross-service write-path coverage, explicit enablement, user API/ownership transfer, real Mongo multi-replica and large-subject recovery evidence, external-store fault recovery and staging live verification exist. Until then `execution_supported` must remain false and the CLI must not expose physical deletion.

> Status: Active
> Scope: public-demo and small-scale beta launch of the governed Agent Runtime and social-content workflows.
> Principle: launch claims require code, configuration, tests and a deployment acceptance record. A disabled flag, an offline fixture or a design document is not a launched capability.

## 1. Launch Definition

The first production release is complete only when a real user can:

1. hold a multi-turn conversation whose older durable context survives token pressure;
2. ask the Agent to choose and execute a bounded read-only plan instead of selecting a hard-coded product pipeline;
3. search first-party tweets and the public web with structured citations;
4. discover and invoke an authorized external MCP tool, with live revocation and approval for writes;
5. invoke an immutable published Workflow-as-Tool and observe the child run result;
6. suspend for approval or human input and resume without repeating a successful side effect;
7. inspect a run, tool calls, token/cost usage and a sanitized failure reason;
8. receive an explicit blocked result when evidence, credentials, dependencies or budget are missing.

The first release does not require Marketplace expansion, publisher self-service, new A/B control planes, parallel multi-agent execution, arbitrary developer shell/file tools or more fixed execution profiles. Those areas remain frozen unless they block security or correctness.

## 2. Evidence Ledger

| Capability | Status | Current evidence | Launch gap | Priority |
|---|---|---|---|---|
| ReAct execution and governed ToolExecutor | Implemented | `runtime/runner.go`, `workflow/tool/`, approval, idempotency and trace tests | staging workload and rollback acceptance | P0 |
| Model-driven short planning | Partial | default-off stable-user canary composes `ModelShortHorizonPlanner -> PlanningCoordinator -> PlannedVerifiedRunner` for platform search, public Web research, authorized external MCP read/approved-write and immutable published Workflow reads; authoritative runs retain low-sensitivity plan, usage, verifier and encrypted resume evidence | deployment acceptance remains | P0 |
| Completion verification | Partial | `runtime/verified_runner.go`, domain collectors/verifiers | production execution still relies on profile-specific post checks for most routes | P0 |
| External MCP | Partial | remote manager, tenant/project authorization, pool, health, approval/revocation tests and independent default-off read plus approved-write Planning-Execute canaries | flags default off; no recorded live read/write acceptance or connector-specific post-state readback against a real server | P0 |
| Workflow-as-Tool | Partial | immutable publication binding, child-run evidence, independent default-off read Planning-Execute canary and revocation-before-resume tests | flags default off; deployment acceptance and parent/child process-restart drill missing | P0 |
| Semantic context compaction | Implemented, deployment acceptance pending | model-distilled session summary, Qdrant memory, token-aware history, Mongo due schedule, leased recovery worker, bounded metrics and five-category long-context gate | capture real restart and provider-quality evidence in staging | P0 |
| Recoverable Agent runs | Implemented, disabled | encrypted checkpoint/resume and approval binding tests | production keys, flags and multi-instance acceptance missing | P0 |
| Web search and page evidence | Implemented, provider-dependent | governed provider config, endpoint policy, evidence collectors | provider freshness/latency acceptance and failure fallback drill | P1 |
| Multi-agent collaboration | Partial | bounded sequential research/draft roles and eval | parallel roles and role-level recovery are not release requirements | P2 frozen |
| Marketplace/Profile experiments | Partial | control-plane foundations | explicitly outside first launch | P3 frozen |

## 3. Target Production Path

```text
User Goal
  -> capability/catalog admission
  -> short-horizon model plan (1-3 actions)
  -> deterministic plan policy
  -> existing ReActRunner
  -> governed ToolExecutor
  -> domain evidence collector and verifier
  -> verified answer/artifact OR blocked/suspended result

Surrounding controls:
  identity + endpoint policy + budget + approval + checkpoint + trace + rollback flag
```

There must be one execution loop. MCP, Workflow-as-Tool and first-party Twitter/Web operations are tools in the same governed path; they must not create separate autonomous runtimes.

## 4. Delivery Stages

### L0: Truthful Baseline And Scope Freeze

Status: Code complete; deployment acceptance is part of L5.

- Keep Marketplace, publisher expansion, additional execution profiles and non-security RBAC work frozen.
- Record every capability as Implemented, Partial, Missing or Disabled.
- Keep the current legacy/runtime route as rollback until the new path passes canary gates.

Acceptance:

- this plan is the canonical launch checklist;
- documentation no longer equates offline fixtures or disabled flags with production completion;
- the repository builds with a clean deployment configuration example.

Rollback: documentation-only; no runtime behavior changes.

### L1: Semantic Context Completion

Status: Code complete; deployment acceptance is part of L5.

- [x] Use model-distilled session summaries and bounded memory retrieval in chat, search, MCP, Workflow and Skill Runtime paths.
- [x] Make summary model, message threshold, idle delay and durable sweep interval deployment configuration.
- [x] Add low-cardinality metrics for summary attempts, failures, latency and retrieved chunk count.
- [x] Replace process-local-only scheduling with Mongo due markers, schedule revisions, bounded recovery sweeps and the existing claim lease.
- [x] Add a fixed long-conversation context-retention set covering facts, preferences, decisions, corrections and prompt-injection residue.

Acceptance:

- recent tool-call/result pairs remain intact;
- older facts required by the test set remain answerable after the raw history budget is exceeded;
- summary failures degrade to recent history without failing the user request;
- no prompt, credential or raw tool payload appears in summary logs.

Rollback: disable semantic retrieval while retaining token-aware recent-history truncation.

### L2: Planning-Execute Production Migration

Status: In progress.

Six increments completed: the service layer has default-off, stable-user canaries for first-party platform search, public Web research, authorized external MCP read/approved-write and immutable published Workflow reads, reusing the governed ReAct runner and ToolExecutor. It persists low-sensitivity plan/usage/verification evidence, rejects unverifiable completion, encrypts the complete `goal.v1` Verified Checkpoint in the existing checkpoint store, and exposes only a sanitized inspection projection plus low-cardinality metrics. Planned Web research requires a structured search result and page-read observation bound to one public URL. MCP read uses the tenant's executable read catalog; the independent write flag uses the governed catalog only when Unified Approval Recovery is enabled. The planner cannot grant permission: risky/write steps suspend, persist the complete verified checkpoint, revalidate the current catalog and approval binding, then execute with a platform-derived idempotency key. Planned Workflow execution revalidates Active Publication and verifies immutable binding, parent/child lineage and authoritative child output. First-party tweet publication remains an explicit `ConfirmPublishTwitter` action outside autonomous planning.

- [x] Add a dedicated default-off production flag and stable canary percentage.
- [x] Compose `ModelShortHorizonPlanner -> PlanningCoordinator -> PlannedVerifiedRunner` in the service layer without duplicating ReAct.
- [x] Persist admitted plan digest, planning usage and verified checkpoint identity in the authoritative Agent execution run.
- [x] Expose planning/verifier evidence in the run inspection surface and add low-cardinality rollout metrics.
- [x] Persist and resume the full verified checkpoint before allowing planned execution to suspend.
- [x] Migrate first-party platform search.
- [x] Migrate public Web research with search-to-page evidence and independent rollback flag.
- [x] Migrate external MCP read with an independent rollback flag and current-authorization resume checks.
- [x] Migrate Workflow-as-Tool read with an independent rollback flag, immutable binding evidence and publication revocation checks.
- [x] Migrate governed External MCP approved writes without weakening approval, idempotency, checkpoint integrity or current-authorization verification.
- A planner may request tools but cannot grant permission, change tenant identity, expand budget or weaken approval.
- Keep explicit capability selection as admission input; remove profile-specific dispatch only after equivalent evidence and recovery tests pass.

Acceptance:

- at least 20 fixed tasks cover success, malformed plan, unknown tool, missing evidence, tool failure, budget exhaustion, approval and resume;
- production canary records plan/admission/execution/verifier outcomes without raw chain-of-thought;
- a failed canary can revert to the current route through one configuration change;
- the write route remains independently disabled until read-only canary error and blocked-result rates meet the gate.

Rollback: set planning canary to zero and continue the current Runtime route; persisted plan fields remain backward-compatible optional data.

### L3: MCP And Workflow Launch Gate

Status: Implementation complete; live evidence pending.

- [x] Enable only allowlisted HTTPS MCP hosts; private/local endpoints stay denied outside explicit development profiles.
- [x] Store only credential references; inject secrets from deployment-managed files or secret storage.
- [x] Turn on pool and health checks with bounded concurrency and low-cardinality metrics.
- [ ] Run MCP conformance plus one real read tool and one approval-gated idempotent write tool.
- [ ] Prove project membership revocation blocks discovery and execution before the remote call.
- [ ] Publish one immutable demonstration Workflow-as-Tool and verify parent/child run evidence, timeout and failure propagation.

Execution procedure and evidence boundary: `docs/agent_launch_runbook.md`.

Acceptance:

- signed acceptance report contains server identity digest, tool/schema digest, latency/error summary and no secret/body dump;
- write approval resume executes the remote side effect at most once for the same idempotency key;
- connector outage returns a bounded blocked/failure state, not a fabricated answer;
- disabling `AGENT_EXTERNAL_MCP_ENABLED` or `AGENT_WORKFLOW_AS_TOOL_ENABLED` is an immediate rollback.

### L4: Deployment Hardening

Status: In progress.

- [x] Create a production Compose/host overlay with pinned images, resource limits, health checks and restart policies.
- [x] Move production secrets out of tracked `.env` files; validate required key length and cross-flag dependencies before startup.
- [x] Configure an internal TLS edge proxy, exact HTTP/WebSocket origins, JWT issuer/audience validation, Grafana cookie flags and public base URLs. Main-app auth remains Bearer JWT; live domain/certificate/browser acceptance is still required.
- [ ] Add database/index migration runbooks, backups and restore drills for MySQL, Mongo, Redis persistence, Qdrant, ES and MinIO. The runbook and SHA-256 manifest gate exist; no real restore drill has been claimed.
- [x] Configure Prometheus/Grafana/trace collection and Alertmanager rules for API errors, LLM/provider errors, tool failures, MCP health, queue backlog, token cost and summary failures. Live threshold tuning and notification delivery remain staging evidence.
- [x] Define retention rules and implement a bounded, leased, default-off/dry-run application worker for dialogue, orphan-message, trace, checkpoint and tool-result payloads. User-level guards make Legal Hold and deletion mutually exclusive; the operator CLI is preview-only by default. WORM eval objects remain excluded.
- [ ] Complete retention launch evidence: the durable user-deletion request and read-only preflight now record Hold/busy blockers, classified Mongo ownership/relationship/governance inventories and external unsupported scope; a default plan-only operator CLI manages request control state. Tweet Service has an authenticated transactional delete boundary, but no user API, registered capability or physical worker exists. Complete governed deletion across Mongo/Qdrant/MinIO/shared relationships, staging Hold/deletion races and deletion/object-failure recovery. The orphan audit has no delete mode by design.

Acceptance:

- all containers become healthy from a clean host using documented commands;
- no service depends on `localhost` for another container or on an optional desktop application;
- a backup restore and one service restart preserve user-visible state and resumable runs;
- secrets do not appear in image layers, repository, logs or API responses.

Rollback: retain the previous pinned image set, database-compatible schema and environment snapshot; deploy is reversible without data deletion.

### L5: Staging, Canary And Public Release

Status: Planned.

The L3/L4/L5 catalog now has 31 required checks (M8-R3, 2026-10-15). The v2 HMAC record binds the independently reviewed published release manifest/source, deployment ID and environment snapshot; every pass requires a matching recent live observation and hashed raw evidence. Legacy v1 records remain verifiable against their original plan but cannot pass `RequireReleaseReady`. This is an evidence gate only; it does not execute or imply live acceptance. See `DEPLOYMENT_ACCEPTANCE_RUNBOOK.md`. The current round is limited to tooling and offline verification by user request.

- Run deterministic offline tests first, then staging integration with explicitly authorized provider/MCP calls.
- Execute the fixed product journey: chat -> search -> cited draft -> approval -> publish -> inspect run -> resume.
- Run failure drills for provider timeout, MCP revocation, Redis/Mongo restart, budget exhaustion and malformed model output.
- Start with one internal account, then a small allowlist, then public beta.

Acceptance:

- zero P0/P1 open launch issues;
- all launch journeys have captured evidence and an owner;
- dashboards and rollback commands are verified before traffic expansion;
- public documentation names beta limitations honestly.

## 5. Required Feature Flags For Staging

The exact production values will be written only after L2-L4 acceptance. The dependency order is:

1. `AGENT_RUNTIME_V2_MODES` for the governed Runtime routes.
2. `AGENT_RECOVERABLE_RUNS_ENABLED=true` before any Planning-Execute rollout.
3. `AGENT_PLANNING_EXECUTE_ENABLED=true`, a non-zero `AGENT_PLANNING_EXECUTE_CANARY_PERCENT`, and one or more route flags (`AGENT_PLANNING_EXECUTE_PLATFORM_SEARCH_ENABLED`, `AGENT_PLANNING_EXECUTE_WEB_RESEARCH_ENABLED`, `AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_READ_ENABLED`, `AGENT_PLANNING_EXECUTE_EXTERNAL_MCP_WRITE_ENABLED`, `AGENT_PLANNING_EXECUTE_WORKFLOW_READ_ENABLED`) enable the selected canaries.
4. `AGENT_EXTERNAL_MCP_ENABLED=true` before either external MCP canary, project scope, managed credentials or unified approval recovery. The write canary additionally requires `AGENT_UNIFIED_APPROVAL_RECOVERY_ENABLED=true`.
5. `AGENT_WORKFLOW_AS_TOOL_ENABLED=true` before Skill catalog.
6. Goal shadow flags remain observation-only; they do not enable production planning.

No deployment should enable every experimental flag at once.

## 6. Verification Matrix

Each launch stage must include:

- focused unit tests and service integration tests;
- `go test -race` for shared state, pools, timers, leases and checkpoint changes;
- `go vet` for affected packages;
- Web build and browser smoke tests for user-visible Agent changes;
- Compose config validation and clean-host startup;
- explicit live acceptance only with user-provided credentials and cost authorization;
- signed production acceptance verification with `-RequireReleaseReady` before traffic expansion;
- `git diff --check` and a reviewed rollback path.

## 7. External Inputs Required Before Actual Deployment

Code completion can continue without these values. The final online deployment cannot:

- target server or cloud, operating system and CPU/RAM/storage limits;
- domain name and TLS termination choice;
- production model/provider and spending ceiling;
- public web-search provider;
- one real MCP server for acceptance;
- production secret delivery method;
- acceptable beta data-retention period.

These inputs are requested only when L4 begins; they are not blockers for L1-L3 implementation.

## 8. Expected Remaining Batches

After the governed Planning-Execute migration and static production hardening, the expected remaining work is 2 focused deployment batches:

1. target-environment backup/restore, clean-host restart/rollback, TLS/browser/alert delivery, and real MCP/Workflow acceptance;
2. L5 fixed-journey staging, failure drills, canary evidence and public-beta release verification.

The estimate excludes frozen Marketplace, parallel multi-agent and new management control-plane work.

## 9. Local Acceptance Checkpoint (2026-09-26)

An isolated authenticated user completed paid-model chat, verified Planning-Execute platform search, completed-run persistence across Agent restart, and explicit draft confirmation followed by Tweet detail and author-timeline readback. The DashScope-compatible adapter was corrected for tool-free `tool_choice` and unsupported `developer` roles. See `docs/PROJECT_PROGRESS.md` and Issue 224 for run IDs and checks.

This is not a release decision. The temporary 49% local canary and ephemeral checkpoint key were removed after testing; production flags remain off. A durable checkpoint secret, a real HTTPS MCP endpoint and credential, pending approval/resume evidence, Workflow-as-Tool parent/child evidence, browser verification, target-host TLS/backup/rollback drills, and the signed 26-check release gate remain required. The local HTTP conformance endpoint was correctly rejected for bearer authentication and was not counted as third-party MCP evidence.
