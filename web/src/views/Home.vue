<script setup lang="ts">
import MainLayout from '../layout/MainLayout.vue'
import ComposeBox from '../components/ComposeBox.vue'
import TweetCard from '../components/TweetCard.vue'
import ReplyModal from '../components/ReplyModal.vue'
import { ref, onMounted } from 'vue'
import { getFeeds, listTweets, type Tweet } from '../api/tweet'

const tweets = ref<Tweet[]>([])
const loading = ref(false)
const cursor = ref('0')
const hasMore = ref(true)
const activeTab = ref<'following' | 'forYou'>('forYou')

const fetchTweets = async (refresh = false) => {
    if (loading.value) return
    loading.value = true
    try {
        const currentCursor = refresh ? '0' : cursor.value

        let res
        if (activeTab.value === 'following') {
            res = await getFeeds(currentCursor)
        } else {
            res = await listTweets(currentCursor)
        }

        const newTweets = res.data.tweets || []
        if (refresh) {
            tweets.value = newTweets
        } else {
            tweets.value.push(...newTweets)
        }

        cursor.value = res.data.next_cursor
        hasMore.value = res.data.has_more

    } catch (error) {
        console.error('Failed to load tweets', error)
    } finally {
        loading.value = false
    }
}

const switchTab = (tab: 'following' | 'forYou') => {
    activeTab.value = tab
    tweets.value = []
    cursor.value = '0'
    hasMore.value = true
    fetchTweets(true)
}

const handleTweetCreated = () => {
    fetchTweets(true)
}

const handleTweetDeleted = (tweetId: string) => {
    tweets.value = tweets.value.filter(t => t.id !== tweetId)
}

const showReplyModal = ref(false)
const replyToTweet = ref<Tweet | null>(null)

const handleReply = (tweet: Tweet) => {
    replyToTweet.value = tweet
    showReplyModal.value = true
}

const handleReplyCreated = () => {
    if (replyToTweet.value) {
        replyToTweet.value.comment_count++
    }
}

onMounted(() => {
    fetchTweets(true)
})
</script>

<template>
  <MainLayout>
      <!-- Header with Tabs -->
      <div class="sticky top-0 bg-white/80 dark:bg-black/80 backdrop-blur-md z-10 border-b border-gray-100 dark:border-gray-800">
          <h1 class="font-bold text-xl px-4 py-3">主页</h1>
          <div class="flex">
              <button
                @click="switchTab('forYou')"
                class="flex-1 text-center py-3 font-medium hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors relative"
                :class="activeTab === 'forYou' ? 'text-gray-900 dark:text-white font-bold' : 'text-gray-500'"
              >
                  为你推荐
                  <div v-if="activeTab === 'forYou'" class="absolute bottom-0 left-1/2 -translate-x-1/2 w-14 h-1 bg-primary rounded-full"></div>
              </button>
              <button
                @click="switchTab('following')"
                class="flex-1 text-center py-3 font-medium hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors relative"
                :class="activeTab === 'following' ? 'text-gray-900 dark:text-white font-bold' : 'text-gray-500'"
              >
                  正在关注
                  <div v-if="activeTab === 'following'" class="absolute bottom-0 left-1/2 -translate-x-1/2 w-14 h-1 bg-primary rounded-full"></div>
              </button>
          </div>
      </div>

      <!-- Compose Box -->
      <ComposeBox @tweet-created="handleTweetCreated" />





      <!-- Feed List -->
      <div v-if="tweets.length > 0">
          <TweetCard 
            v-for="tweet in tweets" 
            :key="tweet.id" 
            :tweet="tweet" 
            @deleted="handleTweetDeleted" 
            @reply="handleReply"
          />
      </div>

      <!-- ... -->

      <ReplyModal 
        :show="showReplyModal" 
        :replyTo="replyToTweet" 
        @close="showReplyModal = false" 
        @reply-created="handleReplyCreated" 
      />
  </MainLayout>
</template>
