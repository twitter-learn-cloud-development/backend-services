<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { isTweetCitation, tweetHref } from './api'
import { hasSiteAiHandoff, siteAiHref } from './handoff'
import type { Account, SupportCitation } from './types'
import { useWorkspace } from './useWorkspace'

const workspace = useWorkspace()
const { messages, draft, busy, account, token, mode } = workspace
const feed = ref<HTMLElement>()
const composer = ref<HTMLTextAreaElement>()
const configuredOrigin = new URL(import.meta.env.VITE_TWITTER_WEB_URL || 'http://127.0.0.1:5173').origin
const allowedOrigins = new Set([configuredOrigin, location.origin])
if (configuredOrigin === 'http://127.0.0.1:5173') allowedOrigins.add('http://localhost:5173')
let parentOrigin: string | undefined
const demo = new URLSearchParams(location.search).get('demo') === '1'
if (!demo) workspace.connectSession('', null)
const caption = computed(() => demo ? '界面演示 · 不执行实际任务' : account.value ? `已连接 @${account.value.username}` : '登录后咨询功能和操作')
const statusLabels = { answered: '已答复', partial: '部分结果', clarification_required: '需要补充信息', unsupported: '当前能力不支持' }

function post(type: string, detail: Record<string, unknown> = {}) {
  if (window.parent === window) return
  if (parentOrigin) window.parent.postMessage({ type, ...detail }, parentOrigin)
  else for (const origin of allowedOrigins) window.parent.postMessage({ type, ...detail }, origin)
}
function receive(event: MessageEvent) {
  if (event.source !== window.parent || !allowedOrigins.has(event.origin) || !event.data || event.data.type !== 'support:session') return
  const data = event.data
  if (typeof data.token !== 'string' || data.token.length > 20000) return
  let profile: Account | null = null
  if (data.token) {
    const value = data.account
    if (!value || typeof value.id !== 'string' || typeof value.username !== 'string' || typeof value.email !== 'string') {
      if (!demo) workspace.connectSession('', null)
      return
    }
    profile = { id: value.id, username: value.username, email: value.email }
  }
  parentOrigin = event.origin
  if (!demo) workspace.connectSession(data.token, profile)
}
function openCitation(event: MouseEvent, citation: SupportCitation) {
  if (!parentOrigin || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return
  if (!isTweetCitation(citation)) return
  event.preventDefault()
  post('support:navigate', { path: citation.path })
}
async function send() {
  if (!demo && !token.value) { post('support:login'); return }
  await workspace.send()
  composer.value?.focus()
}
function onKey(event: KeyboardEvent) {
  if (event.key === 'Escape' && !event.isComposing) { post('support:minimize'); return }
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && event.keyCode !== 229) { event.preventDefault(); void send() }
}
watch(() => messages.value.length, async () => { await nextTick(); feed.value?.scrollTo({ top: feed.value.scrollHeight, behavior: 'smooth' }) })
onMounted(() => { window.addEventListener('message', receive); post('support:ready') })
onBeforeUnmount(() => { window.removeEventListener('message', receive); workspace.stop() })
</script>

<template>
  <section class="support-mini" aria-label="小栖客服">
    <header class="mini-header">
      <div class="mini-avatar" aria-hidden="true">栖</div>
      <div><strong>小栖 · 你的使用向导</strong><p>{{ caption }}</p></div>
      <button aria-label="新建客服对话" title="新建对话" :disabled="busy" @click="workspace.newConversation">＋</button>
      <button aria-label="收起客服" title="收起" @click="post('support:minimize')">−</button>
    </header>
    <div ref="feed" class="mini-feed" role="log" aria-live="polite" aria-label="客服消息">
      <div v-if="!messages.length" class="mini-welcome">
        <span class="mini-eyebrow">一点帮助，随时在这里</span>
        <h1>你好，我是小栖。</h1><p>我帮你了解功能、规则和操作方法。<br>内容查询、分析和创作，请交给站内 AI 助手。</p>
        <button v-for="prompt in ['Workflow 是什么，如何创建和运行？', '站内 AI 助手在哪里，如何使用？', '保存工作流和发布给 Agent 有什么区别？']" :key="prompt" @click="workspace.choosePrompt(prompt); composer?.focus()">{{ prompt }} <span aria-hidden="true">↗</span></button>
        <small>小栖只做使用向导，不执行站内任务。</small>
      </div>
      <article v-for="message in messages" :key="message.id" :class="['mini-message', message.role]">
        <small>{{ message.role === 'user' ? '你' : message.role === 'error' ? '连接提示' : '小栖' }}<template v-if="message.status"> · {{ hasSiteAiHandoff(message.response) ? '可前往站内 AI 助手' : statusLabels[message.status] }}</template></small>
        <p>{{ message.text }}</p>
        <nav v-if="hasSiteAiHandoff(message.response)" class="mini-citations" aria-label="继续咨询站内 AI 助手">
          <a :href="siteAiHref(parentOrigin || configuredOrigin)" target="_blank" rel="noopener noreferrer">前往站内 AI 助手 ↗</a>
          <span>由你选择继续咨询，不会自动发送当前问题。</span>
        </nav>
        <nav v-if="message.response?.citations?.length" class="mini-citations" aria-label="本轮查询的推文引用">
          <span>本轮可核实的推文 · 点击查看</span>
          <a v-for="citation in message.response.citations" :key="citation.id" :href="tweetHref(citation)" target="_blank" rel="noopener noreferrer" @click="openCitation($event, citation)">{{ citation.label }} ↗</a>
        </nav>
        <button v-if="message.attempt" :disabled="busy" @click="workspace.retry(message)">重试同一次请求</button>
        <details v-if="message.response"><summary>查询依据（{{ message.response.evidence_refs.length }}）</summary><p v-for="ref in message.response.evidence_refs" :key="ref" class="mini-evidence">{{ ref }}</p></details>
      </article>
      <p v-if="busy" class="mini-wait" role="status">小栖正在核实信息… <button @click="workspace.stop">停止等待</button></p>
    </div>
    <footer class="mini-footer">
      <button v-if="mode === 'live' && !token" class="mini-login" @click="post('support:login')">登录现有账号后咨询</button>
      <form @submit.prevent="send">
        <textarea ref="composer" v-model="draft" aria-label="客服问题" placeholder="有什么可以帮你？" rows="2" maxlength="8000" :disabled="busy" @keydown="onKey" />
        <button type="submit" aria-label="发送客服问题" :disabled="busy || !draft.trim()">↑</button>
      </form>
      <small>{{ demo ? '界面示例，不执行任务' : '小栖依据已发布指南解释使用方法，不查询或分析站内内容' }}</small>
    </footer>
  </section>
