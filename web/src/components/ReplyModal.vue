<script setup lang="ts">
import { ref, computed } from 'vue'
import { Dialog, DialogPanel, TransitionChild, TransitionRoot } from '@headlessui/vue'
import { XMarkIcon, PhotoIcon } from '@heroicons/vue/24/outline'
import { useUserStore } from '../stores/user'
import { createTweet, type Tweet } from '../api/tweet'
import { uploadMedia } from '../api/upload'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const props = defineProps<{
  show: boolean
  replyTo: Tweet | null
}>()

const emit = defineEmits(['close', 'reply-created'])

const userStore = useUserStore()
const content = ref('')
const loading = ref(false)
const selectedFiles = ref<File[]>([])
const previewUrls = ref<{ url: string, type: 'image' | 'video' }[]>([])
const fileInput = ref<HTMLInputElement | null>(null)

const formattedTime = computed(() => {
    if (!props.replyTo?.created_at) return ''
    return dayjs(props.replyTo.created_at).fromNow()
})

const close = () => {
    emit('close')
    // Reset state after close animation
    setTimeout(() => {
        content.value = ''
        selectedFiles.value = []
        previewUrls.value.forEach(p => URL.revokeObjectURL(p.url))
        previewUrls.value = []
    }, 200)
}

const triggerFileInput = () => {
    fileInput.value?.click()
}

