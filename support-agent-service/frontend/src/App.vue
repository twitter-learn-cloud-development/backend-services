<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import Icon from "./components/Icon.vue";
import SupportWidget from "../integration/SupportWidget.vue";
import { starters } from "./demo.ts";
import { ready, tweetHref } from "./api.ts";
import { hasSiteAiHandoff, siteAiHref } from "./handoff.ts";
import { useWorkspace } from "./useWorkspace.ts";
import type { SupportStatus } from "./types.ts";

const workspace = useWorkspace();
const windowOrigin = location.origin;
function openTweet(path: string) {
  const base = new URL(import.meta.env.VITE_TWITTER_WEB_URL || 'http://127.0.0.1:5173');
  if (/^\/tweets\/[0-9]+$/.test(path)) window.open(new URL(path, base.origin).href, '_blank', 'noopener,noreferrer');
}
const {
  mode,
  account,
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
} = workspace;
const serviceReady = ref<boolean | null>(null);
const search = ref("");
const searchOpen = ref(false);
const email = ref("");
const password = ref("");
const input = ref<HTMLTextAreaElement>();
const chatScroll = ref<HTMLElement>();
const loginInput = ref<HTMLInputElement>();
const modalElement = ref<HTMLElement>();
const sidebarElement = ref<HTMLElement>();
const compactMedia = window.matchMedia("(max-width: 780px)");
const compact = ref(compactMedia.matches);
const updateCompact = () => {
  compact.value = compactMedia.matches;
};
const composing = ref(false);
const nearBottom = ref(true);
let poll: ReturnType<typeof setInterval> | undefined;
const filteredThreads = computed(() =>
  threads.value.filter((item) =>
    item.title.toLowerCase().includes(search.value.toLowerCase()),
  ),
);
const statuses: Record<SupportStatus, string> = {
  answered: "已回答",
  partial: "已回答可确认部分",
  clarification_required: "需要你补充信息",
  unsupported: "当前能力范围有限",
};
const characterLimit = computed(() => draft.value.trim().length > 8000);

function focusInput() {
  nextTick(() => input.value?.focus());
}
function prompt(text: string) {
  workspace.choosePrompt(text);
  focusInput();
}
function newChat() {
  workspace.newConversation();
  focusInput();
}
function onKey(event: KeyboardEvent) {
  if (event.key === "Escape") {
    settingsOpen.value = false;
    guideOpen.value = false;
    mobileSidebar.value = false;
  }
  if (
    event.key === "Enter" &&
    !event.shiftKey &&
    !event.isComposing &&
    !composing.value
  ) {
    event.preventDefault();
    void submit();
  }
}
async function submit() {
  nearBottom.value = true;
  await workspace.send();
  focusInput();
}
function trackScroll() {
  const area = chatScroll.value;
  if (area)
    nearBottom.value =
      area.scrollHeight - area.scrollTop - area.clientHeight < 100;
}
async function scrollBottom(force = false) {
  await nextTick();
  if (chatScroll.value && (nearBottom.value || force))
    chatScroll.value.scrollTop = chatScroll.value.scrollHeight;
}
async function signIn() {
  try {
    await workspace.signIn(email.value.trim(), password.value);
  } finally {
    password.value = "";
  }
}
async function refreshHealth() {
  serviceReady.value = await ready();
}
function closeSettings() {
  if (!loginBusy.value) settingsOpen.value = false;
}
function documentKey(event: KeyboardEvent) {
  if (event.key === "Escape") {
    closeSettings();
    guideOpen.value = false;
    mobileSidebar.value = false;
  }
  const focusPanel =
    settingsOpen.value || guideOpen.value
      ? modalElement.value
      : compact.value && mobileSidebar.value
        ? sidebarElement.value
        : undefined;
  if (event.key === "Tab" && focusPanel) {
    const items = Array.from(
      focusPanel.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), a[href], summary, [tabindex="0"]',
      ) || [],
    ).filter((item) => item.offsetParent !== null);
    const first = items[0],
      last = items.at(-1);
    if (!first || !last) return;
    if (
      event.shiftKey &&
      (document.activeElement === first ||
        document.activeElement === focusPanel)
    ) {
      event.preventDefault();
      last.focus();
    } else if (
      !event.shiftKey &&
      (document.activeElement === last || document.activeElement === focusPanel)
    ) {
      event.preventDefault();
      first.focus();
    }
  }
}
watch([() => messages.value.length, busy], () => {
  void scrollBottom();
});
watch(
  () => conversation.value?.id,
  () => {
    nearBottom.value = true;
    void scrollBottom(true);
  },
);
watch(settingsOpen, async (value) => {
  password.value = "";
  loginError.value = "";
  if (value) {
    await nextTick();
    loginInput.value?.focus();
  } else focusInput();
});
watch(guideOpen, async (value) => {
  if (value) {
    await nextTick();
    modalElement.value?.focus();
  } else focusInput();
});
watch(mobileSidebar, async (value) => {
  await nextTick();
  if (value)
    sidebarElement.value
      ?.querySelector<HTMLButtonElement>(".new-chat")
      ?.focus();
  else if (compact.value)
    document.querySelector<HTMLButtonElement>(".mobile-menu")?.focus();
});
onMounted(() => {
  void refreshHealth();
  poll = setInterval(() => {
    if (!document.hidden) void refreshHealth();
  }, 30000);
  document.addEventListener("keydown", documentKey);
  compactMedia.addEventListener("change", updateCompact);
});
onUnmounted(() => {
  clearInterval(poll);
  workspace.stop();
  document.removeEventListener("keydown", documentKey);
  compactMedia.removeEventListener("change", updateCompact);
});
</script>

