<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '../stores/user'
import { login } from '../api/auth'

const email = ref('')
const password = ref('')
const loading = ref(false)
const errorMsg = ref('')

const router = useRouter()
const userStore = useUserStore()
let disposed = false
onBeforeUnmount(() => { disposed = true })

const handleLogin = async () => {
  if (loading.value) return
  const epoch = userStore.beginLogin()
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await login({
      email: email.value,
      password: password.value
    })
    
    if (!disposed && userStore.completeLogin(epoch, res.data.token, res.data.user)) void router.replace('/')

  } catch (err: any) {
    if (disposed || !userStore.isCurrent(epoch)) return
    if (err.response && err.response.data) {
        errorMsg.value = err.response.data.error || '登录失败'
    } else if (err.message) {
        errorMsg.value = err.message
    } else {
        errorMsg.value = '网络错误，请检查后端服务'
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-full flex-1 flex-col justify-center px-6 py-12 lg:px-8">
    <div class="sm:mx-auto sm:w-full sm:max-w-sm">
      <img class="mx-auto h-10 w-auto" src="https://upload.wikimedia.org/wikipedia/commons/6/6f/Logo_of_Twitter.svg" alt="Twitter Clone" />
      <h2 class="mt-10 text-center text-2xl/9 font-bold tracking-tight text-gray-900">登录你的账号</h2>
    </div>

    <div class="mt-10 sm:mx-auto sm:w-full sm:max-w-sm">
      <form class="space-y-6" @submit.prevent="handleLogin">
        <div>
          <label for="email" class="block text-sm/6 font-medium text-gray-900">邮箱地址</label>
          <div class="mt-2">
            <input v-model="email" id="email" name="email" type="email" autocomplete="email" required class="block w-full rounded-md bg-white px-3 py-1.5 text-base text-gray-900 outline-1 -outline-offset-1 outline-gray-300 placeholder:text-gray-400 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 sm:text-sm/6" />
          </div>
        </div>

        <div>
          <div class="flex items-center justify-between">
            <label for="password" class="block text-sm/6 font-medium text-gray-900">密码</label>
            <!-- <div class="text-sm">
              <a href="#" class="font-semibold text-indigo-600 hover:text-indigo-500">忘记密码?</a>
            </div> -->
          </div>
          <div class="mt-2">
            <input v-model="password" id="password" name="password" type="password" autocomplete="current-password" required class="block w-full rounded-md bg-white px-3 py-1.5 text-base text-gray-900 outline-1 -outline-offset-1 outline-gray-300 placeholder:text-gray-400 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 sm:text-sm/6" />
          </div>
        </div>

        <div v-if="errorMsg" class="text-red-500 text-sm text-center">
            {{ errorMsg }}
        </div>

        <div>
          <button type="submit" :disabled="loading" class="flex w-full justify-center rounded-md bg-indigo-600 px-3 py-1.5 text-sm/6 font-semibold text-white shadow-sm hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 disabled:opacity-50">
            {{ loading ? '登录中...' : '登录' }}
          </button>
        </div>
      </form>

      <p class="mt-10 text-center text-sm/6 text-gray-500">
        还没有账号?
        <router-link to="/register" class="font-semibold text-indigo-600 hover:text-indigo-500">立即注册</router-link>
      </p>
    </div>
  </div>
</template>
