# Agent End-to-End Task Matrix

> Version: `agent-e2e-goal-v1`
> Status: Baseline
> Rule: A fluent final answer does not pass a task without the required evidence and observable completion criteria.

## Result Vocabulary

- `passed`: every required criterion is verified.
- `repair`: verification failed but the bounded repair budget remains.
- `blocked`: a dependency, permission, approval or trustworthy evidence is unavailable.
- `failed`: an unrecoverable execution or policy error occurred.

## Fixed Tasks

| ID | Scenario | Required completion criteria | Required evidence | Test level |
|---|---|---|---|---|
| E2E-01 | Continuous conversation follow-up | Uses the selected dialogue history; does not invent a new session | Dialogue ID and message persistence reference | Offline + integration |
| E2E-02 | Ambiguous request | Asks one useful clarification before a consequential action | `ask_human` step and suspended run | Offline |
| E2E-03 | Model capability selection | Only chat-capable models are offered and invoked | Catalog model capability snapshot | Offline + UI |
| E2E-04 | Budget exhaustion | Stops before exceeding the limit and reports the boundary honestly | Budget snapshot and fixed error code | Offline |
| E2E-05 | Platform tweet search | Returns existing tweet identities from structured search results | Tool observation with tweet IDs/references | Offline + integration |
| E2E-06 | Search-result follow-up | Reads the selected prior result instead of fabricating full content | Prior observation reference and detail-tool result | Offline + integration |
| E2E-07 | Public web research | Returns claims with resolvable citations from the configured provider | Search/page observations and citation references | Controlled integration |
| E2E-08 | Missing web evidence | Refuses unsupported details and does not claim a pending background result | Empty/error observation and blocked result | Offline |
| E2E-09 | Grounded content draft | Draft claims are supported by supplied platform/web evidence | Claim-to-evidence references and draft artifact | Offline + controlled integration |
| E2E-10 | Rewrite constraints | Meets requested language, format and configured length limits | Deterministic output checks and artifact digest | Offline |
| E2E-11 | Research then draft | Dynamically uses research before drafting when evidence is absent | Plan steps, observations and verified artifact | Offline + controlled integration |
| E2E-12 | Evidence conflict | Reports conflicting sources or asks for resolution instead of choosing silently | Conflicting evidence references and verifier result | Offline |
| E2E-13 | Tweet publish approval | Never publishes before explicit approval; success requires the target tweet in authoritative post-write state | Approval evidence, structured tweet ID and before/after snapshot | Offline + integration |
| E2E-14 | Idempotent tweet publish | A retry restores the persisted structured result and creates no duplicate tweet | Idempotency key, unchanged before/after state and target tweet reference | Offline + integration |
| E2E-15 | User MCP read | Uses only an active connection owned by the current user/project | Connection revision and tool observation | Offline + controlled integration |
| E2E-16 | MCP write and revocation | Requires approval; revoked access fails closed before the call | Approval, authorization snapshot and audit result | Offline + controlled integration |
| E2E-17 | Workflow as Tool | Executes the exact published revision and verifies declared output | Workflow revision/hash, child run and output evidence | Offline + integration |
| E2E-18 | Workflow/tool failure repair | Re-plans a retryable failure once, then completes or reports blocked | Failed observation, repair decision and final verification | Offline |
| E2E-19 | Checkpoint and resume | Resumes the same run after human input without replaying completed writes | Checkpoint revision, resume evidence and idempotency record | Offline + integration |
| E2E-20 | Provider outage | Tries only policy-allowed fallback; otherwise terminates as blocked without fabricated output | Provider error, routing decision and terminal status | Offline + controlled integration |

## G3 Offline Outcome Evidence

| Task | Offline result | Artifact projection | Evidence and decision assertions | Fixture |
|---|---|---|---|---|
| E2E-02 | `suspended` | None before human clarification | Admitted `ask_human` plan, `PendingAction`, resumable Verified checkpoint and zero tool exposure | `runtime/e2e_goal_tasks_test.go::TestE2E02AmbiguousRequestProducesClarificationOutcome` |
| E2E-11 | `verified` | One `content.draft` Artifact with final-answer digest/reference only | Research Tool observation plus Artifact evidence satisfy both required criteria; Artifact lists both supporting evidence IDs | `runtime/e2e_goal_tasks_test.go::TestE2E11ResearchThenDraftProducesVerifiedArtifact` |
| E2E-18 | `verified` after one repair | One verified `agent.response` Artifact | Failed Observation remains in the cumulative Run; one `execution_failed` recovery decision and second admitted plan are recorded | `runtime/e2e_goal_tasks_test.go::TestE2E18ToolFailureRepairsOnceWithAuditableOutcome` |

## G4 Offline Outcome Evidence

