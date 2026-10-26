<script setup lang="ts">
import { ref, shallowRef, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { useUserStore } from '../stores/user'
import { useMessengerStore, type Message } from '../stores/messenger'
import { getConversations, getMessages, sendMessage, type Conversation } from '../api/messenger'
import { PaperAirplaneIcon, ArrowLeftIcon, XMarkIcon } from '@heroicons/vue/24/solid'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'
import { MessageHistory, compareIDs } from '../utils/messageHistory'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const route = useRoute()
const userStore = useUserStore()
const messengerStore = useMessengerStore()

const conversations = ref<Conversation[]>([])
const currentMessages = shallowRef<Message[]>([])
const activePeerId = ref<string | null>(null)
const messageInput = ref('')
const loading = ref(false)
const historyState = ref({ more: true, syncing: false, continuation: false, error: '' })
const sending = ref(false), sendError = ref(''), listError = ref(''), listLoading = ref(false), listMore = ref(false)
let listCursor = '0', listEpoch = 0, disposed = false
let refreshAfterList = false
let history: MessageHistory<Message> | null = null
const loadOlder = async () => {
    const selected = history, height = chatContainer.value?.scrollHeight || 0, top = chatContainer.value?.scrollTop || 0
    await selected?.older(); await nextTick()
    if (!disposed && selected === history && chatContainer.value) chatContainer.value.scrollTop = top + chatContainer.value.scrollHeight - height
}
const recoverMessages = () => history?.recover()
const chatContainer = ref<HTMLElement | null>(null)

const activeConversation = computed(() => {
    return conversations.value.find(c => c.peer_id === activePeerId.value)
})

// 格式化时间
const formatTime = (timestamp: number) => {
    return dayjs(timestamp).fromNow()
}

// 加载会话列表
const loadConversations = async (more = false) => {
    if (disposed || (more && !listMore.value)) return
    if (listLoading.value) { if (!more) refreshAfterList = true; return }
    const epoch = more ? listEpoch : ++listEpoch, cursor = more ? listCursor : '0'
    listLoading.value = true; listError.value = ''
    try {
        const res = await getConversations({ limit: 50, cursor })
        if (disposed || epoch !== listEpoch) return
        const page = res.data
        if (page.has_more && (!page.next_cursor || page.next_cursor === cursor)) throw new Error('cursor')
        const rows = new Map((more ? conversations.value : []).map(c => [c.peer_id, c]))
        for (const conv of page.conversations || []) rows.set(conv.peer_id, conv)
        conversations.value = [...rows.values()].sort((a, b) => compareIDs(b.latest_message.id, a.latest_message.id))
        listCursor = page.next_cursor || '0'; listMore.value = page.has_more
    } catch { if (!disposed && epoch === listEpoch) listError.value = '会话加载失败，请重试' }
    finally {
        if (!disposed && epoch === listEpoch) {
            listLoading.value = false
            if (refreshAfterList) { refreshAfterList = false; void loadConversations() }
        }
    }
}

// 加载聊天记录
const loadMessages = async (peerId: string) => {
    history?.dispose(); currentMessages.value = []; sendError.value = ''; messageInput.value = ''
    const userID = String(userStore.user?.id || '')
    const selected = new MessageHistory<Message>(async cursor => (await getMessages(peerId, { limit: 50, cursor })).data,
        row => (row.sender_id === userID && row.receiver_id === peerId) || (row.sender_id === peerId && row.receiver_id === userID),
        () => {
            if (history !== selected || disposed) return
            currentMessages.value = [...selected.items]; loading.value = selected.busy
            historyState.value = { more: selected.hasMore, syncing: selected.syncing, continuation: selected.continuation, error: selected.error }
        })
    history = selected
    await selected.older()
    if (!disposed && history === selected) { scrollToBottom(); void loadConversations() }
}

// 发送消息
const handleSendMessage = async () => {
    if (!messageInput.value.trim() || !activePeerId.value || sending.value) return
    const selected = history, peer = activePeerId.value, epoch = userStore.revision, content = messageInput.value
    sending.value = true; sendError.value = ''
    try {
        const res = await sendMessage({
            receiver_id: peer,
            content: content
        })
        
        if (disposed || selected !== history || !userStore.isCurrent(epoch)) return
        selected?.push(res.data)
        if (messageInput.value === content) messageInput.value = ''
        scrollToBottom(); void loadConversations()
    } catch { if (!disposed && selected === history && userStore.isCurrent(epoch)) sendError.value = '发送结果未确定，草稿已保留。先同步历史再决定是否重发，重发可能重复。' }
    finally { if (!disposed) sending.value = false }
}

const scrollToBottom = () => {
    nextTick(() => {
        if (!disposed && chatContainer.value) {
            chatContainer.value.scrollTop = chatContainer.value.scrollHeight
        }
    })
}

// 监听 WebSocket 新消息
const onNewMessage = (msg: Message) => {
    if (disposed) return
    if (msg.sender_id !== String(userStore.user?.id) && msg.receiver_id !== String(userStore.user?.id)) return
    history?.push(msg)
    scheduleSync()
}
let syncTimer: ReturnType<typeof setTimeout> | null = null
const sync = () => { if (!disposed && document.visibilityState === 'visible') { void history?.recover(); void loadConversations() } }
const scheduleSync = () => { if (syncTimer) return; syncTimer = setTimeout(() => { syncTimer = null; sync() }, 250) }
let pollTimer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
    messengerStore.onMessage(onNewMessage)
    messengerStore.onReady(scheduleSync)
    messengerStore.connect()
    document.addEventListener('visibilitychange', sync)
    pollTimer = setInterval(sync, 30000)
    // 先加载会话列表
    void loadConversations()
    
    // 如果有 query 参数，自动选中
    const queryPeerId = route.query.peer_id as string
    if (queryPeerId) {
        activePeerId.value = queryPeerId
        // 如果会话列表中没有这个 peer (新会话)，我们可能需要手动构造一个临时的 conversation 或者不做处理(只显示右侧)
        // 简单处理：只选中右侧，左侧不显示高亮（直到发送第一条消息）
    }

})

