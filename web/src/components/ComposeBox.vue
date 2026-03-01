<script setup lang="ts">
import { ref } from 'vue'
import { PhotoIcon, ChartBarIcon, XMarkIcon, PlusIcon } from '@heroicons/vue/24/outline'
import { uploadMedia } from '../api/upload'
import { createTweet } from '../api/tweet'
import { useUserStore } from '../stores/user'

const userStore = useUserStore()
const emit = defineEmits(['tweet-created'])

const content = ref('')
const loading = ref(false)
const selectedFiles = ref<File[]>([])
const previewUrls = ref<{ url: string, type: 'image' | 'video' }[]>([])
const fileInput = ref<HTMLInputElement | null>(null)

// 投票相关
const showPoll = ref(false)
const pollOptions = ref<string[]>(['', ''])
const pollDuration = ref(1440) // 默认 24小时 (分钟)

const durationOptions = [
    { label: '15分钟', value: 15 },
    { label: '1小时', value: 60 },
    { label: '24小时', value: 1440 },
    { label: '3天', value: 4320 },
    { label: '7天', value: 10080 },
]

const addPollOption = () => {
    if (pollOptions.value.length < 4) {
        pollOptions.value.push('')
    }
}

const togglePoll = () => {
    if (showPoll.value) {
        showPoll.value = false
        pollOptions.value = ['', '']
        pollDuration.value = 1440
    } else {
        showPoll.value = true
        // 如果有媒体，提示互斥
        if (selectedFiles.value.length > 0) {
            if (confirm('投票不能与媒体同时发送，是否移除媒体？')) {
                selectedFiles.value = []
                previewUrls.value.forEach(p => URL.revokeObjectURL(p.url))
                previewUrls.value = []
            } else {
                showPoll.value = false
            }
        }
    }
}

// 触发文件选择
const triggerFileInput = () => {
    fileInput.value?.click()
}

// 处理文件选择
const handleFileChange = (event: Event) => {
    const target = event.target as HTMLInputElement
    // 投票互斥检查
    if (showPoll.value) {
        alert('投票不能与媒体同时发送')
        // select files cleared?
        target.value = ''
        return
    }

    if (target.files) {
        // 追加模式，或者覆盖模式？通常是追加，但这里简化为追加
        // 检查总数限制
        const newFiles = Array.from(target.files)
        if (selectedFiles.value.length + newFiles.length > 4) {
             alert('最多上传 4 个媒体文件')
             return
        }

        for (const file of newFiles) {
             selectedFiles.value.push(file)
             previewUrls.value.push({
                 url: URL.createObjectURL(file),
                 type: file.type.startsWith('video/') ? 'video' : 'image'
             })
        }
    }
    // 重置 input 以允许重复选择同一文件
    target.value = ''
}

// 移除媒体
const removeMedia = (index: number) => {
    selectedFiles.value.splice(index, 1)
    const mediaUrl = previewUrls.value[index]?.url
    if (mediaUrl) URL.revokeObjectURL(mediaUrl) // 释放内存
    previewUrls.value.splice(index, 1)
}

// 发布推文
const handleTweet = async () => {
    if (!content.value.trim() && selectedFiles.value.length === 0) return
    
    loading.value = true
    try {
        let mediaUrls: string[] = []
        
        // 1. 上传所有媒体
        if (selectedFiles.value.length > 0) {
            // 并行上传
            const uploadPromises = selectedFiles.value.map(file => uploadMedia(file))
            const responses = await Promise.all(uploadPromises)
            
            // 收集 URL
            mediaUrls = responses.map(res => res.data.url)
        }

        // 2. 创建推文
        const tweetData: any = {
            content: content.value,
            media_urls: mediaUrls
        }
        
        if (showPoll.value) {
            // 过滤空选项
            const validOptions = pollOptions.value.filter(o => o.trim())
            if (validOptions.length < 2) {
                alert('至少需要两个投票选项')
                loading.value = false
                return
            }
            tweetData.poll_options = validOptions
            tweetData.poll_duration_minutes = pollDuration.value
        }

        await createTweet(tweetData)

        // 3. 重置状态
        content.value = ''
        selectedFiles.value = []
        previewUrls.value.forEach(p => URL.revokeObjectURL(p.url))
        previewUrls.value = []
        showPoll.value = false
        pollOptions.value = ['', '']
        pollDuration.value = 1440
        
        // 4. 通知父组件刷新列表
        emit('tweet-created')

    } catch (error) {
        console.error('Failed to tweet', error)
        alert('发送失败')
    } finally {
        loading.value = false
    }
}
</script>

