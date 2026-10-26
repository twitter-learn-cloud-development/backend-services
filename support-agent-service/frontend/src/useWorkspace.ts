import { computed, ref } from "vue";
import { ApiError, createAttempt, login, sendChat } from "./api.ts";
import {
  demoReply,
  sampleConversations,
  starters,
  type SampleId,
} from "./demo.ts";
import type {
  Account,
  Attempt,
  ChatMessage,
  Conversation,
  Mode,
} from "./types.ts";

const now = () => new Date().toISOString();

function demoDelay(signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const abort = () => {
      clearTimeout(timer);
      reject(new DOMException("Stopped", "AbortError"));
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", abort);
      resolve();
    }, 850);
    signal.addEventListener("abort", abort, { once: true });
  });
}

export function useWorkspace() {
  const mode = ref<Mode>("demo");
  const token = ref("");
  const account = ref<Account | null>(null);
  const demoThreads = ref<Conversation[]>(sampleConversations());
  const liveThreads = ref<Conversation[]>([]);
  const selectedId = ref<string | null>(null);
  const draft = ref("");
  const pendingSample = ref<SampleId>();
  const busy = ref(false);
  const settingsOpen = ref(false);
  const loginBusy = ref(false);
  const loginError = ref("");
  const toast = ref("");
  const mobileSidebar = ref(false);
  const guideOpen = ref(false);
  let controller: AbortController | undefined;
  let sessionRevision = 0;
  let toastTimer: ReturnType<typeof setTimeout> | undefined;

  const threads = computed(() =>
    mode.value === "demo" ? demoThreads.value : liveThreads.value,
  );
  const conversation = computed(() =>
    threads.value.find((item) => item.id === selectedId.value),
  );
  const messages = computed(() => conversation.value?.messages || []);
  const latest = computed(() => {
    const last = messages.value.at(-1);
    return last?.role === "assistant" ? last : undefined;
  });
  const sourceCount = computed(
    () => latest.value?.response?.evidence_refs.length || 0,
  );

  function notify(text: string) {
    toast.value = text;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      toast.value = "";
    }, 2600);
  }

  function newConversation() {
    if (busy.value) return;
    selectedId.value = null;
    draft.value = "";
    pendingSample.value = undefined;
    mobileSidebar.value = false;
  }

  function selectConversation(id: string) {
    if (busy.value) return;
    selectedId.value = id;
    draft.value = "";
    pendingSample.value = undefined;
    mobileSidebar.value = false;
  }

  function changeMode(next: Mode) {
    if (busy.value || next === mode.value) return;
    mode.value = next;
    newConversation();
  }

  function choosePrompt(text: string) {
    if (busy.value) return;
    draft.value = text;
    pendingSample.value = starters.find((item) => item.prompt === text)?.id;
  }

  async function signIn(email: string, password: string) {
    if (loginBusy.value || busy.value) return;
    loginBusy.value = true;
    loginError.value = "";
    const revision = sessionRevision;
    const savedDraft = mode.value === "live" ? draft.value : "";
    try {
      const session = await login(email, password);
      if (revision !== sessionRevision) return;
      token.value = session.token;
      account.value = session.account;
      liveThreads.value = [];
      mode.value = "live";
      newConversation();
      draft.value = savedDraft;
      settingsOpen.value = false;
      notify(`已连接 ${session.account.username} 的账号`);
    } catch (error) {
      if (revision !== sessionRevision) return;
      loginError.value =
        error instanceof ApiError && error.status === 401
          ? "邮箱或密码不正确，请检查后重试。"
          : error instanceof ApiError
            ? error.message
            : "登录连接失败，请确认 Gateway 已启动。";
    } finally {
      if (revision === sessionRevision) loginBusy.value = false;
    }
  }

  function logout() {
    resetSession();
    mode.value = "demo";
    settingsOpen.value = false;
    notify("已退出账号，真实会话已从当前页面清空");
  }

  function resetSession() {
    sessionRevision++;
    controller?.abort();
    controller = undefined;
    busy.value = false;
    loginBusy.value = false;
    loginError.value = "";
    token.value = "";
    account.value = null;
    liveThreads.value = [];
    newConversation();
  }

  function connectSession(nextToken: string, nextAccount: Account | null) {
    if (mode.value === 'live' && nextToken === token.value && nextAccount?.id === account.value?.id) return;
    resetSession();
    mode.value = "live";
    token.value = nextToken;
    account.value = nextAccount;
  }

  async function perform(attempt: Attempt, sample?: SampleId) {
    const thread = threads.value.find((item) => item.id === attempt.threadId);
    if (!thread || busy.value) return;
    const activeMode = mode.value;
    const revision = sessionRevision;
    busy.value = true;
    const activeController = new AbortController();
    controller = activeController;
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      activeController.abort();
    }, 125000);
    try {
      let message: ChatMessage;
      if (activeMode === "demo") {
        await demoDelay(activeController.signal);
        const reply = demoReply(sample);
        message = {
          id: crypto.randomUUID(),
          role: "assistant",
          text: reply.answer,
          status: reply.status,
          actions: reply.actions,
          createdAt: now(),
        };
      } else {
        const response = await sendChat(
          attempt,
          token.value,
          activeController.signal,
        );
        if (revision !== sessionRevision) return;
        thread.remoteId = response.conversation_id;
        message = {
          id: crypto.randomUUID(),
          role: "assistant",
          text: response.answer,
          response,
          status: response.status,
          actions: response.suggested_actions,
          createdAt: response.created_at,
        };
      }
      if (revision !== sessionRevision) return;
      thread.messages.push(message);
      thread.updatedAt = now();
    } catch (error) {
      if (revision !== sessionRevision) return;
      const stopped =
        error instanceof DOMException && error.name === "AbortError";
      const apiError = error instanceof ApiError ? error : undefined;
      const text = stopped
        ? timedOut
          ? "等待时间较长，结果暂时未知。可以重试同一次请求。"
          : activeMode === "demo"
            ? "已停止这次演示。"
            : "已停止等待。服务端可能仍在处理，可以重试同一次请求获取结果。"
        : apiError?.message || "连接中断。你的问题已保留，可以重试同一次请求。";
      const retryable =
        activeMode === "live" &&
        ![400, 401, 403, 409, 413, 422].includes(apiError?.status || 0);
      thread.messages.push({
        id: crypto.randomUUID(),
        role: "error",
        text,
        createdAt: now(),
        ...(retryable ? { attempt } : {}),
        requestId: apiError?.requestId,
      });
      if (apiError?.status === 401) {
        token.value = "";
        account.value = null;
      }
    } finally {
      clearTimeout(timer);
      if (revision === sessionRevision) {
        controller = undefined;
        busy.value = false;
      }
    }
  }

  async function send() {
    const text = draft.value.trim();
    if (!text || text.length > 8000 || busy.value) return;
    if (mode.value === "live" && !token.value) {
      settingsOpen.value = true;
      return;
    }
    let thread = conversation.value;
    if (!thread) {
      thread = {
        id: crypto.randomUUID(),
        title: text.slice(0, 20),
        updatedAt: now(),
        messages: [],
      };
      threads.value.unshift(thread);
      selectedId.value = thread.id;
    }
    const sample = starters.find(
      (item) => item.id === pendingSample.value && item.prompt === text,
    )?.id;
    thread.messages.push({
      id: crypto.randomUUID(),
      role: "user",
      text,
      createdAt: now(),
    });
    thread.sample = false;
    draft.value = "";
    pendingSample.value = undefined;
    await perform(createAttempt(text, thread.id, thread.remoteId), sample);
  }

  async function retry(message: ChatMessage) {
    if (!message.attempt || busy.value) return;
    if (!token.value) {
      settingsOpen.value = true;
      return;
    }
    const thread = conversation.value;
    if (!thread || thread.id !== message.attempt.threadId) return;
    // Once a newer turn exists, retrying the old payload would reorder history.
    if (thread.messages.at(-1)?.id !== message.id) {
      notify("这条请求后已有新消息，请重新描述问题");
      return;
    }
    thread.messages = thread.messages.filter((item) => item.id !== message.id);
    await perform(message.attempt);
  }

  function stop() {
    controller?.abort();
  }

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      notify("已复制答复");
    } catch {
      notify("复制失败，请选中文字手动复制");
    }
  }

  return {
    mode,
    account,
    token,
    threads,
    conversation,
    messages,
    latest,
    sourceCount,
    draft,
    busy,
    settingsOpen,
    loginBusy,
    loginError,
    toast,
    mobileSidebar,
    guideOpen,
    notify,
    newConversation,
    selectConversation,
    changeMode,
    choosePrompt,
    signIn,
    logout,
    connectSession,
    send,
    retry,
    stop,
    copy,
  };
}
