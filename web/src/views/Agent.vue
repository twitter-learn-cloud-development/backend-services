<script setup lang="ts">
import { ref, onMounted, nextTick } from 'vue'
import { PlusIcon, ChatBubbleLeftIcon, PaperClipIcon, PaperAirplaneIcon, ArrowPathIcon, SparklesIcon } from '@heroicons/vue/24/outline'
import { getModels, getDialogues, getDialogueMessages, chat, consult, assistPublish, uploadFileAnalysis, multiAgentPublish } from '../api/agent'

const dialogues = ref<any[]>([])
const activeDialogueId = ref<string>('')
const messages = ref<any[]>([])
const models = ref<any[]>([])
const activeModelId = ref<string>('')
const inputContent = ref('')
const isLoading = ref(false)

// 发送模式
const modes = [
    { id: 'chat', name: '直接对话' },
    { id: 'consult', name: '资讯/搜索' },
    { id: 'assist', name: '辅助发推' },
    { id: 'multi', name: '智能体协作发推' }
]
const activeMode = ref('chat')

const fileInputRef = ref<HTMLInputElement | null>(null)

onMounted(async () => {
    await fetchModels()
    await fetchDialogues()
})

const fetchModels = async () => {
    try {
        const res = await getModels()
        models.value = res.data.model_kind_list || []
        if (models.value.length > 0) {
            activeModelId.value = models.value[0].id.toString()
        }
    } catch (e) {
        console.error("Failed to load models:", e)
    }
}

const fetchDialogues = async () => {
    try {
        const res = await getDialogues()
        dialogues.value = res.data.repository_dialogue_list || []
    } catch (e) {
        console.error("Failed to load dialogues:", e)
    }
}

const selectDialogue = async (id: string) => {
    if (activeDialogueId.value === id) return
    activeDialogueId.value = id
    messages.value = []
    isLoading.value = true
    try {
        const res = await getDialogueMessages(id)
        const msgs = res.data.messages || []
        // 后端返回的是 Q&A 组合，展开成独立的消息气泡
        messages.value = msgs.flatMap((m: any) => [
            { role: 'user', content: m.question },
            { role: 'assistant', content: m.response }
        ]).filter((m: any) => m.content) // 过滤掉空内容
        scrollToBottom()
    } catch (e) {
        console.error("Failed to load messages:", e)
    } finally {
        isLoading.value = false
    }
}

const createNewDialogue = () => {
    activeDialogueId.value = ''
    messages.value = []
}

const handleFileUpload = async (event: Event) => {
    const target = event.target as HTMLInputElement
    if (!target.files || target.files.length === 0) return
    const file = target.files[0]
    if (!file) return
    
    // 假设当前激活模型的第一个文件类型支持
    const currentModel = models.value.find(m => m.id.toString() === activeModelId.value)
    if (!currentModel || !currentModel.file_kind_list || currentModel.file_kind_list.length === 0) {
        alert("当前模型不支持文件解析")
        return
    }
    const fileKindId = currentModel.file_kind_list[0].id.toString()
    
    isLoading.value = true
    try {
        const res = await uploadFileAnalysis(file, fileKindId)
        alert("文件解析成功！FileKey: " + res.data.file_key)
        // 解析成功后，后端可能会把它加入特定对话或开启新对话，这里需要重新拉取
        await fetchDialogues()
        if (res.data.file_key) {
           await selectDialogue(res.data.file_key)
        }
    } catch (e) {
        console.error("Upload failed", e)
        alert("文件上传失败")
    } finally {
        isLoading.value = false
        if (fileInputRef.value) fileInputRef.value.value = ''
    }
}

const triggerFileUpload = () => {
    fileInputRef.value?.click()
}