<template>
  <div class="border-b border-gray-100 dark:border-gray-800 p-4">
    <div class="flex space-x-4">
       <!-- Avatar -->
      <img 
        :src="userStore.user?.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'" 
        alt="Avatar" 
        class="w-10 h-10 rounded-full object-cover"
      />
      
      <div class="flex-1">
          <!-- Input -->
          <textarea 
            v-model="content"
            class="w-full border-none focus:ring-0 text-xl placeholder-gray-500 dark:bg-black dark:text-white resize-none h-24"
            placeholder="有什么新鲜事?!"
          ></textarea>

          <!-- Media Preview Grid -->
          <div v-if="previewUrls.length > 0" class="relative mt-2 mb-4 grid gap-2" :class="previewUrls.length > 1 ? 'grid-cols-2' : 'grid-cols-1'">
              <div v-for="(media, index) in previewUrls" :key="index" class="relative group">
                  <img v-if="media.type === 'image'" :src="media.url" class="rounded-xl w-full h-32 object-cover border border-gray-100 dark:border-gray-800" />
                  <video v-else :src="media.url" class="rounded-xl w-full h-32 object-cover border border-gray-100 dark:border-gray-800" controls></video>
                  
                  <button 
                    @click="removeMedia(index)"
                    class="absolute top-1 right-1 bg-black/50 text-white rounded-full p-1 hover:bg-black/70 transition-colors"
                  >
                      <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                        <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
                      </svg>
                  </button>
              </div>
          </div>

          <div class="border-t border-gray-100 dark:border-gray-800 pt-3 flex justify-between items-center">
              <!-- Tools -->
              <div class="flex space-x-2 text-primary">
                  <button 
                    @click="triggerFileInput"
                    class="p-2 rounded-full hover:bg-blue-50 dark:hover:bg-blue-900/20 transition-colors"
                    title="媒体"
                  >
                      <PhotoIcon class="w-5 h-5" />
                      <input 
                        type="file" 
                        ref="fileInput" 
                        class="hidden" 
                        multiple
                        accept="image/*,video/*"
                        @change="handleFileChange"
                      />
                  </button>
                  <!-- Other icons (GIF, Poll, Emoji etc) -->
                  
                  <button 
                    @click="togglePoll"
                    class="p-2 rounded-full hover:bg-blue-50 dark:hover:bg-blue-900/20 transition-colors"
                    :class="showPoll ? 'text-blue-500' : ''"
                    title="投票"
                  >
                      <ChartBarIcon class="w-5 h-5" />
                  </button>
              </div>
              
            <!-- Poll Editor -->
            <div v-if="showPoll" class="mt-3 border border-gray-200 dark:border-gray-800 rounded-xl p-3">
                <div class="space-y-2">
                    <div v-for="(_, index) in pollOptions" :key="index" class="flex items-center space-x-2">
                        <input 
                            v-model="pollOptions[index]" 
                            type="text" 
                            :placeholder="`选项 ${index + 1}`"
                            class="flex-1 bg-transparent border border-gray-300 dark:border-gray-700 rounded p-2 focus:ring-2 focus:ring-primary focus:border-transparent"
                            maxlength="25"
                        />
                        <button v-if="pollOptions.length > 2" @click="pollOptions.splice(index, 1)" class="text-red-500">
                             <XMarkIcon class="w-5 h-5"/>
                        </button>
                    </div>
                </div>
                
                <div class="mt-3 flex justify-between items-center text-sm">
                    <button v-if="pollOptions.length < 4" @click="addPollOption" class="text-primary flex items-center hover:underline">
                        <PlusIcon class="w-4 h-4 mr-1"/> 添加选项
                    </button>
                    <div v-else></div> <!-- Spacer -->

                    <select v-model="pollDuration" class="bg-transparent border border-gray-300 dark:border-gray-700 rounded p-1 text-gray-600 dark:text-gray-300">
                        <option v-for="d in durationOptions" :key="d.value" :value="d.value">{{ d.label }}</option>
                    </select>
                </div>
                
                <div class="mt-3 border-t border-gray-100 dark:border-gray-800 pt-2 text-center">
                    <button @click="togglePoll" class="text-red-500 text-sm hover:underline">移除投票</button>
                </div>
            </div>

               <!-- Submit Button -->
              <button 
                @click="handleTweet"
                :disabled="loading || (!content.trim() && selectedFiles.length === 0)"
                class="bg-primary hover:bg-blue-600 text-white font-bold py-1.5 px-4 rounded-full disabled:opacity-50 transition-colors"
              >
                  {{ loading ? '发送中...' : '发推' }}
              </button>
          </div>
      </div>
    </div>
  </div>
</template>