| Task | Offline result | Artifact projection | Evidence and decision assertions | Fixture |
|---|---|---|---|---|
| E2E-12 | `suspended` after verified conflict | One `agent.evidence_conflict.clarification.v1` Artifact with prompt digest/run reference only | Two paired trusted observations assert different canonical values from different public references; the exact configured `ask_human` occurs after all conflict evidence. Matching values, silent selection, unrelated/early questions, unpaired observations and forged ledgers fail closed | `evidence/evidence_conflict_goal_test.go::TestEvidenceConflictVerifiedRunnerProducesVerifiedSuspensionOutcome` |
| E2E-13 | `suspended`, then `verified` after approval resume | No final Artifact; authoritative environment Evidence references the created tweet | The real ReAct approval checkpoint persists the Before snapshot; zero writes occur before approval, resume executes the real `create_tweet` MCP handler once, and Timeline After state contains only the target addition | `service/tweet_publish_goal_integration_test.go::TestTweetPublishGoalApprovalAndIdempotentReplay` |
| E2E-14 | `verified` idempotent replay | No final Artifact; replayed structured result points to the original tweet | A second complete run with the same stable key restores persisted structured output through ToolExecutor, performs no second TweetService write, and proves unchanged Before/After state containing the target | `service/tweet_publish_goal_integration_test.go::TestTweetPublishGoalApprovalAndIdempotentReplay` |
| E2E-15 | `verified` tenant-bound read | No final Artifact; one digest-only Tool Observation Evidence item | Before/After snapshots bind the authenticated actor, active connection Revision and reviewed schema/policy digest; the real Runtime and External MCP Manager execute exactly one paired read observation, while another tenant receives an empty catalog | `service/external_mcp_goal_integration_test.go::TestExternalMCPReadGoalUsesCurrentTenantBinding` |
| E2E-16 | `suspended`, then blocked after revocation | No final Artifact | The write first produces an `approval_required` ToolExecutor audit with zero remote calls. After approval, the connection Revision/status changes; Resume rebuilds the current Environment catalog, rejects the unavailable tool before execution and keeps remote calls at zero | `service/external_mcp_goal_integration_test.go::TestExternalMCPWriteApprovalThenRevocationFailsBeforeRemoteCall` |
| E2E-17 | `verified` exact published workflow | No final Artifact; one digest-only Tool Observation Evidence item | Before/After snapshots bind the authenticated actor and immutable publication digest. The real Scheduler creates one authoritative child run using the published Revision/DSL Hash; the structured result binds parent Run/Action, child Run, declared response digest and persisted OutputJSON digest. A child node failure propagates as a parent Tool error and cannot emit completion evidence | `service/workflow_tool_goal_integration_test.go::TestWorkflowToolGoalExecutesExactRevisionAndVerifiesChildOutput` and `TestWorkflowToolGoalPropagatesChildFailureWithoutCompletionEvidence` |
| E2E-19 | `suspended` at approval revision 1, `suspended` at human-input revision 2, then `verified` | No final Artifact; one authoritative Tweet state Evidence item and two digest-only Checkpoint Resume Evidence items | JSON checkpoint round-trips preserve the same Run. Approval resume executes `create_tweet` once and persists its Observation before the later human checkpoint; human resume continues at the next Step. Checkpoint revisions increase monotonically, resume references bind revisions 1 and 2, while the real idempotency store records one completion and the TweetService fake records one write | `service/checkpoint_resume_goal_integration_test.go::TestCheckpointResumePreservesCompletedWriteAndEvidence` |
| E2E-20 | `verified` through an allowed fallback, or `blocked` when fallback is denied/exhausted | No Artifact on blocked routes; one digest-only Provider Routing Evidence item | Catalog traversal remains limited to explicit capability-compatible fallbacks. Transient/allowed failure records `fallback_allowed`; permanent 4xx/auth failure records `fallback_denied` and makes zero fallback calls; all unavailable routes end `fallback_exhausted`. Blocked Goal outcomes retain one failed model Step, no assistant/final answer, fixed failure codes and a low-sensitive route digest/reference | `service/provider_outage_goal_integration_test.go::TestProviderOutageGoalUsesAllowedFallbackOrBlocksWithoutFabrication` |

These G3/G4 fixtures are deterministic and offline. E2E-13/14 use an in-process controlled composition of the real Runtime, ToolExecutor, MCP handler and Timeline adapter. E2E-15/16 compose the real Runtime, External MCP Environment/Manager and ToolExecutor with an in-memory connection store and remote Caller. E2E-17 composes the real Runtime, Workflow Environment, ToolExecutor and DAG Scheduler with in-memory publication/run stores. E2E-19 composes the real Runtime, ToolExecutor, MCP handler and Tweet Write Environment across two JSON checkpoint round-trips with in-memory approval/idempotency stores. E2E-20 composes the real Catalog/ProviderRouter, ReActRunner and VerifiedRunner with in-memory Provider clients to exercise allowed fallback, denied fallback and route exhaustion. They do not connect deployed services or enable production Goal execution.