const sendMessage = async () => {
    if (!inputContent.value.trim() || isLoading.value) return
    
    const content = inputContent.value
    inputContent.value = ''
    
    // 乐观追加用户消息
    messages.value.push({ role: 'user', content })
    scrollToBottom()
    
    isLoading.value = true
    try {
        const reqData = {
            content,
            dialogue_id: activeDialogueId.value || '0',
            model_kind_id: activeModelId.value || '0'
        }
        
        let res
        if (activeMode.value === 'chat') {
            res = await chat(reqData)
            messages.value.push({ role: 'assistant', content: res.data.response })
        } else if (activeMode.value === 'consult') {
            res = await consult(reqData)
            let answer = res.data.response + '\n\n'
            if (res.data.tweet_list && res.data.tweet_list.length > 0) {
                answer += '**推荐推文:**\n'
                res.data.tweet_list.forEach((t: any, i: number) => {
                    answer += `${i+1}. [${t.summary}](${t.url})\n`
                })
            }
            messages.value.push({ role: 'assistant', content: answer })
        } else if (activeMode.value === 'assist') {
            res = await assistPublish(reqData)
            let answer = res.data.response + '\n\n'
            if (res.data.tweet_list && res.data.tweet_list.length > 0) {
                answer += '**草稿候选:**\n'
                res.data.tweet_list.forEach((t: any, i: number) => {
                    answer += `${i+1}. ${t.content}\n`
                })
            }
            messages.value.push({ role: 'assistant', content: answer })
        } else if (activeMode.value === 'multi') {
            res = await multiAgentPublish({
                domain: "general", // 这里简化，实际可做弹窗
                author_user_id: "0", 
                style_ratio: 0.5,
                reference_tweet_ids: [],
                content: content
            })
            messages.value.push({ role: 'assistant', content: res.data.response })
        }
        
        scrollToBottom()
        // 发送完刷新会话列表
        await fetchDialogues()
        // 自动选中最新产生的对话 ID（如果新建）
        // 如果当前 activeDialogueId 为空或者为 0，就取拉回来的第一条（最新产生的）作为当前的对话
        if (!activeDialogueId.value || activeDialogueId.value === '0') {
            if (dialogues.value.length > 0) {
                activeDialogueId.value = dialogues.value[0].id.toString()
            }
        }
        
    } catch (e) {
        console.error("Send failed", e)
        messages.value.push({ role: 'assistant', content: "❌ 请求失败，请重试。" })
    } finally {
        isLoading.value = false
    }
}

const chatContainerRef = ref<HTMLElement | null>(null)
const scrollToBottom = () => {
    nextTick(() => {
        if (chatContainerRef.value) {
            chatContainerRef.value.scrollTop = chatContainerRef.value.scrollHeight
        }
    })
}
</script>

