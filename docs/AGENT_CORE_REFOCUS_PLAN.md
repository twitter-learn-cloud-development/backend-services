# Agent Core Refocus Plan

> Status: Active
> Started: 2026-08-08
> Scope: Agent execution core and its adapters. Social platform, timeline and unrelated control planes are out of scope.
> Current stage: G0/G1/G2/G3 complete. G4 fixed migration matrix has deterministic or controlled evidence through E2E-20; production Goal execution and controlled provider/profile promotion remain off.


## 1. Decision

The project will stop expanding the Agent control plane until the execution core can reliably complete the fixed end-to-end task matrix in `docs/AGENT_E2E_TASK_MATRIX.md`.

The existing Runtime, governed Tool Executor, Workflow Engine, approval/checkpoint, budget and observability code remain the foundation. The migration must not create a second production ReAct loop or let an Environment adapter bypass Tool policy.

## 2. Product Positioning

The target product is a governed Agent Runtime for social content and business automation, not a general local coding agent clone.

The core must support multiple environment packs:

- Twitter: search, read, draft, publish and interaction state.
- Web: search, page read and citation evidence.
- Workflow: immutable published Workflow-as-Tool revisions.
- MCP: user/project connections with current authorization.
- Developer: optional file, shell, Git and test tools for technical demonstrations.

Developer tools are not enabled for ordinary social users by default. Every action continues through the governed Tool Executor.

## 3. Findings And Corrections

| Finding | Current status | Correction |
|---|---|---|
| Keyword capability planning owns routing | Contradicted target | Model proposes a short plan; deterministic policy validates tools, budget and permissions |
| Capability combinations map to fixed execution profiles | Partial | Profiles become configuration/admission data rather than service dispatch branches |
| ReAct execution exists | Implemented | Extend the existing `runtime.ReActRunner`; do not replace it |
| Common environment observation contract | Partial | Twitter Read, Web Read, Workflow-as-Tool and External MCP capability discovery now use current governed catalogs and low-sensitivity snapshots; extend the boundary only to write-state observation |
| Common definition of done | Partial | A final answer requests completion; a Verifier decides completion |
| Evidence checks | Partial | Consolidate tool, approval, artifact and environment evidence in an append-only ledger |
| Multi-Agent collaboration | Partial | Fixed templates remain compatibility presets; dynamic delegation must be plan-driven |
| Evaluation proves real task outcomes | Partial | Gate on state assertions, evidence and recovery, not model text alone |

## 4. Target Runtime

```text
TaskSpec
   |
   v
Context Resolver ---- Rules / history / skills / available connections
   |
   v
Short-Horizon Planner ---- proposes only the next one to three steps
   |
   v
Existing ReActRunner
Observe -> Decide -> Act -> Validate -> Repair
   |                    |
   |                    +-> governed ToolExecutor
   v
Verifier ---- CompletionCriteria + EvidenceLedger + environment snapshots
   |
   +-> passed: Answer + Artifacts + Evidence
   +-> retryable: bounded repair
   +-> blocked: explicit missing dependency/evidence
```

Sandbox, policy, budget, approval, checkpoint and trace surround this flow. They must not replace planning or completion verification.

## 5. Runtime Contracts

The first compatibility layer lives in `internal/module/agent/runtime/goal.go`:

- `TaskSpec`: goal, constraints, required completion criteria, allowed tools and repair limit.
- `Environment`: available governed tools and low-sensitivity state snapshots.
- `EvidenceLedger`: append-only provenance references without arbitrary raw tool output.
- `Verifier`: deterministic/domain verification contract.
- `RequiredEvidenceVerifier`: minimum evidence-presence guard, not a truth judge.

The G1 adapter now also provides `VerifiedCheckpoint`/`VerifiedRunner.Resume`.
Resume restores cumulative usage and evidence, preserves the original before
snapshot, and re-resolves the current tool catalog before any action continues.