const handleFileChange = (event: Event) => {
    const target = event.target as HTMLInputElement
    if (target.files) {
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
    target.value = ''
}

const removeMedia = (index: number) => {
    selectedFiles.value.splice(index, 1)
    const mediaUrl = previewUrls.value[index]?.url
    if (mediaUrl) URL.revokeObjectURL(mediaUrl)
    previewUrls.value.splice(index, 1)
}

const handleReply = async () => {
    if ((!content.value.trim() && selectedFiles.value.length === 0) || !props.replyTo) return
    
    loading.value = true
    try {
        let mediaUrls: string[] = []
        
        if (selectedFiles.value.length > 0) {
            const uploadPromises = selectedFiles.value.map(file => uploadMedia(file))
            const responses = await Promise.all(uploadPromises)
            mediaUrls = responses.map(res => res.data.url)
        }

        await createTweet({
            content: content.value,
            media_urls: mediaUrls,
            parent_id: props.replyTo.id
        })

        emit('reply-created')
        close()

    } catch (error) {
        console.error('Failed to reply', error)
        alert('回复失败')
    } finally {
        loading.value = false
    }
}
</script>

<template>
  <TransitionRoot as="template" :show="show">
    <Dialog as="div" class="relative z-50" @close="close">
      <TransitionChild as="template" enter="ease-out duration-300" enter-from="opacity-0" enter-to="opacity-100" leave="ease-in duration-200" leave-from="opacity-100" leave-to="opacity-0">
        <div class="fixed inset-0 bg-gray-500 bg-opacity-40 transition-opacity" />
      </TransitionChild>

      <div class="fixed inset-0 z-10 overflow-y-auto">
        <div class="flex min-h-full items-start justify-center p-4 text-center sm:p-0 pt-10 sm:pt-20">
          <TransitionChild as="template" enter="ease-out duration-300" enter-from="opacity-0 translate-y-4 sm:translate-y-0 sm:scale-95" enter-to="opacity-100 translate-y-0 sm:scale-100" leave="ease-in duration-200" leave-from="opacity-100 translate-y-0 sm:scale-100" leave-to="opacity-0 translate-y-4 sm:translate-y-0 sm:scale-95">
            <DialogPanel class="relative transform overflow-hidden rounded-2xl bg-white dark:bg-black text-left shadow-xl transition-all sm:my-8 sm:w-full sm:max-w-xl border border-gray-100 dark:border-gray-800">
              <div class="px-4 pb-4 pt-5 sm:p-6 sm:pb-4">
                <div class="absolute right-0 top-0 hidden pr-4 pt-4 sm:block">
                  <button type="button" class="rounded-md bg-white dark:bg-black text-gray-400 hover:text-gray-500 focus:outline-none" @click="close">
                    <span class="sr-only">Close</span>
                    <XMarkIcon class="h-6 w-6" aria-hidden="true" />
                  </button>
                </div>
                
                <div class="mt-3 sm:mt-0 sm:ml-4 sm:text-left">
                  <!-- Parent Tweet Context -->
                  <div class="flex space-x-3 relative mb-2">
                       <div class="flex-shrink-0 flex flex-col items-center">
                            <img 
                                :src="replyTo?.user?.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'" 
                                class="w-10 h-10 rounded-full object-cover"
                            />
                            <!-- Connecting Line -->
                            <div class="w-0.5 bg-gray-200 dark:bg-gray-800 flex-grow mt-2 h-full absolute top-10 left-[19px] -z-10"></div>
                       </div>
                       <div class="flex-1 pb-4">
                            <div class="flex items-center space-x-1 text-sm">
                                <span class="font-bold text-gray-900 dark:text-white">{{ replyTo?.user?.username }}</span>
                                <span class="text-gray-500">@{{ replyTo?.user?.username }}</span>
                                <span class="text-gray-500">·</span>
                                <span class="text-gray-500">{{ formattedTime }}</span>
                            </div>
                            <p class="text-gray-900 dark:text-white mt-1 text-sm line-clamp-3">{{ replyTo?.content }}</p>
                            <div class="mt-2 text-gray-500 text-sm">
                                回复 <span class="text-primary">@{{ replyTo?.user?.username }}</span>
                            </div>
                       </div>
                  </div>

                  <!-- Reply Input -->
                  <div class="flex space-x-3 mt-4">
                      <img 
                        :src="userStore.user?.avatar || 'https://abs.twimg.com/sticky/default_profile_images/default_profile_400x400.png'" 
                        class="w-10 h-10 rounded-full object-cover"
                      />
                      <div class="flex-1">
                          <textarea 
                            v-model="content"
                            class="w-full border-none focus:ring-0 text-lg placeholder-gray-500 dark:bg-black dark:text-white resize-none h-24 p-0"
                            placeholder="发布你的回复"
                          ></textarea>

                          <!-- Media Preview -->
                          <div v-if="previewUrls.length > 0" class="relative mt-2 mb-4 grid gap-2" :class="previewUrls.length > 1 ? 'grid-cols-2' : 'grid-cols-1'">
                              <div v-for="(media, index) in previewUrls" :key="index" class="relative group">
                                  <img v-if="media.type === 'image'" :src="media.url" class="rounded-xl w-full h-32 object-cover border border-gray-100 dark:border-gray-800" />
                                  <video v-else :src="media.url" class="rounded-xl w-full h-32 object-cover border border-gray-100 dark:border-gray-800" controls></video>
                                  
                                  <button 
                                    @click="removeMedia(index)"
                                    class="absolute top-1 right-1 bg-black/50 text-white rounded-full p-1 hover:bg-black/70 transition-colors"
                                  >
                                      <XMarkIcon class="h-4 w-4 text-white" />
                                  </button>
                              </div>
                          </div>
                      </div>
                  </div>
                </div>
              </div>

              <!-- Tools and Submit -->
              <div class="border-t border-gray-100 dark:border-gray-800 px-4 py-3 sm:px-6 flex justify-between items-center bg-gray-50 dark:bg-black">
                   <div class="flex space-x-2 text-primary ml-[52px]">
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
                  </div>
                  <button
                    type="button"
                    class="rounded-full bg-primary px-4 py-1.5 text-sm font-bold text-white shadow-sm hover:bg-blue-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600 disabled:opacity-50"
                    @click="handleReply"
                    :disabled="loading || (!content.trim() && selectedFiles.length === 0)"
                  >
                    {{ loading ? '发送中...' : '回复' }}
                  </button>
              </div>
            </DialogPanel>
          </TransitionChild>
        </div>
      </div>
    </Dialog>
  </TransitionRoot>
</template>