onUnmounted(() => {
    disposed = true; ++listEpoch; history?.dispose()
    if (syncTimer) clearTimeout(syncTimer)
    if (pollTimer) clearInterval(pollTimer)
    document.removeEventListener('visibilitychange', sync)
    messengerStore.offReady(scheduleSync)
    messengerStore.offMessage(onNewMessage)
})

// 监听 activePeerId 变化，加载消息
watch(activePeerId, (newId) => {
    if (newId) {
        loadMessages(newId)
    } else {
        history?.dispose(); history = null
        currentMessages.value = []
    }
})

</script>

<template>
<div class="flex h-screen border-r border-gray-100 dark:border-gray-800">
    <!-- 会话列表 (左侧) -->
    <div class="w-full md:w-1/3 flex flex-col border-r border-gray-100 dark:border-gray-800" :class="{'hidden md:flex': activePeerId}">
        <div class="p-4 border-b border-gray-100 dark:border-gray-800 font-bold text-xl">
            私信
        </div>
        <div class="flex-1 overflow-y-auto">
            <p v-if="listError" role="alert" class="p-3">{{ listError }} <button @click="loadConversations()">重试</button></p>
            <div 
                v-for="conv in conversations" 
                :key="conv.peer_id"
                @click="activePeerId = conv.peer_id"
                class="p-4 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors border-b border-gray-50 dark:border-gray-800"
                :class="{'bg-blue-50 dark:bg-gray-800': activePeerId === conv.peer_id}"
            >
                <div class="flex items-center space-x-3">
                    <img 
                        :src="conv.peer?.avatar || 'https://www.gravatar.com/avatar/00000000000000000000000000000000?d=mp&f=y'" 
                        class="w-12 h-12 rounded-full object-cover flex-shrink-0 bg-gray-200 dark:bg-gray-700"
                        alt="Avatar"
                    />
                    <div class="flex-1 min-w-0">
                        <div class="flex justify-between items-baseline">
                            <h3 class="font-bold truncate text-gray-900 dark:text-gray-100">
                                {{ conv.peer?.username || `用户 ${conv.peer_id}` }}
                            </h3>
                            <span class="text-xs text-gray-500" v-if="conv.latest_message">{{ formatTime(conv.latest_message.created_at) }}</span>
                        </div>
                        <p class="text-sm text-gray-500 truncate" v-if="conv.latest_message">
                            {{ conv.latest_message.content }}
                        </p>
                    </div>
                    <div v-if="conv.unread_count > 0" class="w-2 h-2 bg-primary rounded-full"></div>
                </div>
            </div>
             <div v-if="conversations.length === 0" class="p-8 text-center text-gray-500">
                暂无会话
            </div>
            <button v-if="listMore" :disabled="listLoading" @click="loadConversations(true)" class="p-4 w-full">{{ listLoading ? '加载中…' : '加载更多会话' }}</button>
        </div>
    </div>

    <!-- 聊天窗口 (右侧) -->
    <div class="w-full md:w-2/3 flex flex-col h-full bg-white dark:bg-black" :class="{'hidden md:flex': !activePeerId}">
        <div v-if="activePeerId" class="flex-1 flex flex-col h-full">
            <!-- Header -->
            <div class="p-4 border-b border-gray-100 dark:border-gray-800 flex items-center space-x-4 sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10">
                <button @click="activePeerId = null" class="md:hidden p-2 -ml-2 rounded-full hover:bg-gray-100 dark:hover:bg-gray-900">
                    <ArrowLeftIcon class="w-5 h-5" />
                </button>
                <div class="font-bold text-lg flex-1">
                    {{ activeConversation?.peer?.username || `用户 ${activePeerId}` }}
                </div>
                <button @click="activePeerId = null" title="关闭会话" class="p-2 -mr-2 rounded-full hover:bg-gray-100 dark:hover:bg-gray-900 text-gray-500">
                    <XMarkIcon class="w-5 h-5" />
                </button>
            </div>

            <!-- Messages -->
            <div class="flex-1 overflow-y-auto p-4 space-y-4" ref="chatContainer">
                <p v-if="historyState.error" role="alert">{{ historyState.error }}</p>
                <button v-if="historyState.more" :disabled="loading || historyState.syncing" @click="loadOlder">{{ loading ? '加载中…' : '加载更早消息' }}</button>
                <button :disabled="loading || historyState.syncing" @click="recoverMessages">{{ historyState.syncing ? '同步中…' : historyState.continuation ? '继续同步未读区间' : '同步最新消息' }}</button>
                <div 
                    v-for="msg in currentMessages" 
                    :key="msg.id" 
                    class="flex items-end space-x-2"
                    :class="msg.sender_id === userStore.user?.id ? 'justify-end' : 'justify-start'"
                >
                    <!-- 对方头像 -->
                    <img 
                        v-if="msg.sender_id !== userStore.user?.id"
                        :src="(msg as any).sender?.avatar || 'https://www.gravatar.com/avatar/00000000000000000000000000000000?d=mp&f=y'" 
                        class="w-8 h-8 rounded-full object-cover mb-1 bg-gray-200 dark:bg-gray-700"
                        alt="Avatar"
                    />

                    <div 
                        class="max-w-[70%] px-4 py-2 rounded-2xl break-words"
                        :class="msg.sender_id === userStore.user?.id 
                            ? 'bg-primary text-white rounded-br-none' 
                            : 'bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-bl-none'"
                    >
                        {{ msg.content }}
                    </div>
                </div>
            </div>

            <!-- Input -->
            <div class="p-4 border-t border-gray-100 dark:border-gray-800 sticky bottom-0 bg-white dark:bg-black">
                <p v-if="sendError" role="alert">{{ sendError }}</p>
                <div class="relative flex items-center bg-gray-100 dark:bg-gray-900 rounded-full px-4 py-1">
                    <input 
                        v-model="messageInput"
                        @keyup.enter="handleSendMessage"
                        type="text" 
                        placeholder="发送私信..." 
                        class="flex-1 bg-transparent py-3 outline-none text-gray-900 dark:text-gray-100"
                    />
                    <button 
                        @click="handleSendMessage"
                        :disabled="!messageInput.trim() || sending"
                        class="p-2 text-primary hover:bg-blue-50 dark:hover:bg-gray-800 rounded-full transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        <PaperAirplaneIcon class="w-5 h-5 -rotate-90" />
                    </button>
                </div>
            </div>
        </div>
        
        <div v-else class="flex-1 flex items-center justify-center text-gray-500">
            <div>
                <h2 class="text-2xl font-bold mb-2">选择一条私信</h2>
                <p>从左侧列表中选择一个对话开始聊天</p>
                <div class="mt-4">
                     <!-- 可以在这里加一个新建私信的按钮 -->
                </div>
            </div>
        </div>
    </div>
</div>
</template>