The production composition root exposes a global `AGENT_GOAL_RUNTIME_ENABLED` kill switch plus default-off platform-search, Web-research and Grounded-Draft shadow flags. A task shadow runs only when the global and matching task flag are both enabled; it verifies the already executed `RunResult`, emits bounded comparison metrics, and never repeats model, tool, Web provider/page calls or changes the user response. Independently of those shadow flags, the Legacy Web completion guard requires at least one structured public citation before persisting search or Web-draft answers; empty results, provider/page failures, and private or malformed references fail closed without forcing `page_read` for ordinary searches. Grounded Draft shadow additionally binds a digest-only `content.draft` Artifact to exact structured platform/Web references, but remains observation-only until controlled Profile/Provider evidence supports promotion.

## 6. Preserve, Refactor, Freeze

### Preserve

- ReActRunner, BudgetTracker and CostEstimator.
- Tool Registry/Executor, schema validation, policy, approval, idempotency, circuit breaker and audit.
- Workflow IR/Scheduler/Blackboard/Checkpoint/Resume.
- MCP governance, Provider adapters and endpoint policy.
- Run trace, metrics, replay and immutable profile versions.

### Refactor Incrementally

- Capability planning into explicit caller selection plus Catalog admission; the legacy keyword planner was removed in G5.
- Capability route map into tool/capability availability projection.
- `unified_agent.go` profile switch into one Runtime path with compatibility adapters.
- Fixed multi-role templates into optional delegation presets.
- Truncated history/tool results into structured observation summaries with references.
- Content-only artifacts into typed artifacts with provenance and verification status.
- Eval cases into environment assertions and completion evidence.

### Freeze

- Marketplace feature expansion and artifact distribution.
- Publisher signing feature expansion.
- New Profile A/B features and new execution profiles.
- Non-security RBAC expansion.
- Task-template expansion and additional management RPCs.
- Additional Eval authorization, archive and signoff machinery.

Security fixes and blockers in frozen areas are still allowed.

## 7. Migration Stages

### G0: Baseline And Contracts

- Freeze scope and publish the 20-task matrix.
- Add TaskSpec, Environment, Evidence and Verifier contracts with offline tests.
- No production routing change.

Acceptance: contracts compile, deterministic tests pass, and existing Runtime tests remain green.

Rollback: remove the unused contract files and documents; no persisted data or API changes exist.

### G1: Verified Runner Adapter

- Add an opt-in wrapper around the existing ReActRunner.
- Capture evidence from existing structured tool observations.
- Treat model final answers as completion requests.
- Run a bounded verifier/repair loop.

Acceptance: fake tasks cover pass, repair, blocked, approval, resume, revoked tools, cumulative budget and budget exhaustion.
The first domain adapter accepts only paired `platform.tweet_search.v1` observations and verifies ledger entries
against the cumulative Runtime result.
The default-off shadow compares Legacy and Goal completion/evidence using the same `RunResult`; it has no model, tool or persistence side effects.

Rollback: disable `AGENT_GOAL_RUNTIME_ENABLED`; legacy RunAgent behavior remains unchanged.

### G2: Environment Packs

- Implement Twitter, Web, Workflow and MCP environment adapters over existing catalogs and executors.
- Add before/after state verification for write actions.
- Keep Developer environment optional and sandboxed.

Acceptance: adapters cannot execute outside ToolExecutor and cannot expose credentials or raw sensitive state.

Progress: complete. `environment.TwitterReadEnvironment` and `environment.WebReadEnvironment` share an internal read-catalog kernel while injecting separate static policies. `environment.WorkflowToolEnvironment` projects only the current tenant's active publications, and `environment.ExternalMCPEnvironment` projects only currently authorized governed MCP tools. `environment.TweetWriteEnvironment` observes the author's recent Timeline before and after `create_tweet`, stores only sorted tweet references and digests, and rejects ownership, tool-policy or snapshot tampering. `TweetPublishGoalCollector` and `TweetPublishGoalVerifier` require a paired structured `platform.tweet_publish.v1` result and prove either exactly one new target reference or an unchanged idempotent replay. `create_tweet` now returns the structured tweet ID, and persisted ToolExecutor replay restores structured content instead of invoking the write again. No Environment owns an executor or is wired to production Goal execution.