<template>
  <div class="flex h-screen bg-gray-50 dark:bg-black w-full text-gray-900 dark:text-white">
    
    <!-- 左侧会话列表 -->
    <div class="w-64 bg-white dark:bg-gray-900 border-r border-gray-200 dark:border-gray-800 flex flex-col hidden md:flex">
      <div class="p-4">
        <button 
            @click="createNewDialogue"
            class="w-full flex items-center justify-center space-x-2 bg-blue-50 text-primary hover:bg-blue-100 dark:bg-gray-800 dark:text-blue-400 dark:hover:bg-gray-700 py-2.5 rounded-xl transition-colors font-medium">
          <PlusIcon class="w-5 h-5" />
          <span>新建对话</span>
        </button>
      </div>
      
      <div class="flex-1 overflow-y-auto px-2 space-y-1">
        <div 
            v-for="dialogue in dialogues" :key="dialogue.id"
            @click="selectDialogue(dialogue.id.toString())"
            :class="[
                'flex items-center space-x-3 p-3 rounded-lg cursor-pointer transition-colors',
                activeDialogueId === dialogue.id.toString() ? 'bg-primary text-white' : 'hover:bg-gray-100 dark:hover:bg-gray-800'
            ]"
        >
            <ChatBubbleLeftIcon class="w-5 h-5 flex-shrink-0" />
            <span class="truncate text-sm font-medium">{{ dialogue.title || '新对话' }}</span>
        </div>
      </div>
    </div>

    <!-- 右侧主聊天区 -->
    <div class="flex-1 flex flex-col h-full bg-white dark:bg-black relative">
        
      <!-- 顶栏配置区 -->
      <div class="h-16 border-b border-gray-200 dark:border-gray-800 flex items-center justify-between px-6 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 absolute top-0 w-full">
         <div class="flex items-center space-x-4">
            <span class="font-bold text-lg">AI 助手</span>
            <select v-model="activeModelId" class="bg-gray-100 dark:bg-gray-800 border-none rounded-lg text-sm px-3 py-1.5 outline-none cursor-pointer">
                <option v-for="model in models" :key="model.id" :value="model.id.toString()">
                    {{ model.name }}
                </option>
            </select>
         </div>
      </div>

      <!-- 消息列表 -->
      <div class="flex-1 overflow-y-auto p-6 pt-24 pb-32 space-y-6" ref="chatContainerRef">
        
        <div v-if="messages.length === 0" class="flex flex-col items-center justify-center h-full text-gray-400 space-y-4">
            <SparklesIcon class="w-16 h-16 opacity-50" />
            <p class="text-lg">在下方输入内容，开始与 AI 交流</p>
        </div>

        <div v-for="(msg, index) in messages" :key="index" class="flex w-full" :class="msg.role === 'user' ? 'justify-end' : 'justify-start'">
            <div 
                class="max-w-[80%] rounded-2xl px-5 py-3 shadow-sm whitespace-pre-wrap"
                :class="msg.role === 'user' ? 'bg-primary text-white rounded-br-none' : 'bg-gray-100 dark:bg-gray-800 rounded-bl-none'"
            >
                {{ msg.content }}
            </div>
        </div>

        <div v-if="isLoading && (!messages.length || messages[messages.length-1].role === 'user')" class="flex w-full justify-start">
             <div class="bg-gray-100 dark:bg-gray-800 rounded-2xl rounded-bl-none px-5 py-4 flex space-x-2 items-center">
                 <div class="w-2 h-2 bg-gray-400 rounded-full animate-bounce"></div>
                 <div class="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style="animation-delay: 0.2s"></div>
                 <div class="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style="animation-delay: 0.4s"></div>
             </div>
        </div>

      </div>

      <!-- 底部输入区 -->
      <div class="absolute bottom-0 w-full bg-gradient-to-t from-white via-white to-transparent dark:from-black dark:via-black pt-10 pb-6 px-6 border-t border-gray-100 dark:border-gray-800">
         <div class="max-w-4xl mx-auto flex flex-col space-y-3 relative">
            
            <!-- 模式切换 -->
            <div class="flex space-x-2">
                <button 
                    v-for="mode in modes" :key="mode.id"
                    @click="activeMode = mode.id"
                    :class="[
                        'px-3 py-1 text-xs font-medium rounded-full transition-colors',
                        activeMode === mode.id ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-black' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700'
                    ]"
                >
                    {{ mode.name }}
                </button>
            </div>

            <div class="relative bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 rounded-2xl shadow-sm focus-within:ring-2 focus-within:ring-primary focus-within:border-primary transition-all flex items-end">
                
                <input type="file" ref="fileInputRef" class="hidden" @change="handleFileUpload" />
                <button @click="triggerFileUpload" class="p-3 text-gray-400 hover:text-primary transition-colors" title="解析文件">
                    <PaperClipIcon class="w-6 h-6" />
                </button>

                <textarea 
                    v-model="inputContent"
                    @keydown.enter.exact.prevent="sendMessage"
                    placeholder="输入消息，Enter 发送，Shift+Enter 换行"
                    class="flex-1 max-h-48 min-h-[52px] bg-transparent border-none focus:ring-0 resize-none py-3 px-2 text-gray-900 dark:text-white"
                    rows="1"
                ></textarea>

                <button 
                    @click="sendMessage"
                    :disabled="!inputContent.trim() || isLoading"
                    class="p-3 text-white m-1.5 rounded-xl transition-colors"
                    :class="inputContent.trim() && !isLoading ? 'bg-primary hover:bg-blue-600' : 'bg-gray-300 dark:bg-gray-700 cursor-not-allowed'"
                >
                    <ArrowPathIcon v-if="isLoading" class="w-5 h-5 animate-spin" />
                    <PaperAirplaneIcon v-else class="w-5 h-5" />
                </button>
            </div>
            
            <div class="text-center text-xs text-gray-400">
                AI Agent 可能会犯错。请核查重要信息。
            </div>
         </div>
      </div>

    </div>
  </div>
</template>