<template>
  <div class="workspace">
    <button
      v-if="mobileSidebar"
      class="sidebar-scrim"
      aria-label="关闭会话栏"
      @click="mobileSidebar = false"
    ></button>
    <aside
      ref="sidebarElement"
      class="sidebar"
      :class="{ open: mobileSidebar }"
      :inert="settingsOpen || guideOpen || (compact && !mobileSidebar)"
      aria-label="会话导航"
    >
      <a class="brand" href="#" @click.prevent="newChat">
        <span class="brand-mark"
          ><svg viewBox="0 0 48 48" aria-hidden="true">
            <path
              d="M10 29c11 1 11-19 27-19-4 8-1 21-13 26-6 2-12-1-14-7Z"
              fill="currentColor"
            />
            <circle cx="31" cy="18" r="1.4" fill="#fff" /></svg
        ></span>
        <span
          ><strong>小栖<span class="brand-dot">.</span></strong
          ><small>你的 Twitter 助手</small></span
        >
      </a>
      <button class="new-chat" :disabled="busy" @click="newChat">
        <Icon name="plus" :size="18" />开启新对话<span>NEW</span>
      </button>
      <div class="sidebar-section-label">
        <span>本次浏览器会话</span
        ><button
          class="icon-button"
          aria-label="搜索当前会话"
          @click="searchOpen = !searchOpen"
        >
          <Icon name="search" :size="16" />
        </button>
      </div>
      <input
        v-if="searchOpen"
        v-model="search"
        class="thread-search"
        placeholder="搜索会话…"
        aria-label="搜索会话"
      />
      <div class="thread-list">
        <button
          v-for="thread in filteredThreads"
          :key="thread.id"
          class="thread"
          :class="{ active: thread.id === conversation?.id }"
          :disabled="busy"
          @click="workspace.selectConversation(thread.id)"
        >
          <Icon name="chat" :size="17" /><span>{{ thread.title }}</span
          ><small v-if="thread.sample">示例</small>
        </button>
        <p v-if="!filteredThreads.length" class="empty-threads">
          {{ search ? "没有找到这段会话" : "新的问题，从这里开始。" }}
        </p>
      </div>
      <div class="sidebar-bottom">
        <div class="sidebar-note">
          <span class="little-star">✦</span>
          <p>让问题有答案，<br />让表达更自在。</p>
          <span class="note-line"></span>
        </div>
        <button class="utility-link" @click="guideOpen = true">
          <Icon name="book" :size="18" />使用指南<Icon
            name="arrow"
            :size="14"
          />
        </button>
        <button
          class="account-button"
          :disabled="busy"
          @click="settingsOpen = true"
        >
          <span class="avatar">{{
            account?.username.slice(0, 1).toUpperCase() || "栖"
          }}</span>
          <span
            ><strong>{{ account?.username || "访客体验" }}</strong
            ><small>{{
              mode === "demo"
                ? "演示空间 · 不查询真实账号"
                : account
                  ? "已连接你的账号"
                  : "登录后开始真实咨询"
            }}</small></span
          ><Icon name="settings" :size="17" />
        </button>
      </div>
    </aside>

    <div
      class="main-workspace"
      :inert="settingsOpen || guideOpen || (compact && mobileSidebar)"
    >
      <header class="topbar">
        <div class="breadcrumb">
          <button
            class="icon-button mobile-menu"
            aria-label="打开会话栏"
            @click="mobileSidebar = true"
          >
            <Icon name="menu" /></button
          ><span class="breadcrumb-root">工作空间</span
          ><span class="breadcrumb-separator">/</span><strong>智能助手</strong
          ><span class="beta">BETA</span>
        </div>
        <div class="topbar-actions">
          <span class="mode-pill" :class="{ live: mode === 'live' }"
            ><span></span>{{ mode === "demo" ? "演示模式" : "真实连接" }}</span
          ><button
            class="connect-button"
            :disabled="busy"
            @click="settingsOpen = true"
          >
            {{ account ? "连接设置" : "连接账号"
            }}<Icon name="arrow" :size="15" />
          </button>
        </div>
      </header>

      <div class="content-columns">
        <main class="chat-panel" aria-label="智能助手对话">
          <section v-if="!messages.length" class="welcome">
            <div class="welcome-eyebrow">
              <span class="tiny-orbit"></span> YOUR PERSONAL SUPPORT SPACE
            </div>
            <h1>有什么想解决的<span class="accent">？</span></h1>
            <p class="welcome-description">
              说说你遇到的问题，小栖陪你找到答案。<br />从最近发布的内容，到那些不太确定的细节。
            </p>
            <div class="welcome-divider">
              <span>可以从这些问题开始</span><span class="divider-rule"></span>
            </div>
            <div class="starter-grid">
              <button
                v-for="starter in starters"
                :key="starter.id"
                class="starter-card"
                :disabled="busy"
                @click="prompt(starter.prompt)"
              >
                <span class="starter-icon" :class="starter.id"
                  ><Icon :name="starter.icon" :size="22" /></span
                ><strong>{{ starter.title }}</strong
                ><small>{{ starter.detail }}</small
                ><Icon name="arrow" :size="18" class="starter-arrow" />
              </button>
            </div>
            <div class="welcome-footnote">
              <Icon name="shield" :size="15" /><span>{{
                mode === "demo"
                  ? "这是界面演示。连接账号后，结果将来自真实查询。"
                  : "只读取当前账号允许访问的记录，结果附证据引用。"
              }}</span>
            </div>
          </section>

          <section
            v-else
            ref="chatScroll"
            class="messages"
            aria-label="当前消息"
            @scroll="trackScroll"
          >
            <div class="conversation-heading">
              <span class="eyebrow">{{
                mode === "demo" ? "DEMO CONVERSATION" : "YOUR CONVERSATION"
              }}</span>
              <h1>{{ conversation?.title }}</h1>
              <p>
                {{
                  mode === "demo"
                    ? "当前回复使用明确标识的界面示例。"
                    : "围绕同一个问题，可以继续追问。"
                }}
              </p>
            </div>
            <article
              v-for="message in messages"
              :key="message.id"
              class="message"
              :class="message.role"
            >
              <div class="message-avatar" v-if="message.role !== 'user'">
                <Icon
                  :name="message.role === 'error' ? 'retry' : 'sparkles'"
                  :size="20"
                />
              </div>
              <div class="message-content">
                <div class="message-byline">
                  <strong>{{
                    message.role === "user"
                      ? "你"
                      : message.role === "error"
                        ? "连接提示"
                        : "小栖"
                  }}</strong
                  ><span
                    v-if="message.role === 'assistant'"
                    class="response-label"
                    :class="message.status"
                    >{{
                      mode === "demo"
                        ? "演示答复"
                        : hasSiteAiHandoff(message.response)
                          ? "可前往站内 AI 助手"
                          : statuses[message.status || "partial"]
                    }}</span
                  >
                </div>
                <p class="message-text">{{ message.text }}</p>
                <nav v-if="hasSiteAiHandoff(message.response)" class="tweet-citations" aria-label="继续咨询站内 AI 助手">
                  <a :href="siteAiHref()" target="_blank" rel="noopener noreferrer">前往站内 AI 助手 ↗</a>
                  <small>不会自动发送当前问题，实际能力以站内 AI 助手为准。</small>
                </nav>
                <nav v-if="message.response?.citations?.length" class="tweet-citations" aria-label="本轮查询的推文引用">
                  <small>本轮可核实的推文 · 点击查看</small>
                  <a v-for="citation in message.response.citations" :key="citation.id" :href="tweetHref(citation)" target="_blank" rel="noopener noreferrer">{{ citation.label }} ↗</a>
                </nav>
                <div v-if="message.role === 'assistant'" class="answer-footer">
                  <button
                    class="icon-button"
                    aria-label="复制答复"
                    title="复制答复"
                    @click="workspace.copy(message.text)"
                  >
                    <Icon name="copy" :size="16" />
                  </button>
                  <details v-if="message.response" class="evidence-details">
                    <summary>
                      <Icon name="link" :size="14" />{{
                        message.response.evidence_refs.length
                      }}
                      条证据引用<Icon name="down" :size="13" />
                    </summary>
                    <div class="evidence-list">
                      <p v-if="!message.response.evidence_refs.length">
                        本次答复没有证据引用。
                      </p>
                      <code
                        v-for="source in message.response.evidence_refs"
                        :key="source"
                        >{{ source }}</code
                      ><small>引用由服务返回；当前没有证据详情入口。</small>
                    </div>
                  </details>
                </div>
                <div v-if="message.actions?.length" class="suggested-actions">
                  <span>你也可以继续问</span
                  ><button
                    v-for="action in message.actions"
                    :key="action"
                    :disabled="busy"
                    @click="prompt(action)"
                  >
                    {{ action }}<Icon name="chevron" :size="14" />
                  </button>
                </div>
                <div v-if="message.role === 'error'" class="error-actions">
                  <button
                    v-if="message.attempt"
                    :disabled="busy"
                    @click="workspace.retry(message)"
                  >
                    <Icon name="retry" :size="15" />重试同一次请求</button
                  ><button
                    v-else
                    :disabled="busy"
                    @click="
                      mode === 'live' && !account
                        ? (settingsOpen = true)
                        : newChat()
                    "
                  >
                    {{ mode === "live" && !account ? "重新登录" : "新建对话" }}
                  </button>
                  <details v-if="message.requestId">
                    <summary>请求编号</summary>
                    <code>{{ message.requestId }}</code>
                  </details>
                </div>
              </div>
            </article>
            <div v-if="busy" class="loading-message" role="status">
              <span class="loading-mark"
                ><Icon name="sparkles" :size="20"
              /></span>
              <div>
                <strong>{{
                  mode === "demo" ? "正在准备演示回复" : "正在等待助手答复"
                }}</strong
                ><span class="loading-dots"><i></i><i></i><i></i></span
                ><small>{{
                  mode === "demo"
                    ? "示例仅用于体验页面交互"
                    : "正在处理；结果以返回状态和证据为准"
                }}</small>
              </div>
            </div>
            <button
              v-if="!nearBottom"
              class="back-to-latest"
              @click="scrollBottom(true)"
            >
              <Icon name="down" :size="15" />回到最新消息
            </button>
          </section>

          <div class="composer-area">
            <form
              class="composer"
              :class="{ invalid: characterLimit }"
              @submit.prevent="submit"
            >
              <label class="sr-only" for="message-input">向小栖发送问题</label>
              <textarea
                id="message-input"
                ref="input"
                v-model="draft"
                :disabled="busy"
                placeholder="描述你的问题，或者继续追问…"
                rows="2"
                @keydown="onKey"
                @compositionstart="composing = true"
                @compositionend="composing = false"
              ></textarea>
              <div class="composer-bottom">
                <span class="composer-hint"
                  ><span class="composer-spark">✦</span
                  >{{
                    busy
                      ? "正在处理本次请求"
                      : mode === "demo"
                        ? "演示体验"
                        : account
                          ? `以 @${account.username} 的身份咨询`
                          : "登录后即可发送"
                  }}</span
                >
                <div class="composer-controls">
                  <span v-if="draft.length > 7000" class="character-count"
                    >{{ draft.length }} / 8000</span
                  ><span class="keyboard-hint">Enter 发送</span
                  ><button
                    v-if="busy"
                    class="send-button stop-button"
                    type="button"
                    aria-label="停止等待"
                    @click="workspace.stop()"
                  >
                    <Icon name="stop" :size="17" /></button
                  ><button
                    v-else
                    class="send-button"
                    type="submit"
                    aria-label="发送问题"
                    :disabled="!draft.trim() || characterLimit"
                  >
                    <Icon name="up" :size="20" />
                  </button>
                </div>
              </div>
            </form>
            <p class="composer-disclaimer">
              {{
                characterLimit
                  ? "问题超过 8000 字，请精简后发送。"
                  : "小栖会说明已知事实与未知部分。Shift + Enter 换行。"
              }}<span>小栖 · 为表达而来</span>
            </p>
          </div>
        </main>

        <aside class="context-panel" aria-label="咨询说明">
          <div class="context-heading">
            <span class="eyebrow">A LITTLE CLARITY</span
            ><span class="mini-stars">✧</span>
          </div>
          <div class="illustration-card">
            <svg
              class="nest-illustration"
              viewBox="0 0 280 205"
              aria-hidden="true"
            >
              <defs>
                <pattern
                  id="grid"
                  width="24"
                  height="24"
                  patternUnits="userSpaceOnUse"
                >
                  <path
                    d="M24 0H0V24"
                    fill="none"
                    stroke="#dbe4de"
                    stroke-width=".65"
                  />
                </pattern>
              </defs>
              <rect width="280" height="205" fill="url(#grid)" />
              <circle cx="209" cy="47" r="23" fill="#f1b37c" />
              <path d="M44 191v-66a84 84 0 0 1 168 0v66" fill="#9bb8ac" />
              <path d="M66 191v-66a62 62 0 0 1 124 0v66" fill="#f7f7ed" />
              <path
                d="M53 184c41 15 107 10 162-4"
                fill="none"
                stroke="#183346"
                stroke-width="2"
              />
              <path
                d="M108 153c29 3 16-55 58-58-8 18 0 47-26 63-13 7-23 2-32-5Z"
                fill="#183346"
              />
              <circle cx="152" cy="112" r="2" fill="#f8f6ec" />
              <path d="m164 105 10 5-13 5" fill="#ed7959" />
              <path
                d="M223 135v25m-12-12h24M30 58v14m-7-7h14"
                stroke="#183346"
                stroke-width="1.5"
              />
              <path
                d="m118 65 5-8m-14 2 1-9"
                stroke="#ed7959"
                stroke-width="2"
                stroke-linecap="round"
              /></svg
            ><span class="illustration-caption"
              >A calmer corner of your feed.</span
            >
          </div>
          <h2>让每个疑问，<br />都有一个着落。</h2>
          <p class="context-intro">
            把问题说清楚，把事实找明白。<br />小栖和你一起，慢慢理顺。
          </p>
          <div class="capability-notes">
            <div>
              <span><Icon name="shield" :size="17" /></span>
              <p>
                <strong>只关注你的账号</strong
                ><small>查询当前账号允许访问的信息</small>
              </p>
            </div>
            <div>
              <span><Icon name="link" :size="17" /></span>
              <p>
                <strong>答案有据可查</strong
                ><small>真实答复附带服务返回的引用</small>
              </p>
            </div>
            <div>
              <span><Icon name="chat" :size="17" /></span>
              <p>
                <strong>不确定，就继续问</strong
                ><small>告诉你能确认什么、还缺什么</small>
              </p>
            </div>
          </div>
          <div class="session-card">
            <div class="session-title">
              <Icon name="dot" :size="15" /><strong>{{
                messages.length ? "这次对话" : "你的咨询空间"
              }}</strong>
            </div>
            <dl>
              <div>
                <dt>当前模式</dt>
                <dd>{{ mode === "demo" ? "界面演示" : "真实咨询" }}</dd>
              </div>
              <div>
                <dt>服务连接</dt>
                <dd class="service-status" :class="{ healthy: serviceReady }">
                  <span></span
                  >{{
                    serviceReady === null
                      ? "检查中"
                      : serviceReady
                        ? "助手已就绪"
                        : "等待本地服务"
                  }}
                </dd>
              </div>
              <div v-if="latest">
                <dt>证据引用</dt>
                <dd>
                  {{
                    mode === "demo" ? "演示不产生真实证据" : `${sourceCount} 条`
                  }}
                </dd>
              </div>
              <div v-if="latest?.status && mode === 'live'">
                <dt>答复状态</dt>
                <dd>{{ statuses[latest.status] }}</dd>
              </div>
            </dl>
            <small>{{
              mode === "demo"
                ? "演示不调用模型，不查询真实数据。"
                : "连接状态仅代表就绪检查；实际查询以答复为准。"
            }}</small>
            <details v-if="latest?.response" class="request-details">
              <summary>查看关联编号<Icon name="down" :size="13" /></summary>
              <p>
                会话 <code>{{ latest.response.conversation_id }}</code>
              </p>
              <p>
                请求 <code>{{ latest.response.request_id }}</code>
              </p>
              <p>
                追踪 <code>{{ latest.response.trace_id }}</code>
              </p>
            </details>
          </div>
          <button class="context-help" @click="guideOpen = true">
            第一次使用？看看提问小提示<Icon name="arrow" :size="14" />
          </button>
        </aside>
      </div>
    </div>

    <div v-if="settingsOpen" class="modal-backdrop" @click.self="closeSettings">
      <section
        ref="modalElement"
        tabindex="-1"
        class="modal settings-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
      >
        <button
          class="modal-close icon-button"
          aria-label="关闭连接设置"
          :disabled="loginBusy"
          @click="closeSettings"
        >
          <Icon name="close" /></button
        ><span class="eyebrow">YOUR SPACE, YOUR CHOICE</span>
        <h2 id="settings-title">连接你的咨询空间</h2>
        <p class="modal-intro">
          先体验页面，或使用现有 Twitter 账号获得真实答复。
        </p>
        <div class="mode-options">
          <button
            :class="{ selected: mode === 'demo' }"
            :disabled="busy || loginBusy"
            @click="workspace.changeMode('demo')"
          >
            <Icon name="sparkles" /><strong>演示模式</strong
            ><small>不用登录，体验页面交互</small
            ><Icon
              v-if="mode === 'demo'"
              name="check"
              :size="17"
              class="mode-check"
            /></button
          ><button
            :class="{ selected: mode === 'live' }"
            :disabled="busy || loginBusy"
            @click="workspace.changeMode('live')"
          >
            <Icon name="link" /><strong>真实连接</strong
            ><small>查询当前账号，结果附引用</small
            ><Icon
              v-if="mode === 'live'"
              name="check"
              :size="17"
              class="mode-check"
            />
          </button>
        </div>
        <form v-if="!account" class="login-form" @submit.prevent="signIn">
          <label for="email">账号邮箱</label
          ><input
            id="email"
            ref="loginInput"
            v-model="email"
            type="email"
            placeholder="你在 Twitter 项目中注册的邮箱"
            required
            autocomplete="username"
            :disabled="loginBusy"
          /><label for="password">密码</label
          ><input
            id="password"
            v-model="password"
            type="password"
            placeholder="输入你的账号密码"
            required
            autocomplete="current-password"
            :disabled="loginBusy"
          />
          <p v-if="loginError" class="login-error" role="alert">
            {{ loginError }}
          </p>
          <button
            class="primary-button"
            type="submit"
            :disabled="loginBusy || !email.trim() || !password"
          >
            {{ loginBusy ? "正在连接账号…" : "登录并开始真实咨询"
            }}<Icon name="arrow" :size="17" />
          </button>
          <p class="login-footnote">
            <Icon
              name="shield"
              :size="14"
            />登录信息仅用于现有账号认证，身份令牌仅保留在页面内存。
          </p>
        </form>
        <div v-else class="connected-account">
          <span class="avatar">{{
            account.username.slice(0, 1).toUpperCase()
          }}</span>
          <div>
            <strong>{{ account.username }}</strong
            ><small>{{ account.email }}</small>
          </div>
          <button
            class="text-button"
            :disabled="busy"
            @click="workspace.logout()"
          >
            <Icon name="logout" :size="16" />退出
          </button>
        </div>
        <button
          v-if="mode === 'demo'"
          class="demo-continue"
          @click="closeSettings"
        >
          继续体验演示<Icon name="chevron" :size="15" />
        </button>
      </section>
    </div>

    <div
      v-if="guideOpen"
      class="modal-backdrop"
      @click.self="guideOpen = false"
    >
      <section
        ref="modalElement"
        tabindex="-1"
        class="modal guide-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="guide-title"
      >
        <button
          class="modal-close icon-button"
          aria-label="关闭使用指南"
          @click="guideOpen = false"
        >
          <Icon name="close" /></button
        ><span class="eyebrow">A GOOD QUESTION GOES A LONG WAY</span>
        <h2 id="guide-title">不用想好类别，直接说就好。</h2>
        <div class="guide-step">
          <span>01</span>
          <div>
            <strong>说出你想确认的事情</strong>
            <p>例如：“最近发布的内容有哪些？它们是什么可见性？”</p>
          </div>
        </div>
        <div class="guide-step">
          <span>02</span>
          <div>
            <strong>看看答复里的事实与限制</strong>
            <p>
              小栖会说明已知和未知部分。证据引用可展开查看，当前不提供引用详情跳转。
            </p>
          </div>
        </div>
        <div class="guide-step">
          <span>03</span>
          <div>
            <strong>在同一段对话里继续追问</strong>
            <p>
              发送后可继续补充问题。侧栏只保留本次页面的会话，刷新后会清空。
            </p>
          </div>
        </div>
        <div class="guide-note">
          <Icon name="eye" :size="20" />
          <p>
            当前是只读咨询。内容清单能证明记录存在和接口返回的可见性，不能直接证明首页推荐情况或限制原因。
          </p>
        </div>
        <button
          class="primary-button"
          @click="
            guideOpen = false;
            focusInput();
          "
        >
          开始我的第一个问题<Icon name="arrow" :size="17" />
        </button>
      </section>
    </div>
    <div v-if="toast" class="toast" role="status">
      <Icon name="check" :size="16" />{{ toast }}
    </div>
    <SupportWidget :token="workspace.token.value" :account="account" :demo="mode === 'demo'" :assistant-url="windowOrigin" @login="settingsOpen = true" @navigate="openTweet" />
  </div>
</template>
