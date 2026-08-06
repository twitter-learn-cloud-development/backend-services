# ADR-002: External MCP Write Idempotency Contract

- Status: Accepted
- Date: 2026-07-27
- Owners: Agent Runtime / Workflow

## Context

MCP `ToolAnnotation.idempotentHint` is advisory. It says repeated calls with the same arguments should have no additional effect, but it does not define how a client distinguishes two intentional business operations from a retry of one logical execution. Approval alone also cannot prevent duplication when the remote write succeeds and the response is lost.

The platform therefore needs a contract that is reviewable in the immutable Tool Snapshot, can be enforced before transport, survives Workflow suspension/resume, and does not expose internal Run or Step identifiers to a third-party server.

## Decision

An external MCP tool may be enabled as `write` only when its reviewed definition satisfies all conditions:

1. `annotations.idempotentHint` is `true`.
2. `_meta["io.twitter-clone/idempotency-key-argument"]` is a valid top-level property name.
3. That property exists in `inputSchema`, has `type: "string"`, and is listed in `required`.

Discovery persists `declared_idempotent` and `idempotency_key_argument` in the immutable Snapshot. Runtime resolution rechecks the persisted Schema and fails closed if it is incomplete or inconsistent. Old Snapshots have zero values and remain unable to execute as `write`; no data migration is required.

Workflow derives a stable local key from Run, Step and qualified Tool identity. Before sending a request, the MCP Manager derives:

```text
tc_mcp_ + hex(SHA-256("external-mcp-write:v1:" + local_execution_key))
```

The platform overwrites the declared argument immediately before validation/approval and again immediately before the transport call. Model, DSL and user values cannot select the key. The remote server never receives the raw Run, Step or Tool execution identity.

Write tools remain Workflow-only, require persistent approval and ToolExecutor idempotency, and may retry a temporary failure at most once with identical arguments and the same remote key. Risky tools remain single-attempt. External MCP compensation remains disabled.

## Consequences

- Cooperating servers can distinguish a retry from a separate intentional operation.
- Schema changes create a new review boundary and clear old policies.
- Servers that only set `idempotentHint` remain usable as `risky`, but cannot be enabled as `write`.
- The platform trusts the remote server to honor its declaration. This is a replay-safety contract, not proof of strict distributed exactly-once behavior.
- A timeout after the final attempt can still have an unknown outcome; operators must inspect the remote system using the idempotency key where supported.

## Rollback

Disable the affected Tool policy or `AGENT_EXTERNAL_MCP_ENABLED`. Older binaries already reject `write`; additive Proto and Snapshot fields are ignored safely. No stored credential, Workflow Revision or prior Snapshot needs destructive migration.

## Rejected Alternatives

- Trust `idempotentHint` alone: it cannot distinguish two intentional identical requests.
- Use approval ID or raw Run/Step ID as the remote key: this leaks platform internals and couples the remote contract to approval lifecycle details.
- Put the key only in MCP request `_meta`: there is no interoperable server-side deduplication guarantee for that custom field.
- Automatically retry all non-read tools: unsafe when the remote result is unknown and no deduplication contract exists.
