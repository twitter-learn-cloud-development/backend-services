import assert from "node:assert/strict";
import { afterEach, describe, it } from "node:test";
import {
  ApiError,
  createAttempt,
  isTweetCitation,
  parseSupportResponse,
  sendChat,
  tweetHref,
} from "../src/api.ts";
import type { SupportCitation } from "../src/types.ts";

const ids = {
  request: "11111111-1111-4111-8111-111111111111",
  conversation: "22222222-2222-4222-8222-222222222222",
  case: "33333333-3333-4333-8333-333333333333",
};

function validResponse(overrides: Record<string, unknown> = {}) {
  return {
    request_id: ids.request,
    conversation_id: ids.conversation,
    case_id: ids.case,
    status: "answered",
    answer: "可以这样处理。",
    suggested_actions: [],
    evidence_refs: ["ticket:42"],
    created_at: "2026-10-02T01:02:03Z",
    trace_id: "trace-example",
    ...overrides,
  };
}

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe("support chat request contract", () => {
  it("starts without a conversation id and creates a UUID idempotency key", () => {
    const attempt = createAttempt("如何重置密码？", "thread-1");
    assert.match(
      attempt.key,
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
    );
    assert.equal(attempt.threadId, "thread-1");
    assert.equal(attempt.body.message, "如何重置密码？");
    assert.equal("conversation_id" in attempt.body, false);
    assert.equal(typeof attempt.body.client_timezone, "string");
  });

  it("includes the existing conversation id on a continuation", () => {
    const attempt = createAttempt("再解释一下", "thread-1", ids.conversation);
    assert.equal(attempt.body.conversation_id, ids.conversation);
  });

  it("sends the attempt body and idempotency key unchanged when the caller retries", async () => {
    const attempt = createAttempt(
      "查一下我的订单",
      "thread-1",
      ids.conversation,
    );
    const captured: Array<{ key: string | null; body: string | null }> = [];
    let calls = 0;
    globalThis.fetch = async (_input, init) => {
      const headers = new Headers(init?.headers);
      captured.push({
        key: headers.get("Idempotency-Key"),
        body: init?.body as string,
      });
      calls += 1;
      return calls === 1
        ? new Response(JSON.stringify({ request_id: ids.request }), {
            status: 503,
          })
        : new Response(JSON.stringify(validResponse()), { status: 200 });
    };

    await assert.rejects(
      sendChat(attempt, "test-token", new AbortController().signal),
      ApiError,
    );
    const result = await sendChat(
      attempt,
      "test-token",
      new AbortController().signal,
    );
    assert.equal(result.status, "answered");
    assert.deepEqual(captured, [
      { key: attempt.key, body: JSON.stringify(attempt.body) },
      { key: attempt.key, body: JSON.stringify(attempt.body) },
    ]);
  });

  it("rejects malformed successful responses instead of presenting them as answers", async (t) => {
    const invalid: Array<[string, Record<string, unknown>]> = [
      ["invalid status", { status: "done" }],
      ["invalid conversation UUID", { conversation_id: "not-a-uuid" }],
      ["blank answer", { answer: "  " }],
      ["invalid evidence schema", { evidence_refs: [42] }],
      ["invalid timestamp", { created_at: "not-a-date" }],
    ];
    for (const [name, override] of invalid) {
      await t.test(name, async () => {
        globalThis.fetch = async () =>
          new Response(JSON.stringify(validResponse(override)), {
            status: 200,
          });
        await assert.rejects(
          sendChat(
            createAttempt("问题", "thread-1"),
            "test-token",
            new AbortController().signal,
          ),
          (error: unknown) =>
            error instanceof ApiError && /格式不完整/.test(error.message),
        );
      });
    }
  });

  it("rejects a response associated with a different continuation conversation", async () => {
    globalThis.fetch = async () =>
      new Response(
        JSON.stringify(
          validResponse({
            conversation_id: ids.case,
          }),
        ),
        { status: 200 },
      );
    await assert.rejects(
      sendChat(
        createAttempt("继续", "thread-1", ids.conversation),
        "test-token",
        new AbortController().signal,
      ),
      (error: unknown) =>
        error instanceof ApiError && /会话/.test(error.message),
    );
  });

  it("maps temporary service failures to actionable retry feedback", async () => {
    globalThis.fetch = async () =>
      new Response(JSON.stringify({ request_id: ids.request }), {
        status: 504,
      });
    await assert.rejects(
      sendChat(
        createAttempt("问题", "thread-1"),
        "test-token",
        new AbortController().signal,
      ),
      (error: unknown) =>
        error instanceof ApiError &&
        error.status === 504 &&
        error.requestId === ids.request &&
        /重试同一次请求/.test(error.message),
    );
  });
});

describe("tweet citation validation", () => {
  const citation: SupportCitation = {
    id: "tweet:987654321012345678",
    kind: "tweet",
    label: "一条可核验的推文",
    path: "/tweets/987654321012345678",
    evidence_ref: "tweet:987654321012345678",
  };

  it("accepts a matching, evidence-backed internal tweet citation", () => {
    const parsed = parseSupportResponse(
      validResponse({ citations: [citation], evidence_refs: [citation.evidence_ref] }),
    );
    assert.deepEqual(parsed.citations, [citation]);
    assert.equal(isTweetCitation(citation), true);
    assert.equal(
      new URL(tweetHref(citation)).pathname,
      citation.path,
    );
  });

  it("keeps citations optional for backwards compatibility and allows empty demo citations", () => {
    assert.equal(parseSupportResponse(validResponse()).citations, undefined);
    assert.deepEqual(parseSupportResponse(validResponse({ citations: [] })).citations, []);
  });

  it("rejects malformed, duplicate, ungrounded, or non-internal citations", async (t) => {
    const invalidCases: Array<[string, unknown[]]> = [
      ["id and path mismatch", [{ ...citation, id: "tweet:123" }]],
      ["external URL", [{ ...citation, path: "https://example.com/tweets/987654321012345678" }]],
      ["protocol-relative URL", [{ ...citation, path: "//example.com/tweets/987654321012345678" }]],
      ["javascript URL", [{ ...citation, path: "javascript:alert(1)" }]],
      ["unknown internal route", [{ ...citation, path: "/users/987654321012345678" }]],
      ["non-numeric tweet id", [{ ...citation, id: "tweet:abc", path: "/tweets/abc" }]],
      ["query or fragment suffix", [{ ...citation, path: `${citation.path}?next=//evil.test` }]],
      ["unreferenced evidence", [{ ...citation, evidence_ref: "not-in-evidence" }]],
      ["duplicate resource id", [citation, { ...citation, label: "duplicate" }]],
      ["wrong kind", [{ ...citation, kind: "user" }]],
      ["blank label", [{ ...citation, label: " " }]],
    ];
    for (const [name, citations] of invalidCases) {
      await t.test(name, () => {
        assert.throws(
          () => parseSupportResponse(validResponse({
            citations,
            evidence_refs: [citation.evidence_ref],
          })),
          ApiError,
        );
      });
    }
  });

  it("refuses to turn a forged citation into a link", () => {
    assert.throws(
      () => tweetHref({ ...citation, path: "https://evil.example/" }),
      ApiError,
    );
  });
});
