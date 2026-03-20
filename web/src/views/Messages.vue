<script setup lang="ts">
import { ref, onMounted, computed, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { useUserStore } from '../stores/user'
import { useMessengerStore, type Message } from '../stores/messenger'
import { getConversations, getMessages, sendMessage, type Conversation } from '../api/messenger'
import { PaperAirplaneIcon, ArrowLeftIcon, XMarkIcon } from '@heroicons/vue/24/solid'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const route = useRoute()
const userStore = useUserStore()
const messengerStore = useMessengerStore()

const conversations = ref<Conversation[]>([])
const currentMessages = ref<Message[]>([])
const activePeerId = ref<string | null>(null)
const messageInput = ref('')
const loading = ref(false)
const chatContainer = ref<HTMLElement | null>(null)

const activeConversation = computed(() => {
    return conversations.value.find(c => c.peer_id === activePeerId.value)
})

// 格式化时间
const formatTime = (timestamp: number) => {
    return dayjs(timestamp).fromNow()
}

// 加载会话列表
const loadConversations = async () => {
    try {
        const res = await getConversations({ limit: 50 })
        conversations.value = res.data.conversations || []
    } catch (e) {
        console.error('Failed to load conversations', e)
    }
}

// 加载聊天记录
const loadMessages = async (peerId: string) => {
    loading.value = true
    try {
        const res = await getMessages(peerId, { limit: 50 })
        // 消息是倒序返回的，需要翻转
        currentMessages.value = (res.data.messages || []).reverse()
        activePeerId.value = peerId
        scrollToBottom()
        
        // 标记已读逻辑... (后端 API 暂未实现)
    } catch (e) {
        console.error('Failed to load messages', e)
    } finally {
        loading.value = false
    }
}

// 发送消息
const handleSendMessage = async () => {
    if (!messageInput.value.trim() || !activePeerId.value) return
    
    try {
        const content = messageInput.value
        messageInput.value = '' // 立即清空
        
        const res = await sendMessage({
            receiver_id: activePeerId.value,
            content: content
        })
        
        // 乐观更新：收到 WebSocket 推送前先显示 (或者等待 WS 推送)
        // 这里依赖 WebSocket 推送来更新 UI，或者收到响应后手动添加
        // 为了体验更好，我们可以手动添加一个 pending 状态的消息，但为了简单，先假设 WS 很快
        // 其实 sendMessage 响应包含了 message 对象，可以直接添加
        if (res.data) {
             // 如果 WS 还没推过来，可以先 push
             const newMsg = res.data
             if (!currentMessages.value.find(m => m.id === newMsg.id)) {
                 currentMessages.value.push(newMsg)
                 scrollToBottom()
             }
        }
    } catch (e) {
        console.error('Failed to send message', e)
    }
}

const scrollToBottom = () => {
    nextTick(() => {
        if (chatContainer.value) {
            chatContainer.value.scrollTop = chatContainer.value.scrollHeight
        }
    })
}

// 监听 WebSocket 新消息
const onNewMessage = (msg: Message) => {
    console.log('New Message Received:', msg, 'ActivePeer:', activePeerId.value)
    // 强制转换为字符串比较，避免 ID 精度丢失或类型不匹配问题
    const senderId = String(msg.sender_id)
    const receiverId = String(msg.receiver_id)
    const activeId = String(activePeerId.value)

    if ((senderId === activeId || receiverId === activeId)) {
        // 只有当消息未存在时添加
        if (!currentMessages.value.find(m => String(m.id) === String(msg.id))) {
            currentMessages.value.push(msg)
            scrollToBottom()
        }
    }
    
    // 更新会话列表的最新消息
    const peerId = msg.sender_id === userStore.user?.id ? msg.receiver_id : msg.sender_id
    const convIndex = conversations.value.findIndex(c => c.peer_id === peerId)
    if (convIndex > -1) {
        const conv = conversations.value[convIndex]
        if (conv) {
            conv.latest_message = msg
            conv.unread_count = msg.sender_id !== userStore.user?.id ? (conv.unread_count + 1) : conv.unread_count
            // 移到顶部
            conversations.value.splice(convIndex, 1)
            conversations.value.unshift(conv)
        }
    } else {
        // 新会话，重新加载列表 (或者手动构造 Conversation 对象)
        loadConversations()
    }
}

onMounted(async () => {
    // 先加载会话列表
    await loadConversations()
    
    // 如果有 query 参数，自动选中
    const queryPeerId = route.query.peer_id as string
    if (queryPeerId) {
        activePeerId.value = queryPeerId
        // 如果会话列表中没有这个 peer (新会话)，我们可能需要手动构造一个临时的 conversation 或者不做处理(只显示右侧)
        // 简单处理：只选中右侧，左侧不显示高亮（直到发送第一条消息）
    }

    messengerStore.onMessage(onNewMessage)
})

// 监听 activePeerId 变化，加载消息
watch(activePeerId, (newId) => {
    if (newId) {
        loadMessages(newId)
    } else {
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
                        :disabled="!messageInput.trim()"
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
