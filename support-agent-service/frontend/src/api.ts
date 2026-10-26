import type {
  Account,
  Attempt,
  SupportResponse,
  SupportStatus,
  SupportCitation,
} from "./types.ts";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const statuses: SupportStatus[] = [
  "answered",
  "partial",
  "clarification_required",
  "unsupported",
];
const object = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);
const strings = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((item) => typeof item === "string");

export class ApiError extends Error {
  requestId?: string;
  status: number;
  constructor(message: string, status = 0, requestId?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.requestId = requestId;
  }
}

export function parseSupportResponse(value: unknown): SupportResponse {
  if (
    !object(value) ||
    !["request_id", "conversation_id", "case_id"].every(
      (field) =>
        typeof value[field] === "string" && UUID.test(value[field] as string),
    ) ||
    !statuses.includes(value.status as SupportStatus) ||
    typeof value.answer !== "string" ||
    !value.answer.trim() ||
    !strings(value.suggested_actions) ||
    !strings(value.evidence_refs) ||
    typeof value.created_at !== "string" ||
    !Number.isFinite(Date.parse(value.created_at)) ||
    typeof value.trace_id !== "string"
  ) {
    throw new ApiError("服务返回的答复格式不完整，请稍后重试。");
  }
  if (value.citations !== undefined) {
    if (!Array.isArray(value.citations) || value.citations.length > 100) {
      throw new ApiError("引用格式不完整，请稍后重试。");
    }
    const seen = new Set<string>();
    for (const citation of value.citations) {
      if (!isTweetCitation(citation) || !value.evidence_refs.includes(citation.evidence_ref) || seen.has(citation.id)) {
        throw new ApiError("引用没有通过校验，请稍后重试。");
      }
      seen.add(citation.id);
    }
  }
  return value as unknown as SupportResponse;
}

export function isTweetCitation(value: unknown): value is SupportCitation {
  if (!object(value) || value.kind !== "tweet" || typeof value.id !== "string" ||
    typeof value.path !== "string" || !/^\/tweets\/[0-9]+$/.test(value.path) ||
    typeof value.label !== "string" || !value.label.trim() || value.label.length > 2000 ||
    typeof value.evidence_ref !== "string" || !value.evidence_ref) return false;
  return value.id === `tweet:${value.path.slice('/tweets/'.length)}`;
}

export function tweetHref(citation: SupportCitation): string {
  if (!isTweetCitation(citation)) throw new ApiError("引用链接无效。");
  const base = import.meta.env?.VITE_TWITTER_WEB_URL || "http://127.0.0.1:5173";
  const origin = new URL(base);
  if (!['http:', 'https:'].includes(origin.protocol) || origin.username || origin.password) {
    throw new ApiError("Twitter 页面地址未正确配置。");
  }
  return new URL(citation.path, origin.origin).href;
}

export function createAttempt(
  message: string,
  threadId: string,
  conversationId?: string,
): Attempt {
  return {
    key: crypto.randomUUID(),
    threadId,
    body: {
      message,
      client_timezone:
        Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Shanghai",
      ...(conversationId ? { conversation_id: conversationId } : {}),
    },
  };
}

export function requestError(status: number): string {
  const messages: Record<number, string> = {
    400: "请求未被接受，请新建对话后重试。",
    401: "登录已失效，请重新登录后继续。",
    403: "当前账号无法访问这段对话，请新建对话。",
    409: "这次请求的编号与已有内容冲突，请保留当前问题并新建对话。",
    413: "问题过长，请精简后重新发送。",
    422: "问题格式未通过校验，请检查内容后重新发送。",
    429: "发送有些频繁，请稍等片刻后重试。",
    500: "服务暂时无法完成请求，请稍后重试。",
    502: "本地服务连接失败，请确认助手和 Gateway 已启动。",
    503: "助手或会话服务暂时不可用，请稍后重试。",
    504: "本次等待超时，结果暂时未知。你可以重试同一次请求。",
  };
  return messages[status] || "无法连接本地服务。你的问题已保留，请稍后重试。";
}

async function jsonResponse(
  response: Response,
  errors: Record<number, string> = {},
): Promise<unknown> {
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    if (!response.ok)
      throw new ApiError(
        errors[response.status] || requestError(response.status),
        response.status,
      );
    throw new ApiError("服务没有返回有效答复，请稍后重试。");
  }
  if (!response.ok) {
    const requestId =
      object(payload) && typeof payload.request_id === "string"
        ? payload.request_id
        : undefined;
    throw new ApiError(
      errors[response.status] || requestError(response.status),
      response.status,
      requestId,
    );
  }
  return payload;
}

export async function sendChat(
  attempt: Attempt,
  token: string,
  signal: AbortSignal,
): Promise<SupportResponse> {
  const response = await fetch("/api/v1/support/chat", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      Authorization: `Bearer ${token}`,
      "Idempotency-Key": attempt.key,
    },
    body: JSON.stringify(attempt.body),
    signal,
  });
  const result = parseSupportResponse(await jsonResponse(response));
  if (
    attempt.body.conversation_id &&
    result.conversation_id !== attempt.body.conversation_id
  ) {
    throw new ApiError("返回的会话与当前对话不一致，请新建对话。");
  }
  return result;
}

export async function login(
  email: string,
  password: string,
): Promise<{ token: string; account: Account }> {
  const response = await fetch("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
    signal: AbortSignal.timeout(15000),
  });
  const payload = await jsonResponse(response, {
    401: "邮箱或密码不正确，请检查后重试。",
    403: "登录请求被拒绝，请确认本地页面的连接配置。",
  });
  if (!object(payload) || typeof payload.token !== "string" || !payload.token) {
    throw new ApiError("登录未返回有效身份，请重新登录。");
  }
  const me = await fetch("/api/v1/users/me", {
    headers: { Authorization: `Bearer ${payload.token}` },
    signal: AbortSignal.timeout(10000),
  });
  const profile = await jsonResponse(me, {
    403: "当前账号无法读取个人资料，请重新登录。",
  });
  const user = object(profile) && object(profile.user) ? profile.user : profile;
  if (
    !object(user) ||
    (typeof user.id !== "string" && typeof user.id !== "number") ||
    typeof user.username !== "string"
  ) {
    throw new ApiError("暂时无法获取账号资料，请稍后重试。");
  }
  return {
    token: payload.token,
    account: {
      id: String(user.id),
      username: user.username,
      email: typeof user.email === "string" ? user.email : email,
    },
  };
}

export async function ready(): Promise<boolean> {
  try {
    const response = await fetch("/health/ready", {
      signal: AbortSignal.timeout(3000),
    });
    const body: unknown = await response.json();
    return response.ok && object(body) && body.status === "ready";
  } catch {
    return false;
  }
}
