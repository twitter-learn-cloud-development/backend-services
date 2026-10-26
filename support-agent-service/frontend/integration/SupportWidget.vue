<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  token?: string
  account?: { id: string; username: string; email: string } | null
  demo?: boolean
  assistantUrl?: string
}>(), { token: '', account: null, demo: false, assistantUrl: 'http://127.0.0.1:5174/' })
const emit = defineEmits<{ navigate: [path: string]; login: [] }>()
const open = ref(false)
const initialized = ref(false)
const frame = ref<HTMLIFrameElement>()
const launcher = ref<HTMLButtonElement>()
const source = computed(() => {
  const url = new URL(props.assistantUrl)
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('Invalid assistant URL')
  url.search = ''
  url.hash = ''
  url.searchParams.set('embed', '1')
  if (props.demo) url.searchParams.set('demo', '1')
  return url.href
})
const origin = computed(() => new URL(source.value).origin)
function session() {
  const complete = Boolean(props.token && props.account?.id && props.account.username && typeof props.account.email === 'string')
  const account = complete && props.account ? { id: props.account.id, username: props.account.username, email: props.account.email } : null
  frame.value?.contentWindow?.postMessage({ type: 'support:session', token: complete ? props.token : '', account }, origin.value)
}
function close() { open.value = false; void nextTick(() => launcher.value?.focus()) }
function receive(event: MessageEvent) {
  if (event.source !== frame.value?.contentWindow || event.origin !== origin.value || !event.data) return
  switch (event.data.type) {
    case 'support:ready': session(); break
    case 'support:minimize': close(); break
    case 'support:login': emit('login'); break
    case 'support:navigate':
      if (typeof event.data.path === 'string' && /^\/tweets\/[0-9]+$/.test(event.data.path)) emit('navigate', event.data.path)
      break
  }
}
function toggle() { initialized.value = true; open.value = !open.value }
function escape(event: KeyboardEvent) { if (event.key === 'Escape' && open.value) close() }
watch(() => [props.token, props.account?.id, props.account?.username, props.account?.email], session)
onMounted(() => { window.addEventListener('message', receive); window.addEventListener('keydown', escape) })
onBeforeUnmount(() => { window.removeEventListener('message', receive); window.removeEventListener('keydown', escape) })
</script>

<template>
  <aside class="xiaoqi-host" aria-label="全站客服入口">
    <section v-if="initialized" v-show="open" id="xiaoqi-panel" class="xiaoqi-panel" aria-label="小栖客服面板" :inert="!open">
      <iframe ref="frame" :src="source" title="小栖客服对话" @load="session" />
    </section>
    <button ref="launcher" class="xiaoqi-launcher" aria-controls="xiaoqi-panel" :aria-expanded="open" :aria-label="open ? '收起小栖客服' : '打开小栖客服'" @click="toggle"><span aria-hidden="true">{{ open ? '−' : '栖' }}</span><span>{{ open ? '收起' : '问问小栖' }}</span></button>
  </aside>
</template>

<style scoped>
.xiaoqi-host { position: fixed; right: max(24px, env(safe-area-inset-right)); bottom: max(22px, env(safe-area-inset-bottom)); z-index: 1100; font-family: system-ui, sans-serif; }
.xiaoqi-launcher { display: flex; align-items: center; justify-content: center; gap: 9px; margin-left: auto; padding: 10px 16px 10px 10px; border: 1px solid #56726b; border-radius: 30px; background: #234a4a; color: #fffdf8; box-shadow: 0 8px 25px #173e3a26; cursor: pointer; font-size: 13px; }
.xiaoqi-launcher > span:first-child { display: grid; place-items: center; width: 29px; height: 29px; border-radius: 50%; background: #fffdf820; font-size: 16px; }.xiaoqi-launcher:focus-visible { outline: 3px solid #bf7457; outline-offset: 4px; }
.xiaoqi-panel { width: 410px; height: min(650px, calc(100dvh - 110px)); margin-bottom: 12px; border-radius: 20px; overflow: hidden; border: 1px solid #d5dbcf; box-shadow: 0 18px 70px #173e3a30; background: #f8f7f3; }.xiaoqi-panel iframe { display: block; width: 100%; height: 100%; border: 0; }
@media (max-width: 520px) { .xiaoqi-host { right: 12px; bottom: max(14px, env(safe-area-inset-bottom)); }.xiaoqi-panel { width: calc(100vw - 24px); height: min(620px, calc(100dvh - 110px)); }.xiaoqi-launcher { padding: 8px 13px 8px 8px; } }
</style>