## G4 Migration Outcome Evidence

| Task | Legacy execution owner | Goal projection | Parity and negative assertions | Fixture |
|---|---|---|---|---|
| E2E-05 | Existing governed platform-search Runtime; exactly one Runner call | `observed_execution`, `verified`, no admitted-plan claim | Legacy Citation and Goal Evidence share the structured `/tweets/{id}` reference; text-only output is blocked by Goal verification | `service/goal_runtime_migration_test.go::TestE2E05PlatformSearchMigrationDualRecordsSingleExecution` |
| E2E-06 | Existing governed platform-search Runtime with only `get_tweets_by_ids` exposed for the follow-up; exactly one Runner call | `observed_execution`, `verified`, prior-reference plus detail-observation evidence | Selected ID must come from trusted dialogue metadata; ambiguous, forged, multi-ID and text-only claims fail closed; Legacy Citation and both Goal Evidence items share `/tweets/{id}` | `service/goal_runtime_followup_migration_test.go::TestE2E06PlatformTweetFollowUpMigrationDualRecordsSingleExecution` |
| E2E-07 | Existing governed Web Runtime with `web_search` and `page_read`; exactly one Runner call | `observed_execution`, `verified`, search-source plus page-content evidence | Search result URL, later `page_read` Action URL, structured page URL, Legacy Citation and both Goal Evidence references must resolve to the same canonical public URL; search-only, forged page URLs and text-only evidence remain blocked | `service/goal_runtime_web_research_migration_test.go::TestE2E07WebResearchMigrationDualRecordsSingleExecution` |
| E2E-08 | Existing governed Web Runtime; exactly one Runner call | `observed_execution`, `blocked`, stable search/page reason code plus low-sensitivity diagnostic evidence | Empty result, Provider/Page error and private or malformed citation fail closed; pending-background claims are not persisted; diagnostics contain only fixed-code digest and structural metadata | `service/goal_runtime_web_missing_evidence_migration_test.go::TestE2E08EmptyWebSearchBlocksPendingClaimWithSingleExecution` |
| E2E-09 | Existing governed platform/Web draft Runtime; exactly one Runner call | `observed_execution`, verified `content.draft` Artifact plus source Evidence | Draft must contain a nearby exact `[/tweets/{id}]` or `[public URL]` marker that resolves to its structured source; missing, forged, cross-source and detached markers are blocked without storing draft/source bodies | `service/goal_runtime_grounded_draft_migration_test.go::TestE2E09PlatformGroundedDraftDualRecordsSingleExecution` |
| E2E-10 | Deterministic completed `RunResult` fixture; no production route or additional execution | `observed_execution`, verified or failed `content.rewrite` Artifact | Canonical explicit constraints bind language, JSON/Markdown-list/plain-text format and Unicode character range; mismatches, task drift, tools and forged Artifact digests fail closed without storing the rewritten body | `evidence/rewrite_constraint_goal_test.go::TestE2E10RewriteConstraintsVerified` |
| E2E-11 | Existing governed platform/Web research-draft Runtime; exactly one Runner call | `observed_execution`, verified `content.draft` Artifact plus source and order Evidence | The G3 admitted plan records research before response; the G4 shadow independently requires a trusted structured research Observation in an earlier Step than the matching terminal `final_answer` Action, then reuses E2E-09 exact citation and Artifact checks. Missing, failed, late or forged research remains blocked | `service/goal_runtime_research_draft_migration_test.go::TestE2E11ResearchThenDraftDualRecordsSingleExecution` |

These are offline migration contracts. E2E-05 through E2E-09 and E2E-11 are independent default-off shadows; E2E-10 is intentionally verifier-only until an explicit structured request contract exists. They do not replace the Legacy response path or prove live Provider/Profile conformance.

## Release Gates

### Deterministic Gate

- All 20 tasks have offline fixtures.
- All policy, budget, approval, idempotency and verification negative cases pass.
- No task succeeds from final-answer text alone.
- Runtime and ToolExecutor tests pass with race checks where shared state is involved.

### Controlled Integration Gate

- E2E-05, 06, 07, 09, 13, 14, 15, 16, 17, 19 and 20 run against explicitly enabled local/controlled dependencies.
- External or paid providers require separate user authorization.
- Reports distinguish code failure, environment failure and unavailable dependency.

### Demonstration Gate

- The UI shows the current plan, executed tools, approval/blocked state and concise evidence references.
- A failed task is understandable without browser alerts or raw RPC errors.
- The five primary demonstrations are platform search, grounded research, draft creation, approved publish and custom Workflow-as-Tool.

## Ownership

- Runtime owns task state, repair limits and verification protocol.
- Environment adapters own state observation, not action authorization.
- ToolExecutor owns action policy, approval, audit, idempotency and circuit breaking.
- Service owns user/project resolution and persistence orchestration.
- Eval owns repeatable execution and scoring, not production routing.