### G3: Unified Planning

- Introduce structured one-to-three-step model plans.
- Validate every proposed tool against Catalog, Policy, Budget, connection ownership and approval.
- Replace keyword capability routing with explicit capability selection and Catalog admission.

Acceptance: ambiguous and failed actions can re-plan without switching to a separate service route.

Progress: complete. `PlannedVerifiedRunner` admits one-to-three-step plans, binds them to the existing Verified/ReAct path and permits one sanitized, budget-nonexpanding recovery for tool failure or missing evidence. `ExplicitCapabilityPlanner` resolves caller-selected capabilities through the immutable Catalog, while requests without an explicit selection fall back only to conversation. The legacy keyword Planner and its compatibility constructor were removed in G5 after repository-wide reachability checks found no production caller. `TaskOutcome` projects plan digests, recovery decisions, digest/reference-only Artifact evidence and verifier checks without model/tool bodies. E2E-02, E2E-11 and E2E-18 have deterministic offline fixtures and outcome/evidence assertions. Production Goal execution remains default-off.

### G4: Task Migration

- Migrate search, grounded research, draft, approved publish and Workflow-as-Tool first.
- Dual-record legacy and Goal Runtime outcomes without duplicating writes.
- Retire profile switch branches only after task parity.

Acceptance: the 20-task matrix passes its required deterministic and controlled integration gates.

Progress: complete for the fixed migration matrix. E2E-05 through E2E-09 and E2E-11 have deterministic default-off migration fixtures; E2E-10/12 have verifier-only offline contracts; E2E-13/14 verify approval and idempotent publish; E2E-15/16 verify MCP tenant authorization and revocation; E2E-17 verifies exact published Workflow execution; E2E-19 verifies monotonic checkpoint revisions and a single persisted write across resumes; E2E-20 verifies policy-allowed Provider fallback and blocked termination without fabricated output. Production Goal execution remains disabled. The next stage is G5 cleanup and a product-reopening audit, not additional control-plane expansion.

### G5: Cleanup And Product Reopening

- Remove dead compatibility branches and duplicated fixed orchestration.
- Rework Eval reports around task outcomes.
- Reopen Marketplace/Profile experiments only after the task scorecard is stable.

Progress: in progress. The first cleanup increment removed the unreachable keyword capability Planner, its named compatibility constructor and keyword-routing tests. Explicit capability selection and Catalog admission remain authoritative. Historical `compat.chat/consult/assist` profiles are retained because existing dialogue resume and product metrics still reference them. Marketplace/Profile experiments remain frozen.

## 8. Risks And Guardrails

- Planner hallucination: plans are proposals; deterministic admission is authoritative.
- Tool escalation: environment tools are the intersection of user connection, catalog, profile, policy and budget.
- Infinite repair: `MaxRepairAttempts` and total run budgets are hard limits.
- False completion: no success without required verification checks.
- Sensitive evidence: ledger stores digests/references; raw data remains in the owning protected store.
- Migration split brain: one write owner only; shadow comparison must never replay side effects.
- Product drift: coding tools remain an optional environment, not the primary social product experience.

## 9. Development Order

Expected focused iterations:

1. G0: 1-2 rounds.
2. G1: 2-3 rounds.
3. G2: 2-3 rounds.
4. G3: 2-3 rounds.
5. G4: 3-4 rounds.
6. G5: 1-2 rounds.

The estimate is 11-17 focused rounds. Completion is determined by the task matrix, not by the number of packages or control-plane features delivered.