</template>

<style>
body:has(.support-mini) { margin: 0; background: #f8f7f3; }
.support-mini { height: 100dvh; display: flex; flex-direction: column; background: #f8f7f3; color: #233d41; font: 14px/1.65 system-ui, sans-serif; }
.support-mini button, .support-mini textarea { font: inherit; }
.support-mini button { cursor: pointer; }
.support-mini button:disabled { opacity: .45; cursor: default; }
.support-mini button:focus-visible, .support-mini a:focus-visible, .support-mini textarea:focus-visible { outline: 2px solid #ad573e; outline-offset: 3px; }
.mini-header { display: flex; gap: 10px; align-items: center; padding: 17px; border-bottom: 1px solid #deded5; background: #fffdf8; }
.mini-header strong { font-size: 14px; }.mini-header p { margin: 0; font-size: 11px; color: #6d7a77; }.mini-header > div:nth-child(2) { flex: 1; }.mini-header button { border: 0; background: transparent; font-size: 22px; width: 30px; color: #233d41; }.mini-avatar { width: 38px; height: 38px; border-radius: 13px; background: #234a4a; color: #fff; display: grid; place-items: center; }
.mini-feed { flex: 1; overflow-y: auto; padding: 20px 17px; overscroll-behavior: contain; }.mini-welcome { padding: 15px 0; }.mini-eyebrow { color: #ad573e; font-size: 11px; letter-spacing: 1px; }.mini-welcome h1 { font-size: 25px; font-weight: 550; margin: 14px 0 10px; }.mini-welcome p { color: #697975; margin-bottom: 22px; }.mini-welcome > button { display: flex; justify-content: space-between; gap: 10px; width: 100%; border: 1px solid #deded5; background: #fffdf8; color: #233d41; text-align: left; border-radius: 11px; margin: 8px 0; padding: 12px; font-size: 12px; }.mini-welcome > small { display: block; font-size: 11px; color: #7b8178; margin-top: 18px; }
.mini-message { margin: 0 0 18px; }.mini-message > small { font-size: 10px; color: #73817b; }.mini-message > p { white-space: pre-wrap; overflow-wrap: anywhere; padding: 13px; margin: 6px 0; background: #fffdf8; border: 1px solid #e4e4da; border-radius: 2px 13px 13px; }.mini-message.user > p { background: #e4ede6; border-color: #d7e2d8; }.mini-message.error > p { background: #fff2e9; }.mini-message > button, .mini-wait button { border: 0; background: transparent; color: #a34f36; text-decoration: underline; }.mini-message details { font-size: 10px; color: #697975; }.mini-evidence { overflow-wrap: anywhere; }.mini-citations { display: grid; gap: 5px; margin: 9px 0; }.mini-citations span { font-size: 10px; color: #697975; }.mini-citations a { font-size: 11px; color: #266559; overflow-wrap: anywhere; }.mini-wait { color: #697975; font-size: 12px; }
.mini-footer { padding: 12px 15px; background: #fffdf8; border-top: 1px solid #deded5; }.mini-footer form { display: flex; align-items: end; border: 1px solid #d4d9ce; border-radius: 13px; padding: 8px; }.mini-footer textarea { border: 0; background: transparent; color: #233d41; width: 100%; resize: none; outline: none; padding: 2px 4px; font-size: 13px; }.mini-footer form button { border: 0; background: #234a4a; color: white; border-radius: 9px; width: 34px; height: 34px; flex-shrink: 0; font-size: 20px; }.mini-footer > small { display: block; font-size: 9px; text-align: center; color: #7b8178; margin-top: 7px; }.mini-login { width: 100%; border: 0; background: #e4ede6; color: #234a4a; padding: 6px; margin-bottom: 10px; border-radius: 8px; }
</style>
