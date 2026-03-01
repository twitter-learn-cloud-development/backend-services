<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { register } from '../api/auth'

const username = ref('')
const email = ref('')
const password = ref('')
const loading = ref(false)
const errorMsg = ref('')

const router = useRouter()

const handleRegister = async () => {
  loading.value = true
  errorMsg.value = ''
  try {
    await register({
      username: username.value,
      email: email.value,
      password: password.value
    })
    // 注册成功，跳转登录
    alert('注册成功，请登录')
    router.push('/login')
  } catch (err: any) {
    if (err.response && err.response.data) {
        errorMsg.value = err.response.data.error || '注册失败'
    } else {
        errorMsg.value = '网络错误'
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
      <h2 class="mt-10 text-center text-2xl/9 font-bold tracking-tight text-gray-900">创建新账号</h2>
    </div>

    <div class="mt-10 sm:mx-auto sm:w-full sm:max-w-sm">
      <form class="space-y-6" @submit.prevent="handleRegister">
        <div>
          <label for="username" class="block text-sm/6 font-medium text-gray-900">用户名</label>
          <div class="mt-2">
            <input v-model="username" id="username" name="username" type="text" required class="block w-full rounded-md bg-white px-3 py-1.5 text-base text-gray-900 outline-1 -outline-offset-1 outline-gray-300 placeholder:text-gray-400 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 sm:text-sm/6" />
          </div>
        </div>

        <div>
           <label for="email" class="block text-sm/6 font-medium text-gray-900">邮箱地址</label>
           <div class="mt-2">
             <input v-model="email" id="email" name="email" type="email" autocomplete="email" required class="block w-full rounded-md bg-white px-3 py-1.5 text-base text-gray-900 outline-1 -outline-offset-1 outline-gray-300 placeholder:text-gray-400 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 sm:text-sm/6" />
           </div>
         </div>

        <div>
          <label for="password" class="block text-sm/6 font-medium text-gray-900">密码</label>
          <div class="mt-2">
            <input v-model="password" id="password" name="password" type="password" autocomplete="new-password" required class="block w-full rounded-md bg-white px-3 py-1.5 text-base text-gray-900 outline-1 -outline-offset-1 outline-gray-300 placeholder:text-gray-400 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 sm:text-sm/6" />
          </div>
        </div>

        <div v-if="errorMsg" class="text-red-500 text-sm text-center">
            {{ errorMsg }}
        </div>

        <div>
          <button type="submit" :disabled="loading" class="flex w-full justify-center rounded-md bg-indigo-600 px-3 py-1.5 text-sm/6 font-semibold text-white shadow-sm hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 disabled:opacity-50">
            {{ loading ? '注册中...' : '注册' }}
          </button>
        </div>
      </form>

      <p class="mt-10 text-center text-sm/6 text-gray-500">
        已有账号?
        <router-link to="/login" class="font-semibold text-indigo-600 hover:text-indigo-500">登录</router-link>
      </p>
    </div>
  </div>
</template>
