<script setup lang="ts">
import { RouterView } from 'vue-router'
import { computed, watch, onMounted, onBeforeUnmount } from 'vue'
import router from './router'
import { useUserStore } from './stores/user'
import SupportWidget from './components/SupportWidget.vue'

const userStore = useUserStore()
function storageChanged(event: StorageEvent) {
  if (event.storageArea === localStorage && (event.key === null || event.key === 'token' || event.key === 'user')) {
    userStore.reload()
  }
}
onMounted(() => window.addEventListener('storage', storageChanged))
onBeforeUnmount(() => window.removeEventListener('storage', storageChanged))
watch(() => userStore.token, (token, oldToken) => {
  if (!token && oldToken) void router.replace('/login')
}, { flush: 'sync' })
const assistantUrl = import.meta.env.VITE_SUPPORT_ASSISTANT_URL || 'http://127.0.0.1:5174/'
const supportAccount = computed(() => userStore.user ? {
  id: String(userStore.user.id),
  username: String(userStore.user.username || ''),
  email: String(userStore.user.email || ''),
} : null)
</script>

<template>
  <RouterView :key="userStore.token" />
  <SupportWidget :key="userStore.token" :token="userStore.token" :account="supportAccount" :assistant-url="assistantUrl" @navigate="router.push($event)" @login="router.push('/login')" />
</template>
